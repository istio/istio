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
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"

	"istio.io/api/annotation"
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
	pm "istio.io/istio/pkg/model"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/queue"
)

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

	// Note: unregister is to update the workload entry status: like setting `istio.io/disconnectedAt`
	// and make the workload entry enqueue `cleanupQueue`
	// cleanup is to delete the workload entry

	// queue contains workloadEntry that need to be unregistered
	queue controllers.Queue
	// cleanupLimit rate limit's auto registered WorkloadEntry cleanup calls to k8s
	cleanupLimit *rate.Limiter
	// cleanupQueue delays the cleanup of auto registered WorkloadEntries to allow for grace period
	cleanupQueue queue.Delayed

	adsConnections        *adsConnections
	lateRegistrationQueue controllers.Queue

	// maxConnectionAge is a duration that workload entry should be cleaned up if it does not reconnects.
	maxConnectionAge time.Duration

	stateStore       *state.Store
	healthController *health.Controller
}

type HealthEvent = health.HealthEvent

// NewController create a controller which manages workload lifecycle and health status.
func NewController(store model.ConfigStoreController, instanceID string, maxConnAge time.Duration) *Controller {
	if !features.WorkloadEntryAutoRegistration && !features.WorkloadEntryHealthChecks {
		return nil
	}

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
		adsConnections:   newAdsConnections(),
		maxConnectionAge: maxConnAge,
	}
	c.queue = controllers.NewQueue("unregister_workloadentry",
		controllers.WithMaxAttempts(maxRetries),
		controllers.WithGenericReconciler(c.unregisterWorkload))
	c.stateStore = state.NewStore(store, c)
	c.healthController = health.NewController(c.stateStore, maxRetries)
	c.setupAutoRecreate()
	return c
}

func (c *Controller) Run(stop <-chan struct{}) {
	if c == nil {
		return
	}
	if c.store != nil && c.cleanupQueue != nil {
		go c.periodicWorkloadEntryCleanup(stop)
		go c.cleanupQueue.Run(stop)
	}
	if features.WorkloadEntryAutoRegistration {
		go c.lateRegistrationQueue.Run(stop)
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

// setupAutoRecreate adds a handler to create entries for existing connections when a WG is added
func (c *Controller) setupAutoRecreate() {
	if !features.WorkloadEntryAutoRegistration {
		return
	}
	c.lateRegistrationQueue = controllers.NewQueue("auto-register existing connections",
		controllers.WithReconciler(func(key kubetypes.NamespacedName) error {
			log.Debugf("(%s) processing WorkloadGroup add for %s/%s", c.instanceID, key.Namespace, key.Name)
			// WorkloadGroup doesn't exist anymore, skip this.
			if c.store.Get(gvk.WorkloadGroup, key.Name, key.Namespace) == nil {
				return nil
			}
			conns := c.adsConnections.ConnectionsForGroup(key)
			for _, conn := range conns {
				proxy := conn.Proxy()
				entryName := autoregisteredWorkloadEntryName(proxy)
				if entryName == "" {
					continue
				}
				if err := c.registerWorkload(entryName, proxy, conn.ConnectedAt()); err != nil {
					log.Error(err)
				}
				proxy.SetWorkloadEntry(entryName, true)
			}
			return nil
		}))

	c.store.RegisterEventHandler(gvk.WorkloadGroup, func(_ config.Config, cfg config.Config, event model.Event) {
		if event == model.EventAdd {
			c.lateRegistrationQueue.Add(cfg.NamespacedName())
		}
	})
}

func setConnectMeta(c *config.Config, controller string, conTime time.Time) {
	if c.Annotations == nil {
		c.Annotations = map[string]string{}
	}
	c.Annotations[annotation.IoIstioWorkloadController.Name] = controller
	c.Annotations[annotation.IoIstioConnectedAt.Name] = conTime.Format(timeFormat)
	delete(c.Annotations, annotation.IoIstioDisconnectedAt.Name)
}

// OnConnect determines whether a connecting proxy represents a non-Kubernetes
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
func (c *Controller) OnConnect(conn connection) error {
	if c == nil {
		return nil
	}
	proxy := conn.Proxy()
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
			if err := ensureProxyCanControlEntry(proxy, wle); err != nil {
				return err
			}
			entryName = wle.Name
		}
	}
	if entryName == "" {
		return nil
	}

	proxy.SetWorkloadEntry(entryName, autoCreate)
	c.adsConnections.Connect(conn)

	err := c.onWorkloadConnect(entryName, proxy, conn.ConnectedAt(), autoCreate)
	if err != nil {
		log.Error(err)
	}
	return err
}

// ensureProxyCanControlEntry ensures the connected proxy's identity matches that of the WorkloadEntry it is associating with.
func ensureProxyCanControlEntry(proxy *model.Proxy, wle *config.Config) error {
	if !features.ValidateWorkloadEntryIdentity {
		// Validation disabled, skip
		return nil
	}
	if proxy.VerifiedIdentity == nil {
		return fmt.Errorf("registration of WorkloadEntry requires a verified identity")
	}
	if proxy.VerifiedIdentity.Namespace != wle.Namespace {
		return fmt.Errorf("registration of WorkloadEntry namespace mismatch: %q vs %q", proxy.VerifiedIdentity.Namespace, wle.Namespace)
	}
	spec := wle.Spec.(*v1alpha3.WorkloadEntry)
	if spec.ServiceAccount != "" && proxy.VerifiedIdentity.ServiceAccount != spec.ServiceAccount {
		return fmt.Errorf("registration of WorkloadEntry service account mismatch: %q vs %q", proxy.VerifiedIdentity.ServiceAccount, spec.ServiceAccount)
	}
	return nil
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
		if err := ensureProxyCanControlEntry(proxy, wle); err != nil {
			return err
		}
		changed, err := c.changeWorkloadEntryStateToConnected(entryName, proxy, conTime)
		if err != nil {
			autoRegistrationErrors.Increment()
			return err
		}
		if !changed {
			return nil
		}
		autoRegistrationUpdates.Increment()
		log.Infof("updated auto-registered WorkloadEntry %s/%s as connected", proxy.Metadata.Namespace, entryName)
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
	if err := ensureProxyCanControlEntry(proxy, entry); err != nil {
		return err
	}
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

	// check if this was actually disconnected AFTER this connTime
	// this check can miss, but when it does the `Update` will fail due to versioning
	// and retry. The retry includes this check and passes the next time.
	if timestamp, ok := wle.Annotations[annotation.IoIstioDisconnectedAt.Name]; ok {
		disconnTime, _ := time.Parse(timeFormat, timestamp)
		if conTime.Before(disconnTime) {
			// we slowly processed a connect and disconnected before getting to this point
			return false, nil
		}
	}

	lastConTime, _ := time.Parse(timeFormat, wle.Annotations[annotation.IoIstioConnectedAt.Name])
	// the proxy has reconnected to another pilot, not belong to this one.
	if conTime.Before(lastConTime) {
		return false, nil
	}
	// Try to update, if it fails we retry all the above logic since the WLE changed
	updated := wle.DeepCopy()
	setConnectMeta(&updated, c.instanceID, conTime)
	_, err := c.store.Update(updated)
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
	if mostRecentConn, err := time.Parse(timeFormat, cfg.Annotations[annotation.IoIstioConnectedAt.Name]); err == nil {
		if mostRecentConn.After(origConnTime) {
			// this disconnect event wasn't processed until after we successfully reconnected
			return false, nil
		}
	}
	// The wle has reconnected to another istiod and controlled by it.
	if cfg.Annotations[annotation.IoIstioWorkloadController.Name] != c.instanceID {
		return false, nil
	}

	conTime, _ := time.Parse(timeFormat, cfg.Annotations[annotation.IoIstioConnectedAt.Name])
	// The wle has reconnected to this istiod,
	// this may happen when the unregister fails retry
	if disconTime.Before(conTime) {
		return false, nil
	}

	wle := cfg.DeepCopy()
	delete(wle.Annotations, annotation.IoIstioConnectedAt.Name)
	wle.Annotations[annotation.IoIstioDisconnectedAt.Name] = disconTime.Format(timeFormat)
	// use update instead of patch to prevent race condition
	_, err := c.store.Update(wle)
	if err != nil {
		return false, fmt.Errorf("disconnect: failed updating WorkloadEntry %s/%s: %v", proxy.Metadata.Namespace, entryName, err)
	}
	return true, nil
}

// OnDisconnect determines whether a connected proxy represents a non-Kubernetes
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
func (c *Controller) OnDisconnect(conn connection) {
	if c == nil {
		return
	}
	if !features.WorkloadEntryAutoRegistration && !features.WorkloadEntryHealthChecks {
		return
	}
	proxy := conn.Proxy()
	// check if the WE already exists, update the status
	entryName, autoCreate := proxy.WorkloadEntry()
	if entryName == "" {
		return
	}

	// if there is still an ads connection, do not unregister.
	if remainingConnections := c.adsConnections.Disconnect(conn); remainingConnections {
		return
	}

	proxy.RLock()
	defer proxy.RUnlock()
	workload := &workItem{
		entryName:   entryName,
		autoCreated: autoCreate,
		proxy:       conn.Proxy(),
		disConTime:  time.Now(),
		origConTime: conn.ConnectedAt(),
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
	log.Infof("updated auto-registered WorkloadEntry %s/%s as disconnected", workItem.proxy.Metadata.Namespace, workItem.entryName)

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

	// If there is `istio.io/connectedAt` set, don't cleanup this workload entry.
	// This may happen when the workload fast reconnects to the same istiod.
	// 1. disconnect: the workload entry has been updated
	// 2. connect: but the patch is based on the old workloadentry because of the propagation latency.
	// So in this case the `istio.io/disconnectedAt` is still there and the cleanup procedure will go on.
	connTime := wle.Annotations[annotation.IoIstioConnectedAt.Name]
	if connTime != "" {
		// handle workload leak when both workload/pilot down at the same time before pilot has a chance to set disconnTime
		connAt, err := time.Parse(timeFormat, connTime)
		if err == nil && uint64(time.Since(connAt)) > uint64(c.maxConnectionAge) {
			return true
		}
		return false
	}

	disconnTime := wle.Annotations[annotation.IoIstioDisconnectedAt.Name]
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
	return wle.Annotations[annotation.IoIstioWorkloadController.Name] == c.instanceID
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
		delete(entry.Labels, pm.LocalityLabel)
	}

	annotations := map[string]string{annotation.IoIstioAutoRegistrationGroup.Name: groupCfg.Name}
	if group.Metadata != nil && group.Metadata.Annotations != nil {
		annotations = mergeLabels(annotations, group.Metadata.Annotations)
	}

	if proxy.Metadata.Network != "" {
		entry.Network = string(proxy.Metadata.Network)
	}
	// proxy.Locality can be unset when auto registration takes place, because
	// its state is not fully initialized. Therefore, we check the bootstrap
	// node.
	if proxy.XdsNode != nil && proxy.XdsNode.Locality != nil {
		entry.Locality = util.LocalityToString(proxy.XdsNode.Locality)
		log.Infof("Setting Locality: %s for WLE: %s via XdsNode", entry.Locality, name)
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

func isAutoRegisteredWorkloadEntry(wle *config.Config) bool {
	return wle != nil && wle.Annotations[annotation.IoIstioAutoRegistrationGroup.Name] != ""
}

func isHealthCheckedWorkloadEntry(wle *config.Config) bool {
	return wle != nil && wle.Annotations[annotation.IoIstioWorkloadController.Name] != "" && !isAutoRegisteredWorkloadEntry(wle)
}
