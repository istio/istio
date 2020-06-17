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
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"k8s.io/apimachinery/pkg/labels"

	"github.com/ghodss/yaml"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1beta1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/clock"

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
	mu               sync.RWMutex
	CurrentState     map[Resource]map[string]Progress
	ObservationTime  map[string]time.Time
	UpdateInterval   time.Duration
	client           dynamic.Interface
	clock            clock.Clock
	knownResources   map[schema.GroupVersionResource]dynamic.NamespaceableResourceInterface
	currentlyWriting ResourceLock
	StaleInterval    time.Duration
	QPS              float32
	Burst            int
}

func (c *DistributionController) Start(restConfig *rest.Config, namespace string, stop <-chan struct{}) {
	scope.Info("Starting status leader controller")
	// default UpdateInterval
	if c.UpdateInterval == 0 {
		c.UpdateInterval = 200 * time.Millisecond
	}
	// default StaleInterval
	if c.StaleInterval == 0 {
		c.StaleInterval = time.Minute
	}
	if c.clock == nil {
		c.clock = clock.RealClock{}
	}
	c.CurrentState = make(map[Resource]map[string]Progress)
	c.ObservationTime = make(map[string]time.Time)
	c.knownResources = make(map[schema.GroupVersionResource]dynamic.NamespaceableResourceInterface)

	// client-go defaults to 5 QPS, with 10 Boost, which is insufficient for updating status on all the config
	// in the mesh.  These values can be configured using environment variables for tuning (see pilot/pkg/features)
	restConfig.QPS = c.QPS
	restConfig.Burst = c.Burst
	var err error
	if c.client, err = dynamic.NewForConfig(restConfig); err != nil {
		scope.Fatalf("Could not connect to kubernetes: %s", err)
	}
	// create watch
	i := informers.NewSharedInformerFactoryWithOptions(kubernetes.NewForConfigOrDie(restConfig), 1*time.Minute,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
			listOptions.LabelSelector = labels.Set(map[string]string{labelKey: "true"}).AsSelector().String()
		})).
		Core().V1().ConfigMaps()
	i.Informer().AddEventHandler(&DistroReportHandler{dc: c})
	// this will list all existing configmaps, as well as updates, right?
	ctx := NewIstioContext(stop)
	go i.Informer().Run(ctx.Done())

	//create Status Writer
	t := c.clock.Tick(c.UpdateInterval)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-t:
				staleReporters := c.writeAllStatus(ctx)
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

func (c *DistributionController) writeAllStatus(ctx context.Context) (staleReporters []string) {
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
			go c.writeStatus(ctx, config, distributionState)
		}
	}
	return
}

func (c *DistributionController) initK8sResource(gvr schema.GroupVersionResource) (result dynamic.NamespaceableResourceInterface) {
	if result, ok := c.knownResources[gvr]; ok {
		return result
	}
	result = c.client.Resource(gvr)
	c.knownResources[gvr] = result
	return
}

func (c *DistributionController) writeStatus(ctx context.Context, config Resource, distributionState Progress) {
	// Note: I'd like to use Pilot's ConfigStore here to avoid duplicate reads and writes, but
	// the update() function is not implemented, and the Get() function returns the resource
	// in a different format than is needed for k8s.updateStatus.
	c.currentlyWriting.Lock(config)
	defer c.currentlyWriting.Unlock(config)
	resourceInterface := c.initK8sResource(config.GroupVersionResource).
		Namespace(config.Namespace)
	// should this be moved to some sort of InformerCache for speed?
	current, err := resourceInterface.Get(ctx, config.Name, metav1.GetOptions{ResourceVersion: config.ResourceVersion})
	if err != nil {
		if errors.IsGone(err) || errors.IsNotFound(err) {
			// this resource has been deleted.  prune its state and move on.
			c.pruneOldVersion(config)
			return
		}
		scope.Errorf("Encountered unexpected error when retrieving status for %v: %s", config, err)
		return

	}
	if config.ResourceVersion != current.GetResourceVersion() {
		// this distribution report is for an old version of the object.  Prune and continue.
		c.pruneOldVersion(config)
		return
	}
	// check if status needs updating
	if needsReconcile, desiredStatus := ReconcileStatuses(current.Object, distributionState, c.clock); needsReconcile {
		// technically, we should be updating probe time even when reconciling isn't needed, but
		// I'm skipping that for efficiency.
		current.Object["status"] = desiredStatus
		_, err := resourceInterface.UpdateStatus(ctx, current, metav1.UpdateOptions{})
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

func GetTypedStatus(in interface{}) (out IstioStatus, err error) {
	var statusBytes []byte
	if statusBytes, err = yaml.Marshal(in); err == nil {
		err = yaml.Unmarshal(statusBytes, &out)
	}
	return
}

func boolToConditionStatus(b bool) v1beta1.ConditionStatus {
	if b {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}

func ReconcileStatuses(current map[string]interface{}, desired Progress, clock clock.Clock) (bool, *IstioStatus) {
	needsReconcile := false
	currentStatus, err := GetTypedStatus(current["status"])
	desiredCondition := IstioCondition{
		Type:               Reconciled,
		Status:             boolToConditionStatus(desired.AckedInstances == desired.TotalInstances),
		LastProbeTime:      metav1.NewTime(clock.Now()),
		LastTransitionTime: metav1.NewTime(clock.Now()),
		Message:            fmt.Sprintf("%d/%d proxies up to date.", desired.AckedInstances, desired.TotalInstances),
	}
	if err != nil {
		// the status field is in an unexpected state.
		scope.Warn("Encountered unexpected status content.  Overwriting status.")
		scope.Debugf("Encountered unexpected status content.  Overwriting status: %v", current["status"])
		currentStatus = IstioStatus{
			Conditions: []IstioCondition{desiredCondition},
		}
		return true, &currentStatus
	}
	var currentCondition *IstioCondition
	conditionIndex := -1
	for i, c := range currentStatus.Conditions {
		if c.Type == Reconciled {
			currentCondition = &c
			conditionIndex = i
		}
	}
	if currentCondition == nil ||
		currentCondition.Message != desiredCondition.Message ||
		currentCondition.Status != desiredCondition.Status {
		needsReconcile = true
	}
	if conditionIndex > -1 {
		currentStatus.Conditions[conditionIndex] = desiredCondition
	} else {
		currentStatus.Conditions = append(currentStatus.Conditions, desiredCondition)
	}
	return needsReconcile, &currentStatus
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
