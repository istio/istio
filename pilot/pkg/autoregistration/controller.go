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
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/autoregistration/internal/health"
	"istio.io/istio/pilot/pkg/autoregistration/internal/state"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/status"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/controllers"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/queue"
)

func init() {
	monitoring.MustRegister(autoRegistrationSuccess)
	monitoring.MustRegister(autoRegistrationUpdates)
	monitoring.MustRegister(autoRegistrationUnregistrations)
	monitoring.MustRegister(autoRegistrationDeletes)
	monitoring.MustRegister(autoRegistrationErrors)
}

var (
	autoRegistrationSuccess = monitoring.NewSum(
		"auto_registration_success_total",
		"Total number of successful auto registrations.",
	)

	autoRegistrationUpdates = monitoring.NewSum(
		"auto_registration_updates_total",
		"Total number of auto registration updates.",
	)

	autoRegistrationUnregistrations = monitoring.NewSum(
		"auto_registration_unregister_total",
		"Total number of unregistrations.",
	)

	autoRegistrationDeletes = monitoring.NewSum(
		"auto_registration_deletes_total",
		"Total number of auto registration cleaned up by periodic timer.",
	)

	autoRegistrationErrors = monitoring.NewSum(
		"auto_registration_errors_total",
		"Total number of auto registration errors.",
	)
)

const (
	// TODO use status or another proper API instead of annotations

	// AutoRegistrationGroupAnnotation on a WorkloadEntry stores the associated WorkloadGroup.
	AutoRegistrationGroupAnnotation = "istio.io/autoRegistrationGroup"
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
)

var log = istiolog.RegisterScope("wle", "wle controller debugging")

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

	stateStore       *state.Store
	healthController *health.Controller
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
	entryName   string
	autoCreated bool
	proxy       *model.Proxy
	disConTime  time.Time
	origConTime time.Time
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
	var autoCreate bool
	if features.WorkloadEntryAutoRegistration && proxy.Metadata.AutoRegisterGroup != "" {
		entryName = autoregisteredWorkloadEntryName(proxy)
		autoCreate = true
	} else if features.WorkloadEntryHealthChecks && proxy.Metadata.WorkloadEntry != "" {
		// a non-empty value of the `WorkloadEntry` field indicates that proxy must correspond to the WorkloadEntry
		wle := c.store.Get(gvk.WorkloadEntry, proxy.Metadata.WorkloadEntry, proxy.Metadata.Namespace)
		if wle == nil {
			// either invalid proxy configuration or config propagation delay
			return fmt.Errorf("proxy metadata indicates that it must correspond to an existing WorkloadEntry, "+
				"however WorkloadEntry %s/%s is not found", proxy.Metadata.Namespace, proxy.Metadata.WorkloadEntry)
		}
		if health.IsEligibleForHealthStatusUpdates(wle) {
			entryName = wle.Name
		}
	}
	if entryName == "" {
		return nil
	}
	proxy.WorkloadEntryName = entryName
	proxy.WorkloadEntryAutoCreated = autoCreate

	c.mutex.Lock()
	c.adsConnections[makeProxyKey(proxy)]++
	c.mutex.Unlock()

	err := c.onWorkloadConnect(entryName, proxy, conTime, autoCreate)
	if err != nil {
		log.Error(err)
	}
	return err
}

// onWorkloadConnect creates/updates WorkloadEntry of the connecting workload.
//
// If workload is using auto-registration, WorkloadEntry will be created automatically.
//
// If workload is not using auto-registration, WorkloadEntry must already exist.
func (c *Controller) onWorkloadConnect(entryName string, proxy *model.Proxy, conTime time.Time, autoCreate bool) error {
	if autoCreate {
		return c.registerWorkload(entryName, proxy, conTime)
	}
	return c.becomeControllerOf(entryName, proxy, conTime)
}

// becomeControllerOf updates an existing WorkloadEntry of a workload that is not using
// auto-registration.
func (c *Controller) becomeControllerOf(entryName string, proxy *model.Proxy, conTime time.Time) error {
	changed, err := c.changeWorkloadEntryStateToConnected(entryName, proxy, conTime)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	log.Infof("updated health-checked WorkloadEntry %s/%s", proxy.Metadata.Namespace, entryName)
	return nil
}

// registerWorkload creates or updates a WorkloadEntry of a workload that is using
// auto-registration.
func (c *Controller) registerWorkload(entryName string, proxy *model.Proxy, conTime time.Time) error {
	wle := c.store.Get(gvk.WorkloadEntry, entryName, proxy.Metadata.Namespace)
	if wle != nil {
		changed, err := c.changeWorkloadEntryStateToConnected(entryName, proxy, conTime)
		if err != nil {
			autoRegistrationErrors.Increment()
			return err
		}
		if !changed {
			return nil
		}
		autoRegistrationUpdates.Increment()
		log.Infof("updated auto-registered WorkloadEntry %s/%s", proxy.Metadata.Namespace, entryName)
		return nil
	}

	// No WorkloadEntry, create one using fields from the associated WorkloadGroup
	groupCfg := c.store.Get(gvk.WorkloadGroup, proxy.Metadata.AutoRegisterGroup, proxy.Metadata.Namespace)
	if groupCfg == nil {
		autoRegistrationErrors.Increment()
		return grpcstatus.Errorf(codes.FailedPrecondition, "auto-registration WorkloadEntry of %v failed: cannot find WorkloadGroup %s/%s",
			proxy.ID, proxy.Metadata.Namespace, proxy.Metadata.AutoRegisterGroup)
	}
	entry := workloadEntryFromGroup(entryName, proxy, groupCfg)
	setConnectMeta(entry, c.instanceID, conTime)
	_, err := c.store.Create(*entry)
	if err != nil {
		autoRegistrationErrors.Increment()
		return fmt.Errorf("auto-registration WorkloadEntry of %v failed: error creating WorkloadEntry: %v", proxy.ID, err)
	}
	hcMessage := ""
	if health.IsEligibleForHealthStatusUpdates(entry) {
		hcMessage = " with health checking enabled"
	}
	autoRegistrationSuccess.Increment()
	log.Infof("auto-registered WorkloadEntry %s/%s%s", proxy.Metadata.Namespace, entryName, hcMessage)
	return nil
}

// changeWorkloadEntryStateToConnected updates given WorkloadEntry to reflect that
// it is now connected to this particular `istiod` instance.
func (c *Controller) changeWorkloadEntryStateToConnected(entryName string, proxy *model.Proxy, conTime time.Time) (bool, error) {
	wle := c.store.Get(gvk.WorkloadEntry, entryName, proxy.Metadata.Namespace)
	if wle == nil {
		return false, fmt.Errorf("failed updating WorkloadEntry %s/%s: WorkloadEntry not found", proxy.Metadata.Namespace, entryName)
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
		return false, fmt.Errorf("failed updating WorkloadEntry %s/%s err: %v", proxy.Metadata.Namespace, entryName, err)
	}
	return true, nil
}

// changeWorkloadEntryStateToDisconnected updates given WorkloadEntry to reflect that
// it is no longer connected to this particular `istiod` instance.
func (c *Controller) changeWorkloadEntryStateToDisconnected(entryName string, proxy *model.Proxy, disconTime, origConnTime time.Time) (bool, error) {
	// unset controller, set disconnect time
	cfg := c.store.Get(gvk.WorkloadEntry, entryName, proxy.Metadata.Namespace)
	if cfg == nil {
		log.Infof("workloadentry %s/%s is not found, maybe deleted or because of propagate latency",
			proxy.Metadata.Namespace, entryName)
		// return error and backoff retry to prevent workloadentry leak
		return false, fmt.Errorf("workloadentry %s/%s is not found", proxy.Metadata.Namespace, entryName)
	}

	// only queue a delete if this disconnect event is associated with the last connect event written to the workload entry
	if mostRecentConn, err := time.Parse(timeFormat, cfg.Annotations[ConnectedAtAnnotation]); err == nil {
		if mostRecentConn.After(origConnTime) {
			// this disconnect event wasn't processed until after we successfully reconnected
			return false, nil
		}
	}
	// The wle has reconnected to another istiod and controlled by it.
	if cfg.Annotations[WorkloadControllerAnnotation] != c.instanceID {
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
		return false, fmt.Errorf("disconnect: failed updating WorkloadEntry %s/%s: %v", proxy.Metadata.Namespace, entryName, err)
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
		entryName:   entryName,
		autoCreated: proxy.WorkloadEntryAutoCreated,
		proxy:       proxy,
		disConTime:  time.Now(),
		origConTime: origConnect,
	}
	// queue has max retry itself
	c.queue.Add(workload)
}

func (c *Controller) unregisterWorkload(item any) error {
	workItem, ok := item.(*workItem)
	if !ok {
		return nil
	}

	changed, err := c.changeWorkloadEntryStateToDisconnected(workItem.entryName, workItem.proxy, workItem.disConTime, workItem.origConTime)
	if err != nil {
		autoRegistrationErrors.Increment()
		return err
	}
	if !changed {
		return nil
	}

	if workItem.autoCreated {
		autoRegistrationUnregistrations.Increment()
	}

	// after grace period, check if the workload ever reconnected
	ns := workItem.proxy.Metadata.Namespace
	c.cleanupQueue.PushDelayed(func() error {
		wle := c.store.Get(gvk.WorkloadEntry, workItem.entryName, ns)
		if wle == nil {
			return nil
		}
		if c.shouldCleanupEntry(*wle) {
			c.cleanupEntry(*wle, false)
		}
		return nil
	}, features.WorkloadEntryCleanupGracePeriod)
	return nil
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
				if c.shouldCleanupEntry(wle) {
					c.cleanupQueue.Push(func() error {
						c.cleanupEntry(wle, true)
						return nil
					})
				}
			}
		case <-stopCh:
			return
		}
	}
}

func (c *Controller) shouldCleanupEntry(wle config.Config) bool {
	// don't clean up if WorkloadEntry is neither auto-registered
	// nor health-checked
	if !isAutoRegisteredWorkloadEntry(&wle) &&
		!(isHealthCheckedWorkloadEntry(&wle) && health.HasHealthCondition(&wle)) {
		return false
	}

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
	if err == nil && time.Since(disconnAt) < features.WorkloadEntryCleanupGracePeriod {
		return false
	}

	return true
}

// cleanupEntry performs clean-up actions on a WorkloadEntry of a proxy that hasn't
// reconnected within a grace period.
func (c *Controller) cleanupEntry(wle config.Config, periodic bool) {
	if err := c.cleanupLimit.Wait(context.TODO()); err != nil {
		log.Errorf("error in WorkloadEntry cleanup rate limiter: %v", err)
		return
	}
	if isAutoRegisteredWorkloadEntry(&wle) {
		c.deleteEntry(wle, periodic)
		return
	}
	if isHealthCheckedWorkloadEntry(&wle) && health.HasHealthCondition(&wle) {
		c.deleteHealthCondition(wle, periodic)
		return
	}
}

// deleteEntry removes WorkloadEntry that was created automatically for a workload
// that is using auto-registration.
func (c *Controller) deleteEntry(wle config.Config, periodic bool) {
	if err := c.store.Delete(gvk.WorkloadEntry, wle.Name, wle.Namespace, &wle.ResourceVersion); err != nil && !errors.IsNotFound(err) {
		log.Warnf("failed cleaning up auto-registered WorkloadEntry %s/%s: %v", wle.Namespace, wle.Name, err)
		autoRegistrationErrors.Increment()
		return
	}
	autoRegistrationDeletes.Increment()
	log.Infof("cleaned up auto-registered WorkloadEntry %s/%s periodic:%v", wle.Namespace, wle.Name, periodic)
}

// deleteHealthCondition updates WorkloadEntry of a workload that is not using auto-registration
// to remove information about the health status (since we can no longer be certain about it).
func (c *Controller) deleteHealthCondition(wle config.Config, periodic bool) {
	err := c.stateStore.DeleteHealthCondition(wle)
	if err != nil {
		log.Warnf("failed cleaning up health-checked WorkloadEntry %s/%s: %v", wle.Namespace, wle.Name, err)
		return
	}
	log.Infof("cleaned up health-checked WorkloadEntry %s/%s periodic:%v", wle.Namespace, wle.Name, periodic)
}

// IsControllerOf implements state.StoreCallbacks.
func (c *Controller) IsControllerOf(wle *config.Config) bool {
	if wle == nil {
		return false
	}
	return wle.Annotations[WorkloadControllerAnnotation] == c.instanceID
}

func autoregisteredWorkloadEntryName(proxy *model.Proxy) string {
	if proxy.Metadata.AutoRegisterGroup == "" {
		return ""
	}
	if len(proxy.IPAddresses) == 0 {
		log.Errorf("auto-registration of %v failed: missing IP addresses", proxy.ID)
		return ""
	}
	if len(proxy.Metadata.Namespace) == 0 {
		log.Errorf("auto-registration of %v failed: missing namespace", proxy.ID)
		return ""
	}
	p := []string{proxy.Metadata.AutoRegisterGroup, sanitizeIP(proxy.IPAddresses[0])}
	if proxy.Metadata.Network != "" {
		p = append(p, string(proxy.Metadata.Network))
	}

	name := strings.Join(p, "-")
	if len(name) > 253 {
		name = name[len(name)-253:]
		log.Warnf("generated WorkloadEntry name is too long, consider making the WorkloadGroup name shorter. Shortening from beginning to: %s", name)
	}
	return name
}

// sanitizeIP ensures an IP address (IPv6) can be used in Kubernetes resource name
func sanitizeIP(s string) string {
	return strings.ReplaceAll(s, ":", "-")
}

func mergeLabels(labels ...map[string]string) map[string]string {
	if len(labels) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(labels)*len(labels[0]))
	for _, lm := range labels {
		for k, v := range lm {
			out[k] = v
		}
	}
	return out
}

var workloadGroupIsController = true

func workloadEntryFromGroup(name string, proxy *model.Proxy, groupCfg *config.Config) *config.Config {
	group := groupCfg.Spec.(*v1alpha3.WorkloadGroup)
	entry := group.Template.DeepCopy()
	entry.Address = proxy.IPAddresses[0]
	// TODO move labels out of entry
	// node metadata > WorkloadGroup.Metadata > WorkloadGroup.Template
	if group.Metadata != nil && group.Metadata.Labels != nil {
		entry.Labels = mergeLabels(entry.Labels, group.Metadata.Labels)
	}
	// Explicitly do not use proxy.Labels, as it is only initialized *after* we register the workload,
	// and it would be circular, as it will set the labels based on the WorkloadEntry -- but we are creating
	// the workload entry.
	if proxy.Metadata.Labels != nil {
		entry.Labels = mergeLabels(entry.Labels, proxy.Metadata.Labels)
		// the label has been converted to "istio-locality: region/zone/subzone"
		// in pilot/pkg/xds/ads.go, and `/` is not allowed in k8s label value.
		// Instead of converting again, we delete it since has set WorkloadEntry.Locality
		delete(entry.Labels, model.LocalityLabel)
	}

	annotations := map[string]string{AutoRegistrationGroupAnnotation: groupCfg.Name}
	if group.Metadata != nil && group.Metadata.Annotations != nil {
		annotations = mergeLabels(annotations, group.Metadata.Annotations)
	}

	if proxy.Metadata.Network != "" {
		entry.Network = string(proxy.Metadata.Network)
	}
	if proxy.Locality != nil {
		entry.Locality = util.LocalityToString(proxy.Locality)
	}
	if proxy.Metadata.ProxyConfig != nil && proxy.Metadata.ProxyConfig.ReadinessProbe != nil {
		annotations[status.WorkloadEntryHealthCheckAnnotation] = "true"
	}
	return &config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.WorkloadEntry,
			Name:             name,
			Namespace:        proxy.Metadata.Namespace,
			Labels:           entry.Labels,
			Annotations:      annotations,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: groupCfg.GroupVersionKind.GroupVersion(),
				Kind:       groupCfg.GroupVersionKind.Kind,
				Name:       groupCfg.Name,
				UID:        kubetypes.UID(groupCfg.UID),
				Controller: &workloadGroupIsController,
			}},
		},
		Spec: entry,
		// TODO status fields used for garbage collection
		Status: nil,
	}
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

func isAutoRegisteredWorkloadEntry(wle *config.Config) bool {
	return wle != nil && wle.Annotations[AutoRegistrationGroupAnnotation] != ""
}

func isHealthCheckedWorkloadEntry(wle *config.Config) bool {
	return wle != nil && wle.Annotations[WorkloadControllerAnnotation] != "" && !isAutoRegisteredWorkloadEntry(wle)
}
