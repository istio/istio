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

package distribution

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
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
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pkg/config"
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

type Controller struct {
	configStore     model.ConfigStore
	mu              sync.RWMutex
	CurrentState    map[status.Resource]map[string]Progress
	ObservationTime map[string]time.Time
	UpdateInterval  time.Duration
	dynamicClient   dynamic.Interface
	clock           clock.Clock
	workers         *status.Controller
	StaleInterval   time.Duration
	cmInformer      cache.SharedIndexInformer
}

func NewController(restConfig *rest.Config, namespace string, cs model.ConfigStore, m *status.Manager) *Controller {
	c := &Controller{
		CurrentState:    make(map[status.Resource]map[string]Progress),
		ObservationTime: make(map[string]time.Time),
		UpdateInterval:  200 * time.Millisecond,
		StaleInterval:   time.Minute,
		clock:           clock.RealClock{},
		configStore:     cs,
		workers: m.CreateIstioStatusController(func(status *v1alpha1.IstioStatus, context interface{}) *v1alpha1.IstioStatus {
			if status == nil {
				return nil
			}
			distributionState := context.(Progress)
			if needsReconcile, desiredStatus := ReconcileStatuses(status, distributionState); needsReconcile {
				return desiredStatus
			}
			return status
		}),
	}

	// client-go defaults to 5 QPS, with 10 Boost, which is insufficient for updating status on all the config
	// in the mesh.  These values can be configured using environment variables for tuning (see pilot/pkg/features)
	restConfig.QPS = float32(features.StatusQPS)
	restConfig.Burst = features.StatusBurst
	var err error
	if c.dynamicClient, err = dynamic.NewForConfig(restConfig); err != nil {
		scope.Fatalf("Could not connect to kubernetes: %s", err)
	}

	// configmap informer
	i := informers.NewSharedInformerFactoryWithOptions(kubernetes.NewForConfigOrDie(restConfig), 1*time.Minute,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
			listOptions.LabelSelector = labels.Set(map[string]string{labelKey: "true"}).AsSelector().String()
		})).
		Core().V1().ConfigMaps()
	c.cmInformer = i.Informer()
	i.Informer().AddEventHandler(&DistroReportHandler{dc: c})

	return c
}

func (c *Controller) Start(stop <-chan struct{}) {
	scope.Info("Starting status leader controller")

	// this will list all existing configmaps, as well as updates, right?
	ctx := status.NewIstioContext(stop)
	go c.cmInformer.Run(ctx.Done())

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

func (c *Controller) handleReport(d Report) {
	defer c.mu.Unlock()
	c.mu.Lock()
	for resstr := range d.InProgressResources {
		res := *status.ResourceFromString(resstr)
		if _, ok := c.CurrentState[res]; !ok {
			c.CurrentState[res] = make(map[string]Progress)
		}
		c.CurrentState[res][d.Reporter] = Progress{d.InProgressResources[resstr], d.DataPlaneCount}
	}
	c.ObservationTime[d.Reporter] = c.clock.Now()
}

func (c *Controller) writeAllStatus() (staleReporters []string) {
	defer c.mu.RUnlock()
	c.mu.RLock()
	for config, fractions := range c.CurrentState {
		if !strings.HasSuffix(config.Group, "istio.io") {
			// don't try to write status for non-istio types
			continue
		}
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

func (c *Controller) pruneOldVersion(config status.Resource) {
	defer c.mu.Unlock()
	c.mu.Lock()
	delete(c.CurrentState, config)
}

func (c *Controller) removeStaleReporters(staleReporters []string) {
	defer c.mu.Unlock()
	c.mu.Lock()
	for key, fractions := range c.CurrentState {
		for _, staleReporter := range staleReporters {
			delete(fractions, staleReporter)
		}
		c.CurrentState[key] = fractions
	}
}

func (c *Controller) queueWriteStatus(config status.Resource, state Progress) {
	c.workers.EnqueueStatusUpdateResource(state, config)
}

func (c *Controller) configDeleted(res config.Config) {
	r := status.ResourceFromModelConfig(res)
	c.workers.Delete(r)
}

func boolToConditionStatus(b bool) string {
	if b {
		return "True"
	}
	return "False"
}

func ReconcileStatuses(current *v1alpha1.IstioStatus, desired Progress) (bool, *v1alpha1.IstioStatus) {
	needsReconcile := false
	desiredCondition := v1alpha1.IstioCondition{
		Type:               "Reconciled",
		Status:             boolToConditionStatus(desired.AckedInstances == desired.TotalInstances),
		LastProbeTime:      timestamppb.Now(),
		LastTransitionTime: timestamppb.Now(),
		Message:            fmt.Sprintf("%d/%d proxies up to date.", desired.AckedInstances, desired.TotalInstances),
	}
	current = current.DeepCopy()
	var currentCondition *v1alpha1.IstioCondition
	conditionIndex := -1
	for i, c := range current.Conditions {
		if c.Type == "Reconciled" {
			currentCondition = current.Conditions[i]
			conditionIndex = i
			break
		}
	}
	if currentCondition == nil ||
		currentCondition.Message != desiredCondition.Message ||
		currentCondition.Status != desiredCondition.Status {
		needsReconcile = true
	}
	if conditionIndex > -1 {
		current.Conditions[conditionIndex] = &desiredCondition
	} else {
		current.Conditions = append(current.Conditions, &desiredCondition)
	}
	return needsReconcile, current
}

type DistroReportHandler struct {
	dc *Controller
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
