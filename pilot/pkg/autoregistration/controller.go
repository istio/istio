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

package autoregistration

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/autoregistration/internal/autoregistration"
	"istio.io/istio/pilot/pkg/autoregistration/internal/externalregistration"
	"istio.io/istio/pilot/pkg/autoregistration/internal/health"
	"istio.io/istio/pilot/pkg/autoregistration/internal/state"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/queue"
	istiolog "istio.io/pkg/log"
)

const (
	// TODO use status or another proper API instead of annotations

	// AutoRegistrationGroupAnnotation on a WorkloadEntry stores the associated WorkloadGroup.
	AutoRegistrationGroupAnnotation = autoregistration.AutoRegistrationGroupAnnotation
	// WorkloadControllerAnnotation on a WorkloadEntry should store the current/last pilot instance connected to the workload for XDS.
	WorkloadControllerAnnotation = "istio.io/workloadController"
	// ConnectedAtAnnotation on a WorkloadEntry stores the time in nanoseconds when the associated workload connected to a Pilot instance.
	ConnectedAtAnnotation = "istio.io/connectedAt"
	// DisconnectedAtAnnotation on a WorkloadEntry stores the time in nanoseconds when the associated workload disconnected from a Pilot instance.
	DisconnectedAtAnnotation = "istio.io/disconnectedAt"

	timeFormat = time.RFC3339Nano
	// maxRetries is the number of times a service will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuings of a service.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms
	maxRetries = 5

	// convenience constant for better readability
	periodicCleanup = true
)

var log = istiolog.RegisterScope("wle", "wle controller debugging")

// Controller manages connect/disconnect/cleanup lifecycle of a workload.
type Controller struct {
	instanceID string
	// TODO move WorkloadEntry related tasks into their own object and give InternalGen a reference.
	// store should either be k8s (for running pilot) or in-memory (for tests). MCP and other config store implementations
	// do not support writing. We only use it here for reading WorkloadEntry/WorkloadGroup.
	store model.ConfigStoreController

	// Note: unregister is to update the workload entry status: like setting `DisconnectedAtAnnotation`
	// and make the workload entry enqueue `cleanupQueue`
	// cleanup is to delete the workload entry

	// queue contains workloadEntry that need to be unregistered
	queue controllers.Queue
	// cleanupLimit rate limit's auto registered WorkloadEntry cleanup calls to k8s
	cleanupLimit *rate.Limiter
	// cleanupQueue delays the cleanup of auto registered WorkloadEntries to allow for grace period
	cleanupQueue queue.Delayed

	mutex sync.Mutex
	// record the current adsConnections number
	// note: this is to handle reconnect to the same istiod, but in rare case the disconnect event is later than the connect event
	// keyed by proxy network+ip
	adsConnections map[string]uint8

	// maxConnectionAge is a duration that workload entry should be cleaned up if it does not reconnects.
	maxConnectionAge time.Duration

	stateStore           *state.Store
	healthController     *health.Controller
	autoregistration     *autoregistration.Controller
	externalregistration *externalregistration.Controller
}

// WorkloadEntryRegistrationStrategy represents a strategy of WorkloadEntry registration, e.g.
// auto-registration or external registration.
type WorkloadEntryRegistrationStrategy interface {
	// OnWorkloadConnect is called on workload connect to update the respective WorkloadEntry resource
	// appropriately.
	OnWorkloadConnect(entryName, entryNs string, proxy *model.Proxy, conTime time.Time) error
	// OnWorkloadDisconnect is called on workload disconnect to update the respective WorkloadEntry resource
	// appropriately.
	OnWorkloadDisconnect(entryName, entryNs string, disconTime, origConTime time.Time) (changed bool, err error)

	WorkloadEntryCleaner
}

// WorkloadEntryCleaner knows how to cleanup WorkloadEntry(s) managed by this controller.
//
// E.g. in the case of auto-registration it is necessary to remove WorkloadEntry resource completely,
// while in the case of health-checked WorkloadEntry(s) that are not using auto-registration it is
// necessary to remove the health condition only.
type WorkloadEntryCleaner interface {
	// GetCleanupGracePeriod returns a grace period to wait prior to cleanup.
	GetCleanupGracePeriod() time.Duration
	// ShouldCleanup returns true if a given WorkloadEntry is eligible for cleanup.
	ShouldCleanup(wle *config.Config) bool
	// Cleanup performs respective cleanup actions on a given WorkloadEntry.
	Cleanup(wle *config.Config, periodic bool)
}

type HealthEvent = health.HealthEvent

// NewController create a controller which manages workload lifecycle and health status.
func NewController(store model.ConfigStoreController, instanceID string, maxConnAge time.Duration) *Controller {
	if features.WorkloadEntryAutoRegistration || features.WorkloadEntryHealthChecks {
		if maxConnAge != math.MaxInt64 {
			maxConnAge += maxConnAge / 2
			// if overflow, set it to max int64
			if maxConnAge < 0 {
				maxConnAge = time.Duration(math.MaxInt64)
			}
		}
		c := &Controller{
			instanceID:       instanceID,
			store:            store,
			cleanupLimit:     rate.NewLimiter(rate.Limit(20), 1),
			cleanupQueue:     queue.NewDelayed(),
			adsConnections:   map[string]uint8{},
			maxConnectionAge: maxConnAge,
		}
		c.queue = controllers.NewQueue("unregister_workloadentry",
			controllers.WithMaxAttempts(maxRetries),
			controllers.WithGenericReconciler(c.unregisterWorkload))
		c.stateStore = state.NewStore(store, c)
		c.healthController = health.NewController(c.stateStore, maxRetries)
		c.autoregistration = autoregistration.NewController(store, c)
		c.externalregistration = externalregistration.NewController(c.stateStore, c)
		return c
	}
	return nil
}

func (c *Controller) Run(stop <-chan struct{}) {
	if c == nil {
		return
	}
	if c.store != nil && c.cleanupQueue != nil {
		go c.periodicWorkloadEntryCleanup(stop)
		go c.cleanupQueue.Run(stop)
	}

	go c.queue.Run(stop)
	go c.healthController.Run(stop)
	<-stop
}

// workItem contains the state of a "disconnect" event used to unregister a workload.
type workItem struct {
	entryName      string
	autoRegistered bool
	proxy          *model.Proxy
	disConTime     time.Time
	origConTime    time.Time
}

func setConnectMeta(c *config.Config, controller string, conTime time.Time) {
	if c.Annotations == nil {
		c.Annotations = map[string]string{}
	}
	c.Annotations[WorkloadControllerAnnotation] = controller
	c.Annotations[ConnectedAtAnnotation] = conTime.Format(timeFormat)
	delete(c.Annotations, DisconnectedAtAnnotation)
}

// RegisterWorkload determines whether a connecting proxy represents a non-Kubernetes
// workload and, if that's the case, initiates special processing required for that type
// of workloads, such as auto-registration, health status updates, etc.
//
// If connecting proxy represents a workload that is using auto-registration, it will
// create a WorkloadEntry resource automatically and be ready to receive health status
// updates.
//
// If connecting proxy represents a workload that is not using auto-registration,
// the WorkloadEntry resource is expected to exist beforehand. Otherwise, no special
// processing will be initiated, e.g. health status updates will be ignored.
func (c *Controller) RegisterWorkload(proxy *model.Proxy, conTime time.Time) error {
	if c == nil {
		return nil
	}
	var entryName string
	var autoRegistered bool
	if c.autoregistration.IsApplicableTo(proxy) {
		entryName = c.autoregistration.GenerateWorkloadEntryName(proxy)
		autoRegistered = true
	} else if c.externalregistration.IsApplicableTo(proxy) {
		wleName, err := c.externalregistration.GetWorkloadEntryName(proxy)
		if err != nil {
			return err
		}
		entryName = wleName
	}
	if entryName == "" {
		return nil
	}
	proxy.WorkloadEntryName = entryName
	proxy.WorkloadEntryAutoRegistered = autoRegistered

	c.mutex.Lock()
	c.adsConnections[makeProxyKey(proxy)]++
	c.mutex.Unlock()

	strategy := c.getRegistrationStrategy(autoRegistered)
	err := strategy.OnWorkloadConnect(entryName, proxy.Metadata.Namespace, proxy, conTime)
	if err != nil {
		log.Error(err)
	}
	return err
}

func (c *Controller) getRegistrationStrategy(autoRegistered bool) WorkloadEntryRegistrationStrategy {
	if autoRegistered {
		return c.autoregistration
	}
	return c.externalregistration
}

// GetWorkloadEntry implements externalregistration.ControllerCallbacks.
func (c *Controller) GetWorkloadEntry(entryName, entryNs string) *config.Config {
	return c.store.Get(gvk.WorkloadEntry, entryName, entryNs)
}

// CreateConnectedWorkloadEntry implements autoregistration.ControllerCallbacks.
func (c *Controller) CreateConnectedWorkloadEntry(wle config.Config, conTime time.Time) error {
	setConnectMeta(&wle, c.instanceID, conTime)
	_, err := c.store.Create(wle)
	return err
}

// ChangeWorkloadEntryStateToConnected implements autoregistration.ControllerCallbacks.
// ChangeWorkloadEntryStateToConnected implements externalregistration.ControllerCallbacks.
//
// Updates given WorkloadEntry to reflect that it is now connected to this particular `istiod` instance.
func (c *Controller) ChangeWorkloadEntryStateToConnected(entryName, entryNs string, conTime time.Time) (bool, error) {
	wle := c.store.Get(gvk.WorkloadEntry, entryName, entryNs)
	if wle == nil {
		return false, fmt.Errorf("failed updating WorkloadEntry %s/%s: WorkloadEntry not found", entryNs, entryName)
	}
	lastConTime, _ := time.Parse(timeFormat, wle.Annotations[ConnectedAtAnnotation])
	// the proxy has reconnected to another pilot, not belong to this one.
	if conTime.Before(lastConTime) {
		return false, nil
	}
	// Try to patch, if it fails then try to create
	_, err := c.store.Patch(*wle, func(cfg config.Config) (config.Config, kubetypes.PatchType) {
		setConnectMeta(&cfg, c.instanceID, conTime)
		return cfg, kubetypes.MergePatchType
	})
	if err != nil {
		return false, fmt.Errorf("failed updating WorkloadEntry %s/%s err: %v", entryNs, entryName, err)
	}
	return true, nil
}

// ChangeWorkloadEntryStateToConnected implements autoregistration.ControllerCallbacks.
// ChangeWorkloadEntryStateToConnected implements externalregistration.ControllerCallbacks.
//
// Updates given WorkloadEntry to reflect that it is no longer connected to this particular `istiod` instance.
func (c *Controller) ChangeWorkloadEntryStateToDisconnected(entryName, entryNs string, disconTime, origConnTime time.Time) (bool, error) {
	// unset controller, set disconnect time
	cfg := c.store.Get(gvk.WorkloadEntry, entryName, entryNs)
	if cfg == nil {
		log.Infof("workloadentry %s/%s is not found, maybe deleted or because of propagate latency",
			entryNs, entryName)
		// return error and backoff retry to prevent workloadentry leak
		return false, fmt.Errorf("workloadentry %s/%s is not found", entryNs, entryName)
	}

	// only queue a delete if this disconnect event is associated with the last connect event written to the workload entry
	if mostRecentConn, err := time.Parse(timeFormat, cfg.Annotations[ConnectedAtAnnotation]); err == nil {
		if mostRecentConn.After(origConnTime) {
			// this disconnect event wasn't processed until after we successfully reconnected
			return false, nil
		}
	}
	// The wle has reconnected to another istiod and controlled by it.
	if !c.IsControllerOf(cfg) {
		return false, nil
	}

	conTime, _ := time.Parse(timeFormat, cfg.Annotations[ConnectedAtAnnotation])
	// The wle has reconnected to this istiod,
	// this may happen when the unregister fails retry
	if disconTime.Before(conTime) {
		return false, nil
	}

	wle := cfg.DeepCopy()
	delete(wle.Annotations, ConnectedAtAnnotation)
	wle.Annotations[DisconnectedAtAnnotation] = disconTime.Format(timeFormat)
	// use update instead of patch to prevent race condition
	_, err := c.store.Update(wle)
	if err != nil {
		return false, fmt.Errorf("disconnect: failed updating WorkloadEntry %s/%s: %v", entryNs, entryName, err)
	}
	return true, nil
}

// QueueUnregisterWorkload determines whether a connected proxy represents a non-Kubernetes
// workload and, if that's the case, terminates special processing required for that type
// of workloads, such as auto-registration, health status updates, etc.
//
// If proxy represents a workload (be it auto-registered or not), WorkloadEntry resource
// will be updated to reflect that the proxy is no longer connected to this particular `istiod`
// instance.
//
// Besides that, if proxy represents a workload that is using auto-registration, WorkloadEntry
// resource will be scheduled for removal if the proxy does not reconnect within a grace period.
//
// If proxy represents a workload that is not using auto-registration, WorkloadEntry resource
// will be scheduled to be marked unhealthy if the proxy does not reconnect within a grace period.
func (c *Controller) QueueUnregisterWorkload(proxy *model.Proxy, origConnect time.Time) {
	if c == nil {
		return
	}
	if !features.WorkloadEntryAutoRegistration && !features.WorkloadEntryHealthChecks {
		return
	}
	// check if the WE already exists, update the status
	entryName := proxy.WorkloadEntryName
	if entryName == "" {
		return
	}

	c.mutex.Lock()
	proxyKey := makeProxyKey(proxy)
	num := c.adsConnections[proxyKey]
	// if there is still ads connection, do not unregister.
	if num > 1 {
		c.adsConnections[proxyKey] = num - 1
		c.mutex.Unlock()
		return
	}
	delete(c.adsConnections, proxyKey)
	c.mutex.Unlock()

	workload := &workItem{
		entryName:      entryName,
		autoRegistered: proxy.WorkloadEntryAutoRegistered,
		proxy:          proxy,
		disConTime:     time.Now(),
		origConTime:    origConnect,
	}
	// queue has max retry itself
	c.queue.Add(workload)
}

func (c *Controller) unregisterWorkload(item any) error {
	workItem, ok := item.(*workItem)
	if !ok {
		return nil
	}
	entryName, entryNs := workItem.entryName, workItem.proxy.Metadata.Namespace

	strategy := c.getRegistrationStrategy(workItem.autoRegistered)

	changed, err := strategy.OnWorkloadDisconnect(entryName, entryNs, workItem.disConTime, workItem.origConTime)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	// after grace period, check if the workload ever reconnected
	c.cleanupQueue.PushDelayed(c.newCleanupTask(entryName, entryNs, strategy, !periodicCleanup), strategy.GetCleanupGracePeriod())
	return nil
}

func (c *Controller) newCleanupTask(entryName, entryNs string, cleaner WorkloadEntryCleaner, periodic bool) queue.Task {
	return func() error {
		wle := c.store.Get(gvk.WorkloadEntry, entryName, entryNs)
		if wle == nil {
			return nil
		}
		if !cleaner.ShouldCleanup(wle) {
			return nil
		}
		if err := c.cleanupLimit.Wait(context.TODO()); err != nil {
			log.Errorf("error in WorkloadEntry cleanup rate limiter: %v", err)
			return nil
		}
		cleaner.Cleanup(wle, periodic)
		return nil
	}
}

// QueueWorkloadEntryHealth enqueues the associated WorkloadEntries health status.
func (c *Controller) QueueWorkloadEntryHealth(proxy *model.Proxy, event HealthEvent) {
	if !features.WorkloadEntryHealthChecks {
		return
	}
	c.healthController.QueueWorkloadEntryHealth(proxy, event)
}

// periodicWorkloadEntryCleanup checks lists all WorkloadEntry
func (c *Controller) periodicWorkloadEntryCleanup(stopCh <-chan struct{}) {
	if !features.WorkloadEntryAutoRegistration && !features.WorkloadEntryHealthChecks {
		return
	}
	ticker := time.NewTicker(10 * features.WorkloadEntryCleanupGracePeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			wles := c.store.List(gvk.WorkloadEntry, metav1.NamespaceAll)
			for _, wle := range wles {
				wle := wle
				if c.autoregistration.ShouldCleanup(&wle) {
					c.cleanupQueue.Push(c.newCleanupTask(wle.Name, wle.Namespace, c.autoregistration, periodicCleanup))
					continue
				}
				if c.externalregistration.ShouldCleanup(&wle) {
					c.cleanupQueue.Push(c.newCleanupTask(wle.Name, wle.Namespace, c.externalregistration, periodicCleanup))
				}
			}
		case <-stopCh:
			return
		}
	}
}

// IsExpired implements autoregistration.ControllerCallbacks.
// IsExpired implements externalregistration.ControllerCallbacks.
//
// IsExpired returns true if a given WorkloadEntry is eligible for cleanup.
func (c *Controller) IsExpired(wle *config.Config, cleanupGracePeriod time.Duration) bool {
	// If there is ConnectedAtAnnotation set, don't cleanup this workload entry.
	// This may happen when the workload fast reconnects to the same istiod.
	// 1. disconnect: the workload entry has been updated
	// 2. connect: but the patch is based on the old workloadentry because of the propagation latency.
	// So in this case the `DisconnectedAtAnnotation` is still there and the cleanup procedure will go on.
	connTime := wle.Annotations[ConnectedAtAnnotation]
	if connTime != "" {
		// handle workload leak when both workload/pilot down at the same time before pilot has a chance to set disconnTime
		connAt, err := time.Parse(timeFormat, connTime)
		if err == nil && uint64(time.Since(connAt)) > uint64(c.maxConnectionAge) {
			return true
		}
		return false
	}

	disconnTime := wle.Annotations[DisconnectedAtAnnotation]
	if disconnTime == "" {
		return false
	}

	disconnAt, err := time.Parse(timeFormat, disconnTime)
	// if we haven't passed the grace period, don't cleanup
	if err == nil && time.Since(disconnAt) < cleanupGracePeriod {
		return false
	}

	return true
}

// IsControlled implements externalregistration.ControllerCallbacks.
//
// IsControlled returns true if a given WorkloadEntry is/was connected to one of the `istiod` instances.
func (c *Controller) IsControlled(wle *config.Config) bool {
	if wle == nil {
		return false
	}
	return wle.Annotations[WorkloadControllerAnnotation] != ""
}

// IsControllerOf implements state.StoreCallbacks.
func (c *Controller) IsControllerOf(wle *config.Config) bool {
	if wle == nil {
		return false
	}
	return wle.Annotations[WorkloadControllerAnnotation] == c.instanceID
}

func makeProxyKey(proxy *model.Proxy) string {
	key := strings.Join([]string{
		string(proxy.Metadata.Network),
		proxy.IPAddresses[0],
		proxy.WorkloadEntryName,
		proxy.Metadata.Namespace,
	}, "~")
	return key
}
