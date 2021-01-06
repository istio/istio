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

package workloadentry

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/queue"
	istiolog "istio.io/pkg/log"
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

	workerNum = 5
)

var log = istiolog.RegisterScope("wle", "wle controller debugging", 0)

type Controller struct {
	instanceID string
	// TODO move WorkloadEntry related tasks into their own object and give InternalGen a reference.
	// store should either be k8s (for running pilot) or in-memory (for tests). MCP and other config store implementations
	// do not support writing. We only use it here for reading WorkloadEntry/WorkloadGroup.
	store model.ConfigStoreCache

	// Note: unregister is to update the workload entry status: like setting `DisconnectedAtAnnotation`
	// and make the workload entry enqueue `cleanupQueue`
	// cleanup is to delete the workload entry

	// queue contains workloadEntry that need to be unregistered
	queue workqueue.RateLimitingInterface
	// cleanupLimit rate limit's autoregistered WorkloadEntry cleanup calls to k8s
	cleanupLimit *rate.Limiter
	// cleanupQueue delays the cleanup of autoregsitered WorkloadEntries to allow for grace period
	cleanupQueue queue.Delayed

	mutex sync.Mutex
	// record the current adsConnections number
	// note: this is to handle reconnect to the same istiod, but in rare case the disconnect event is later than the connect event
	// keyed by proxy network+ip
	adsConnections map[string]uint8

	// maxConnectionAge is a duration that workload entry should be cleanedup if it does not reconnects.
	maxConnectionAge time.Duration
}

// NewController create a controller which manages workload lifecycle and health status.
func NewController(store model.ConfigStoreCache, instanceID string, maxConnAge time.Duration) *Controller {
	if features.WorkloadEntryAutoRegistration {
		maxConnAge := maxConnAge + maxConnAge/2
		// if overflow, set it to max int64
		if maxConnAge < 0 {
			maxConnAge = time.Duration(math.MaxInt64)
		}
		return &Controller{
			instanceID:       instanceID,
			store:            store,
			cleanupLimit:     rate.NewLimiter(rate.Limit(20), 1),
			cleanupQueue:     queue.NewDelayed(),
			queue:            workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
			adsConnections:   map[string]uint8{},
			maxConnectionAge: maxConnAge,
		}
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

	for i := 0; i < workerNum; i++ {
		go wait.Until(c.worker, time.Second, stop)
	}
	<-stop
	c.queue.ShutDown()
}

func (c *Controller) worker() {
	for c.processNextWorkItem() {
	}
}

// workItem contains the state of a "disconnect" event used to unregister a workload.
type workItem struct {
	entryName   string
	proxy       *model.Proxy
	disConTime  time.Time
	origConTime time.Time
}

func (c *Controller) processNextWorkItem() bool {
	item, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(item)

	workItem, ok := item.(*workItem)
	if !ok {
		return true
	}

	err := c.unregisterWorkload(workItem.entryName, workItem.proxy, workItem.disConTime, workItem.origConTime)
	c.handleErr(err, item)
	return true
}

func setConnectMeta(c *config.Config, controller string, conTime time.Time) {
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

	c.mutex.Lock()
	c.adsConnections[proxy.Metadata.Network+proxy.IPAddresses[0]]++
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
		log.Infof("updated auto-registered WorkloadEntry %s/%s", proxy.Metadata.Namespace, entryName)
		return nil
	}

	// No WorkloadEntry, create one using fields from the associated WorkloadGroup
	groupCfg := c.store.Get(gvk.WorkloadGroup, proxy.Metadata.AutoRegisterGroup, proxy.Metadata.Namespace)
	if groupCfg == nil {
		return fmt.Errorf("auto-registration WorkloadEntry of %v failed: cannot find WorkloadGroup %s/%s",
			proxy.ID, proxy.Metadata.Namespace, proxy.Metadata.AutoRegisterGroup)
	}
	entry := workloadEntryFromGroup(entryName, proxy, groupCfg)
	setConnectMeta(entry, c.instanceID, conTime)
	_, err := c.store.Create(*entry)
	if err != nil {
		return fmt.Errorf("auto-registration WorkloadEntry of %v failed: error creating WorkloadEntry: %v", proxy.ID, err)
	}
	log.Infof("auto-registered WorkloadEntry %s/%s", proxy.Metadata.Namespace, entryName)
	return nil
}

func (c *Controller) QueueUnregisterWorkload(proxy *model.Proxy, origConnect time.Time) {
	if !features.WorkloadEntryAutoRegistration || c == nil {
		return
	}
	// check if the WE already exists, update the status
	entryName := autoregisteredWorkloadEntryName(proxy)
	if entryName == "" {
		return
	}

	c.mutex.Lock()
	num := c.adsConnections[proxy.Metadata.Network+proxy.IPAddresses[0]]
	// if there is still ads connection, do not unregister.
	if num > 1 {
		c.adsConnections[proxy.Metadata.Network+proxy.IPAddresses[0]] = num - 1
		c.mutex.Unlock()
		return
	}
	delete(c.adsConnections, proxy.Metadata.Network+proxy.IPAddresses[0])
	c.mutex.Unlock()

	disconTime := time.Now()
	if err := c.unregisterWorkload(entryName, proxy, disconTime, origConnect); err != nil {
		log.Errorf(err)
		c.queue.AddRateLimited(&workItem{
			entryName:   entryName,
			proxy:       proxy,
			disConTime:  disconTime,
			origConTime: origConnect,
		})
	}
}

func (c *Controller) unregisterWorkload(entryName string, proxy *model.Proxy, disconTime, origConnTime time.Time) error {
	// unset controller, set disconnect time
	cfg := c.store.Get(gvk.WorkloadEntry, entryName, proxy.Metadata.Namespace)
	if cfg == nil {
		// return error and backoff retry to prevent workloadentry leak
		// TODO(@hzxuzhonghu): update the Get interface, fallback to calling apiserver.
		return fmt.Errorf("workloadentry %s/%s is not found, maybe deleted or because of propagate latency", proxy.Metadata.Namespace, entryName)
	}

	// only queue a delete if this disconnect event is associated with the last connect event written to the worload entry
	if mostRecentConn, err := time.Parse(timeFormat, cfg.Annotations[ConnectedAtAnnotation]); err == nil {
		if mostRecentConn.After(origConnTime) {
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
	if disconTime.Before(conTime) {
		return nil
	}

	wle := cfg.DeepCopy()
	delete(wle.Annotations, ConnectedAtAnnotation)
	wle.Annotations[DisconnectedAtAnnotation] = disconTime.Format(timeFormat)
	// use update instead of patch to prevent race condition
	_, err := c.store.Update(wle)
	if err != nil {
		return fmt.Errorf("disconnect: failed updating WorkloadEntry %s/%s: %v", proxy.Metadata.Namespace, entryName, err)
	}

	// after grace period, check if the workload ever reconnected
	ns := proxy.Metadata.Namespace
	c.cleanupQueue.PushDelayed(func() error {
		wle := c.store.Get(gvk.WorkloadEntry, entryName, ns)
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
		return
	}
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
	p := []string{proxy.Metadata.AutoRegisterGroup, proxy.IPAddresses[0]}
	if proxy.Metadata.Network != "" {
		p = append(p, proxy.Metadata.Network)
	}

	name := strings.Join(p, "-")
	if len(name) > 253 {
		name = name[len(name)-253:]
		log.Warnf("generated WorkloadEntry name is too long, consider making the WorkloadGroup name shorter. Shortening from beginning to: %s", name)
	}
	return name
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
		entry.Network = proxy.Metadata.Network
	}
	if proxy.Locality != nil {
		entry.Locality = util.LocalityToString(proxy.Locality)
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

func (c *Controller) handleErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	if c.queue.NumRequeues(key) < maxRetries {
		log.Debugf(err)
		c.queue.AddRateLimited(key)
		return
	}

	c.queue.Forget(key)
	log.Errorf(err)
}
