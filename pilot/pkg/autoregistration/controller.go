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
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"

	"istio.io/api/meta/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/status"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/queue"
	istiolog "istio.io/pkg/log"
	"istio.io/pkg/monitoring"
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
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries = 15
)

type HealthEvent struct {
	// whether or not the agent thought the target is healthy
	Healthy bool `json:"healthy,omitempty"`
	// error message propagated
	Message string `json:"errMessage,omitempty"`
}

type HealthCondition struct {
	proxy     *model.Proxy
	entryName string
	condition *v1alpha1.IstioCondition
}

var log = istiolog.RegisterScope("wle", "wle controller debugging", 0)

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

	// healthCondition is a fifo queue used for updating health check status
	healthCondition controllers.Queue
}

type HealthStatus = v1alpha1.IstioCondition

// NewController create a controller which manages workload lifecycle and health status.
func NewController(store model.ConfigStoreController, instanceID string, maxConnAge time.Duration) *Controller {
	if features.WorkloadEntryAutoRegistration || features.WorkloadEntryHealthChecks {
		maxConnAge := maxConnAge + maxConnAge/2
		// if overflow, set it to max int64
		if maxConnAge < 0 {
			maxConnAge = time.Duration(math.MaxInt64)
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
		c.healthCondition = controllers.NewQueue("healthcheck",
			controllers.WithMaxAttempts(maxRetries),
			controllers.WithGenericReconciler(c.updateWorkloadEntryHealth))
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
	go c.healthCondition.Run(stop)
	<-stop
}

// workItem contains the state of a "disconnect" event used to unregister a workload.
type workItem struct {
	entryName   string
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

func (c *Controller) RegisterWorkload(proxy *model.Proxy, conTime time.Time) error {
	if !features.WorkloadEntryAutoRegistration || c == nil {
		return nil
	}
	// check if the WE already exists, update the status
	entryName := autoregisteredWorkloadEntryName(proxy)
	if entryName == "" {
		return nil
	}
	proxy.AutoregisteredWorkloadEntryName = entryName

	c.mutex.Lock()
	c.adsConnections[makeProxyKey(proxy)]++
	c.mutex.Unlock()

	if err := c.registerWorkload(entryName, proxy, conTime); err != nil {
		log.Errorf(err)
		return err
	}
	return nil
}

func (c *Controller) registerWorkload(entryName string, proxy *model.Proxy, conTime time.Time) error {
	wle := c.store.Get(gvk.WorkloadEntry, entryName, proxy.Metadata.Namespace)
	if wle != nil {
		lastConTime, _ := time.Parse(timeFormat, wle.Annotations[ConnectedAtAnnotation])
		// the proxy has reconnected to another pilot, not belong to this one.
		if conTime.Before(lastConTime) {
			return nil
		}
		// Try to patch, if it fails then try to create
		_, err := c.store.Patch(*wle, func(cfg config.Config) (config.Config, kubetypes.PatchType) {
			setConnectMeta(&cfg, c.instanceID, conTime)
			return cfg, kubetypes.MergePatchType
		})
		if err != nil {
			return fmt.Errorf("failed updating WorkloadEntry %s/%s err: %v", proxy.Metadata.Namespace, entryName, err)
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
	if _, f := entry.Annotations[status.WorkloadEntryHealthCheckAnnotation]; f {
		hcMessage = " with health checking enabled"
	}
	autoRegistrationSuccess.Increment()
	log.Infof("auto-registered WorkloadEntry %s/%s%s", proxy.Metadata.Namespace, entryName, hcMessage)
	return nil
}

func (c *Controller) QueueUnregisterWorkload(proxy *model.Proxy, origConnect time.Time) {
	if !features.WorkloadEntryAutoRegistration || c == nil {
		return
	}
	// check if the WE already exists, update the status
	entryName := proxy.AutoregisteredWorkloadEntryName
	if entryName == "" {
		return
	}

	c.mutex.Lock()
	num := c.adsConnections[makeProxyKey(proxy)]
	// if there is still ads connection, do not unregister.
	if num > 1 {
		c.adsConnections[makeProxyKey(proxy)] = num - 1
		c.mutex.Unlock()
		return
	}
	delete(c.adsConnections, makeProxyKey(proxy))
	c.mutex.Unlock()

	workload := &workItem{
		entryName:   entryName,
		proxy:       proxy,
		disConTime:  time.Now(),
		origConTime: origConnect,
	}
	if err := c.unregisterWorkload(workload); err != nil {
		log.Errorf(err)
		c.queue.Add(workload)
	}
}

func (c *Controller) unregisterWorkload(item interface{}) error {
	workItem, ok := item.(*workItem)
	if !ok {
		return nil
	}

	// unset controller, set disconnect time
	cfg := c.store.Get(gvk.WorkloadEntry, workItem.entryName, workItem.proxy.Metadata.Namespace)
	if cfg == nil {
		// return error and backoff retry to prevent workloadentry leak
		// TODO(@hzxuzhonghu): update the Get interface, fallback to calling apiserver.
		return fmt.Errorf("workloadentry %s/%s is not found, maybe deleted or because of propagate latency",
			workItem.proxy.Metadata.Namespace, workItem.entryName)
	}

	// only queue a delete if this disconnect event is associated with the last connect event written to the worload entry
	if mostRecentConn, err := time.Parse(timeFormat, cfg.Annotations[ConnectedAtAnnotation]); err == nil {
		if mostRecentConn.After(workItem.origConTime) {
			// this disconnect event wasn't processed until after we successfully reconnected
			return nil
		}
	}
	// The wle has reconnected to another istiod and controlled by it.
	if cfg.Annotations[WorkloadControllerAnnotation] != c.instanceID {
		return nil
	}

	conTime, _ := time.Parse(timeFormat, cfg.Annotations[ConnectedAtAnnotation])
	// The wle has reconnected to this istiod,
	// this may happen when the unregister fails retry
	if workItem.disConTime.Before(conTime) {
		return nil
	}

	wle := cfg.DeepCopy()
	delete(wle.Annotations, ConnectedAtAnnotation)
	wle.Annotations[DisconnectedAtAnnotation] = workItem.disConTime.Format(timeFormat)
	// use update instead of patch to prevent race condition
	if _, err := c.store.Update(wle); err != nil {
		autoRegistrationErrors.Increment()
		return fmt.Errorf("disconnect: failed updating WorkloadEntry %s/%s: %v", workItem.proxy.Metadata.Namespace, workItem.entryName, err)
	}

	autoRegistrationUnregistrations.Increment()

	// after grace period, check if the workload ever reconnected
	ns := workItem.proxy.Metadata.Namespace
	c.cleanupQueue.PushDelayed(func() error {
		wle := c.store.Get(gvk.WorkloadEntry, workItem.entryName, ns)
		if wle == nil {
			return nil
		}
		if c.shouldCleanupEntry(*wle) {
			c.cleanupEntry(*wle)
		}
		return nil
	}, features.WorkloadEntryCleanupGracePeriod)
	return nil
}

// QueueWorkloadEntryHealth enqueues the associated WorkloadEntries health status.
func (c *Controller) QueueWorkloadEntryHealth(proxy *model.Proxy, event HealthEvent) {
	// we assume that the workload entry exists
	// if auto registration does not exist, try looking
	// up in NodeMetadata
	entryName := proxy.AutoregisteredWorkloadEntryName
	if entryName == "" {
		log.Errorf("unable to derive WorkloadEntry for health update for %v", proxy.ID)
		return
	}

	condition := transformHealthEvent(proxy, entryName, event)
	c.healthCondition.Add(condition)
}

// updateWorkloadEntryHealth updates the associated WorkloadEntries health status
// based on the corresponding health check performed by istio-agent.
func (c *Controller) updateWorkloadEntryHealth(obj interface{}) error {
	condition := obj.(HealthCondition)
	// get previous status
	cfg := c.store.Get(gvk.WorkloadEntry, condition.entryName, condition.proxy.Metadata.Namespace)
	if cfg == nil {
		return fmt.Errorf("failed to update health status for %v: WorkloadEntry %v not found", condition.proxy.ID, condition.entryName)
	}
	// The workloadentry has reconnected to the other istiod
	if cfg.Annotations[WorkloadControllerAnnotation] != c.instanceID {
		return nil
	}

	// check if the existing health status is newer than this one
	wleStatus, ok := cfg.Status.(*v1alpha1.IstioStatus)
	if ok {
		healthCondition := status.GetCondition(wleStatus.Conditions, status.ConditionHealthy)
		if healthCondition != nil {
			if healthCondition.LastProbeTime.AsTime().After(condition.condition.LastProbeTime.AsTime()) {
				return nil
			}
		}
	}

	// replace the updated status
	wle := status.UpdateConfigCondition(*cfg, condition.condition)
	// update the status
	_, err := c.store.UpdateStatus(wle)
	if err != nil {
		return fmt.Errorf("error while updating WorkloadEntry health status for %s: %v", condition.proxy.ID, err)
	}
	log.Debugf("updated health status of %v to %v", condition.proxy.ID, condition.condition)
	return nil
}

// periodicWorkloadEntryCleanup checks lists all WorkloadEntry
func (c *Controller) periodicWorkloadEntryCleanup(stopCh <-chan struct{}) {
	if !features.WorkloadEntryAutoRegistration {
		return
	}
	ticker := time.NewTicker(10 * features.WorkloadEntryCleanupGracePeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			wles, err := c.store.List(gvk.WorkloadEntry, metav1.NamespaceAll)
			if err != nil {
				log.Warnf("error listing WorkloadEntry for cleanup: %v", err)
				continue
			}
			for _, wle := range wles {
				wle := wle
				if c.shouldCleanupEntry(wle) {
					c.cleanupQueue.Push(func() error {
						c.cleanupEntry(wle)
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
	// don't clean-up if connected or non-autoregistered WorkloadEntries
	if wle.Annotations[AutoRegistrationGroupAnnotation] == "" {
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
		// if it has been 1.5*maxConnectionAge since workload connected, should delete it.
		if err == nil && uint64(time.Since(connAt)) > uint64(c.maxConnectionAge)+uint64(c.maxConnectionAge/2) {
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

func (c *Controller) cleanupEntry(wle config.Config) {
	if err := c.cleanupLimit.Wait(context.TODO()); err != nil {
		log.Errorf("error in WorkloadEntry cleanup rate limiter: %v", err)
		return
	}
	if err := c.store.Delete(gvk.WorkloadEntry, wle.Name, wle.Namespace, &wle.ResourceVersion); err != nil && !errors.IsNotFound(err) {
		log.Warnf("failed cleaning up auto-registered WorkloadEntry %s/%s: %v", wle.Namespace, wle.Name, err)
		autoRegistrationErrors.Increment()
		return
	}
	autoRegistrationDeletes.Increment()
	log.Infof("cleaned up auto-registered WorkloadEntry %s/%s", wle.Namespace, wle.Name)
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

func transformHealthEvent(proxy *model.Proxy, entryName string, event HealthEvent) HealthCondition {
	cond := &v1alpha1.IstioCondition{
		Type: status.ConditionHealthy,
		// last probe and transition are the same because
		// we only send on transition in the agent
		LastProbeTime:      timestamppb.Now(),
		LastTransitionTime: timestamppb.Now(),
	}
	out := HealthCondition{
		proxy:     proxy,
		entryName: entryName,
		condition: cond,
	}
	if event.Healthy {
		cond.Status = status.StatusTrue
		return out
	}
	cond.Status = status.StatusFalse
	cond.Message = event.Message
	return out
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
	if proxy.Metadata != nil && proxy.Metadata.Labels != nil {
		entry.Labels = mergeLabels(entry.Labels, proxy.Metadata.Labels)
	}

	annotations := map[string]string{AutoRegistrationGroupAnnotation: groupCfg.Name}
	if group.Metadata != nil && group.Metadata.Annotations != nil {
		annotations = mergeLabels(annotations, group.Metadata.Annotations)
	}

	if proxy.Metadata.Network != "" {
		entry.Network = string(proxy.Metadata.Network)
	}
	// proxy.Locality is unset when auto registration takes place, because its
	// state is not fully initialized. Therefore, we check the bootstrap node.
	if proxy.XdsNode.Locality != nil {
		entry.Locality = util.LocalityToString(proxy.XdsNode.Locality)
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
	return string(proxy.Metadata.Network) + proxy.IPAddresses[0]
}
