// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package status

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gogo/protobuf/types"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/clock"

	"istio.io/api/meta/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/pkg/log"
)

var scope = log.RegisterScope("status",
	"CRD distribution status debugging", 0)

type Progress struct {
	AckedInstances int
	TotalInstances int
}

func (p *Progress) PlusEquals(p2 Progress) {
	p.TotalInstances += p2.TotalInstances
	p.AckedInstances += p2.AckedInstances
}

type DistributionController struct {
	configStore     model.ConfigStore
	mu              sync.RWMutex
	CurrentState    map[Resource]map[string]Progress
	ObservationTime map[string]time.Time
	UpdateInterval  time.Duration
	dynamicClient   dynamic.Interface
	clock           clock.Clock
	workers         WorkerQueue
	StaleInterval   time.Duration
	cmInformer      cache.SharedIndexInformer
}

func NewController(restConfig rest.Config, namespace string, cs model.ConfigStore) *DistributionController {
	c := &DistributionController{
		CurrentState:    make(map[Resource]map[string]Progress),
		ObservationTime: make(map[string]time.Time),
		UpdateInterval:  200 * time.Millisecond,
		StaleInterval:   time.Minute,
		clock:           clock.RealClock{},
		configStore:     cs,
	}

	// client-go defaults to 5 QPS, with 10 Boost, which is insufficient for updating status on all the config
	// in the mesh.  These values can be configured using environment variables for tuning (see pilot/pkg/features)
	restConfig.QPS = float32(features.StatusQPS)
	restConfig.Burst = features.StatusBurst
	var err error
	if c.dynamicClient, err = dynamic.NewForConfig(&restConfig); err != nil {
		scope.Fatalf("Could not connect to kubernetes: %s", err)
	}

	// configmap informer
	i := informers.NewSharedInformerFactoryWithOptions(kubernetes.NewForConfigOrDie(&restConfig), 1*time.Minute,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
			listOptions.LabelSelector = labels.Set(map[string]string{labelKey: "true"}).AsSelector().String()
		})).
		Core().V1().ConfigMaps()
	c.cmInformer = i.Informer()
	i.Informer().AddEventHandler(&DistroReportHandler{dc: c})

	return c
}

func (c *DistributionController) Start(stop <-chan struct{}) {
	scope.Info("Starting status leader controller")

	// this will list all existing configmaps, as well as updates, right?
	ctx := NewIstioContext(stop)
	go c.cmInformer.Run(ctx.Done())

	c.workers = NewWorkerPool(func(resource *Resource, progress *Progress) {
		c.writeStatus(*resource, *progress)
	}, uint(features.StatusMaxWorkers.Get()))
	c.workers.Run(ctx)

	//  create Status Writer
	t := c.clock.Tick(c.UpdateInterval)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-t:
				staleReporters := c.writeAllStatus()
				if len(staleReporters) > 0 {
					c.removeStaleReporters(staleReporters)
				}
			}
		}
	}()
}

func (c *DistributionController) handleReport(d DistributionReport) {
	defer c.mu.Unlock()
	c.mu.Lock()
	for resstr := range d.InProgressResources {
		res := *ResourceFromString(resstr)
		if _, ok := c.CurrentState[res]; !ok {
			c.CurrentState[res] = make(map[string]Progress)
		}
		c.CurrentState[res][d.Reporter] = Progress{d.InProgressResources[resstr], d.DataPlaneCount}
	}
	c.ObservationTime[d.Reporter] = c.clock.Now()
}

func (c *DistributionController) writeAllStatus() (staleReporters []string) {
	defer c.mu.RUnlock()
	c.mu.RLock()
	for config, fractions := range c.CurrentState {
		var distributionState Progress
		for reporter, w := range fractions {
			// check for stale data here
			if c.clock.Since(c.ObservationTime[reporter]) > c.StaleInterval {
				scope.Warnf("Status reporter %s has not been heard from since %v, deleting report.",
					reporter, c.ObservationTime[reporter])
				staleReporters = append(staleReporters, reporter)
			} else {
				distributionState.PlusEquals(w)
			}
		}
		if distributionState.TotalInstances > 0 { // this is necessary when all reports are stale.
			c.queueWriteStatus(config, distributionState)
		}
	}
	return
}

func (c *DistributionController) writeStatus(config Resource, distributionState Progress) {
	schema, _ := collections.All.FindByGroupVersionResource(config.GroupVersionResource)
	if schema == nil {
		scope.Warnf("schema %v could not be identified", schema)
		c.pruneOldVersion(config)
		return
	}
	if !strings.HasSuffix(schema.Resource().Group(), "istio.io") {
		// we don't write status for objects we don't own
		return
	}
	current := c.configStore.Get(schema.Resource().GroupVersionKind(), config.Name, config.Namespace)
	if current == nil {
		scope.Warnf("config store missing entry %v, status will not update", config)
		// this happens when resources are rapidly deleted, such as the validation-readiness checker
		c.pruneOldVersion(config)
		return
	}
	if config.Generation != strconv.FormatInt(current.Generation, 10) {
		// this distribution report is for an old version of the object.  Prune and continue.
		c.pruneOldVersion(config)
		return
	}

	// check if status needs updating
	if needsReconcile, desiredStatus := ReconcileStatuses(current, distributionState, current.Generation); needsReconcile {
		// technically, we should be updating probe time even when reconciling isn't needed, but
		// I'm skipping that for efficiency.
		current.Status = desiredStatus
		_, err := c.configStore.UpdateStatus(*current)
		if err != nil {
			scope.Errorf("Encountered unexpected error updating status for %v, will try again later: %s", config, err)
			return
		}
	}
}

func (c *DistributionController) pruneOldVersion(config Resource) {
	defer c.mu.Unlock()
	c.mu.Lock()
	delete(c.CurrentState, config)
}

func (c *DistributionController) removeStaleReporters(staleReporters []string) {
	defer c.mu.Unlock()
	c.mu.Lock()
	for key, fractions := range c.CurrentState {
		for _, staleReporter := range staleReporters {
			delete(fractions, staleReporter)
		}
		c.CurrentState[key] = fractions
	}
}

func (c *DistributionController) queueWriteStatus(config Resource, state Progress) {
	c.workers.Push(config, state)
}

func (c *DistributionController) configDeleted(res config.Config) {
	r := ResourceFromModelConfig(res)
	c.workers.Delete(*r)
}

func GetTypedStatus(in interface{}) (out *v1alpha1.IstioStatus, err error) {
	if ret, ok := in.(*v1alpha1.IstioStatus); ok {
		return ret, nil
	}
	return nil, fmt.Errorf("cannot cast %t: %v to IstioStatus", in, in)
}

func boolToConditionStatus(b bool) string {
	if b {
		return "True"
	}
	return "False"
}

func ReconcileStatuses(current *config.Config, desired Progress, generation int64) (bool, *v1alpha1.IstioStatus) {
	needsReconcile := false
	currentStatus, err := GetTypedStatus(current.Status)
	desiredCondition := v1alpha1.IstioCondition{
		Type:               "Reconciled",
		Status:             boolToConditionStatus(desired.AckedInstances == desired.TotalInstances),
		LastProbeTime:      types.TimestampNow(),
		LastTransitionTime: types.TimestampNow(),
		Message:            fmt.Sprintf("%d/%d proxies up to date.", desired.AckedInstances, desired.TotalInstances),
	}
	if err != nil {
		// the status field is in an unexpected state.
		if scope.DebugEnabled() {
			scope.Debugf("Encountered unexpected status content.  Overwriting status: %v", current.Status)
		} else {
			scope.Warn("Encountered unexpected status content.  Overwriting status.")
		}
		currentStatus = &v1alpha1.IstioStatus{
			Conditions: []*v1alpha1.IstioCondition{&desiredCondition},
		}
		currentStatus.ObservedGeneration = generation
		return true, currentStatus
	}
	currentStatus = currentStatus.DeepCopy()
	var currentCondition *v1alpha1.IstioCondition
	conditionIndex := -1
	for i, c := range currentStatus.Conditions {
		if c.Type == "Reconciled" {
			currentCondition = currentStatus.Conditions[i]
			conditionIndex = i
		}
	}
	if currentCondition == nil ||
		currentCondition.Message != desiredCondition.Message ||
		currentCondition.Status != desiredCondition.Status {
		needsReconcile = true
	}
	if conditionIndex > -1 {
		currentStatus.Conditions[conditionIndex] = &desiredCondition
	} else {
		currentStatus.Conditions = append(currentStatus.Conditions, &desiredCondition)
	}
	currentStatus.ObservedGeneration = generation
	return needsReconcile, currentStatus
}

type DistroReportHandler struct {
	dc *DistributionController
}

func (drh *DistroReportHandler) OnAdd(obj interface{}) {
	drh.HandleNew(obj)
}

func (drh *DistroReportHandler) OnUpdate(oldObj, newObj interface{}) {
	drh.HandleNew(newObj)
}

func (drh *DistroReportHandler) HandleNew(obj interface{}) {
	cm, ok := obj.(*v1.ConfigMap)
	if !ok {
		scope.Warnf("expected configmap, but received %v, discarding", obj)
		return
	}
	rptStr := cm.Data[dataField]
	scope.Debugf("using report: %s", rptStr)
	dr, err := ReportFromYaml([]byte(cm.Data[dataField]))
	if err != nil {
		scope.Warnf("received malformed distributionReport %s, discarding: %v", cm.Name, err)
		return
	}
	drh.dc.handleReport(dr)
}

func (drh *DistroReportHandler) OnDelete(obj interface{}) {
	// TODO: what do we do here?  will these ever be deleted?
}
