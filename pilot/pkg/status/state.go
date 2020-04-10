package status

import (
	"context"
	"fmt"
	"github.com/ghodss/yaml"
	"istio.io/pkg/log"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1beta1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/clock"
	"sync"
	"time"
)

var scope = log.RegisterScope("statusController",
	"component for writing distribution status to istiio CRDs", 0)

type Fraction struct {
	Numerator int
	Denominator int
}

func (fraction Fraction) Add(fraction2 Fraction) {
	fraction.Denominator += fraction2.Denominator
	fraction.Numerator += fraction2.Numerator
}

type DistributionController struct {
	lock sync.RWMutex
	CurrentState    map[Resource]map[string]Fraction
	ObservationTime map[string]time.Time
	UpdateInterval  time.Duration
	client          dynamic.Interface
	clock           clock.Clock
	StaleInterval   time.Duration
}

func (c *DistributionController) Start(restConfig *rest.Config, stop <-chan struct{}) {
	// default UpdateInterval
	if c.UpdateInterval == 0 {
		c.UpdateInterval = 100*time.Millisecond
	}
	// default StaleInterval
	if c.StaleInterval == 0 {
		c.StaleInterval = time.Minute
	}
	if c.clock == nil {
		c.clock = clock.RealClock{}
	}
	c.CurrentState = make(map[Resource] map[string] Fraction)
	c.ObservationTime = make(map[string]time.Time)
	c.client = dynamic.NewForConfigOrDie(restConfig)
	// create watch
	i := informers.NewSharedInformerFactory(kubernetes.NewForConfigOrDie(restConfig), 1*time.Minute).
		Core().V1().ConfigMaps()
	i.Informer().AddEventHandler(DistroReportHandler{dc:*c})
	// this will list all existing configmaps, as well as updates, right?
	i.Informer().Run(stop)
	
	//create Status Writer
	t:=c.clock.Tick(c.UpdateInterval)
	go func() {
		for {
			select {
			case <-stop:
				return
			case _ = <-t:
				staleReporters := c.writeAllStatus()
				if len(staleReporters) > 0 {
					c.removeStaleReporters(staleReporters)
				}
			}
		}
	}()
}

func (c *DistributionController) handleReport(d DistributionReport) {
	defer c.lock.Unlock()
	c.lock.Lock()
	for resstr := range d.InProgressResources {
		res := *ResourceFromString(resstr)
		if _, ok := c.CurrentState[res]; !ok {
			c.CurrentState[res] = make(map[string]Fraction)
		}
		c.CurrentState[res][d.Reporter] = Fraction{d.InProgressResources[resstr], d.DataPlaneCount}
	}
	c.ObservationTime[d.Reporter] = c.clock.Now()
}

func (c *DistributionController) writeAllStatus() (staleReporters []string) {
	defer c.lock.RUnlock()
	c.lock.RLock()
	for config, fractions := range c.CurrentState {
		var distributionState Fraction
		for reporter, w := range fractions {
			// check for stale data here
			if c.clock.Since(c.ObservationTime[reporter]) > c.StaleInterval {
				scope.Warnf("Status reporter %s has not been heard from since %v, deleting report.",
					reporter, c.ObservationTime[reporter])
				staleReporters = append(staleReporters, reporter)
				continue
			}
			distributionState.Add(w)
		}
		go c.writeStatus(config, distributionState)
	}
	return
}

func (c *DistributionController) writeStatus(config Resource, distributionState Fraction) {
	// Note: I'd like to use Pilot's ConfigStore here to avoid duplicate reads and writes, but
	// the update() function is not implemented, and the Get() function returns the resource
	// in a different format than is needed for k8s.updateStatus.
	ctx := context.TODO()
	resourceInterface := c.client.Resource(config.GroupVersionResource).
		Namespace(config.Namespace)
	// should this be moved to some sort of InformerCache for speed?
	current, err := resourceInterface.Get(ctx, config.Name, v12.GetOptions{ResourceVersion: config.ResourceVersion})
	if err != nil {
		if errors.IsGone(err){
			// this resource has been deleted.  prune its state and move on.
			c.pruneOldVersion(config)
			return
		} else {
			scope.Errorf("Encountered unexpected error when retrieving status for %v: %s", config, err)
			return
		}
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
		_, err := resourceInterface.UpdateStatus(ctx, current, v12.UpdateOptions{})
		if err != nil {
			scope.Errorf("Encountered unexpected error updating status for %v, will try again later: %s", config, err)
			return
		}
	}
}

func (c *DistributionController) pruneOldVersion(config Resource) {
	defer c.lock.Unlock()
	c.lock.Lock()
	delete(c.CurrentState, config)
}

func (c *DistributionController) removeStaleReporters(staleReporters []string) {
	defer c.lock.Unlock()
	c.lock.Lock()
	for key, fractions := range c.CurrentState {
		for _, staleReporter := range staleReporters {
			delete(fractions, staleReporter)
		}
		c.CurrentState[key] = fractions
	}
}

func getTypedStatus(in interface{}) (out IstioStatus, err error) {
	var statusBytes []byte
	if statusBytes, err = yaml.Marshal(in); err == nil {
		err = yaml.Unmarshal(statusBytes, &out)
	}
	return
}

func boolToConditionStatus(b bool) v1beta1.ConditionStatus {
	if b {
		return v12.ConditionTrue
	}
	return v12.ConditionFalse
}

func ReconcileStatuses(current map[string]interface{}, desired Fraction, clock clock.Clock) (bool, *IstioStatus) {
	needsReconcile := false
	currentStatus, err := getTypedStatus(current["status"])
	desiredCondition := IstioCondition{
		Type:               StillPropagating,
		Status:             boolToConditionStatus(desired.Numerator != desired.Denominator),
		LastProbeTime:      v12.NewTime(clock.Now()),
		LastTransitionTime: v12.NewTime(clock.Now()),
		Message:            fmt.Sprintf("%d/%d dataplanes up to date.", desired.Numerator, desired.Denominator),
	}
	if err != nil {
		// the status field is in an unexpected state.
		scope.Warn("Encountered unexpected status content.  Overwriting status.")
		scope.Debugf("Encountered unexpected status content.  Overwriting status: %v", current["status"])
		currentStatus = IstioStatus{
			Conditions:         []IstioCondition{desiredCondition},
		}
		return true, &currentStatus
	}
	var currentCondition *IstioCondition
	conditionIndex := -1
	for i, c := range currentStatus.Conditions {
		if c.Type == StillPropagating {
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
	dc DistributionController
}
func (drh DistroReportHandler) OnAdd(obj interface{}){
	drh.HandleNew(obj)
}

func (drh DistroReportHandler) OnUpdate(oldObj, newObj interface{}) {
	drh.HandleNew(newObj)
}

func (drh DistroReportHandler) HandleNew(obj interface{}) {
	cm := obj.(v1.ConfigMap)
	dr, err := ReportFromYaml([]byte(cm.Data["somekey"]))
	if err != nil {
		scope.Warnf("received malformed distributionReport %s, discarding: %v", cm.Name, err)
		return
	}
	drh.dc.handleReport(dr)
}

func (drh DistroReportHandler) OnDelete(obj interface{}){
	// TODO: what do we do here?  will these ever be deleted?
}