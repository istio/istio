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

package controller

import (
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/filter"
	"istio.io/istio/pkg/util/sets"
)

// PodCache is an eventually consistent pod cache
type PodCache struct {
	informer filter.FilteredSharedIndexInformer

	sync.RWMutex
	// podsByIP maintains stable pod IP to name key mapping
	// this allows us to retrieve the latest status by pod IP.
	// This should only contain RUNNING or PENDING pods with an allocated IP.
	podsByIP map[string]string
	// IPByPods is a reverse map of podsByIP. This exists to allow us to prune stale entries in the
	// pod cache if a pod changes IP.
	IPByPods map[string]string

	// needResync is map of IP to endpoint namespace/name. This is used to requeue endpoint
	// events when pod event comes. This typically happens when pod is not available
	// in podCache when endpoint event comes.
	needResync         map[string]sets.Set
	queueEndpointEvent func(string)

	c *Controller
}

func newPodCache(c *Controller, informer filter.FilteredSharedIndexInformer, queueEndpointEvent func(string)) *PodCache {
	out := &PodCache{
		informer:           informer,
		c:                  c,
		podsByIP:           make(map[string]string),
		IPByPods:           make(map[string]string),
		needResync:         make(map[string]sets.Set),
		queueEndpointEvent: queueEndpointEvent,
	}

	return out
}

// Copied from kubernetes/pkg/controller/endpoint/endpoints_controller.go
func shouldPodBeInEndpoints(pod *v1.Pod) bool {
	switch pod.Spec.RestartPolicy {
	case v1.RestartPolicyNever:
		return pod.Status.Phase != v1.PodFailed && pod.Status.Phase != v1.PodSucceeded
	case v1.RestartPolicyOnFailure:
		return pod.Status.Phase != v1.PodSucceeded
	default:
		return true
	}
}

// IsPodReady is copied from kubernetes/pkg/api/v1/pod/utils.go
func IsPodReady(pod *v1.Pod) bool {
	return IsPodReadyConditionTrue(pod.Status)
}

// IsPodReadyConditionTrue returns true if a pod is ready; false otherwise.
func IsPodReadyConditionTrue(status v1.PodStatus) bool {
	condition := GetPodReadyCondition(status)
	return condition != nil && condition.Status == v1.ConditionTrue
}

func GetPodReadyCondition(status v1.PodStatus) *v1.PodCondition {
	_, condition := GetPodCondition(&status, v1.PodReady)
	return condition
}

func GetPodCondition(status *v1.PodStatus, conditionType v1.PodConditionType) (int, *v1.PodCondition) {
	if status == nil {
		return -1, nil
	}
	return GetPodConditionFromList(status.Conditions, conditionType)
}

// GetPodConditionFromList extracts the provided condition from the given list of condition and
// returns the index of the condition and the condition. Returns -1 and nil if the condition is not present.
func GetPodConditionFromList(conditions []v1.PodCondition, conditionType v1.PodConditionType) (int, *v1.PodCondition) {
	if conditions == nil {
		return -1, nil
	}
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return i, &conditions[i]
		}
	}
	return -1, nil
}

// onEvent updates the IP-based index (pc.podsByIP).
func (pc *PodCache) onEvent(curr interface{}, ev model.Event) error {
	// When a pod is deleted obj could be an *v1.Pod or a DeletionFinalStateUnknown marker item.
	pod, ok := curr.(*v1.Pod)
	if !ok {
		tombstone, ok := curr.(cache.DeletedFinalStateUnknown)
		if !ok {
			return fmt.Errorf("couldn't get object from tombstone %+v", curr)
		}
		pod, ok = tombstone.Obj.(*v1.Pod)
		if !ok {
			return fmt.Errorf("tombstone contained object that is not a pod %#v", curr)
		}
	}

	ip := pod.Status.PodIP
	// PodIP will be empty when pod is just created, but before the IP is assigned
	// via UpdateStatus.
	if len(ip) == 0 {
		return nil
	}

	key := kube.KeyFunc(pod.Name, pod.Namespace)
	switch ev {
	case model.EventAdd:
		// can happen when istiod just starts
		if pod.DeletionTimestamp != nil || !IsPodReady(pod) {
			return nil
		} else if shouldPodBeInEndpoints(pod) {
			pc.update(ip, key)
		} else {
			return nil
		}
	case model.EventUpdate:
		if pod.DeletionTimestamp != nil || !IsPodReady(pod) {
			// delete only if this pod was in the cache
			pc.deleteIP(ip, key)
			ev = model.EventDelete
		} else if shouldPodBeInEndpoints(pod) {
			pc.update(ip, key)
		} else {
			return nil
		}
	case model.EventDelete:
		// delete only if this pod was in the cache,
		// in most case it has already been deleted in `UPDATE` with `DeletionTimestamp` set.
		if !pc.deleteIP(ip, key) {
			return nil
		}
	}
	pc.notifyWorkloadHandlers(pod, ev)
	return nil
}

// notifyWorkloadHandlers fire workloadInstance handlers for pod
func (pc *PodCache) notifyWorkloadHandlers(pod *v1.Pod, ev model.Event) {
	// fire instance handles for workload
	ep := NewEndpointBuilder(pc.c, pod).buildIstioEndpoint(pod.Status.PodIP, 0, "", model.AlwaysDiscoverable)
	workloadInstance := &model.WorkloadInstance{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		Kind:      model.PodKind,
		Endpoint:  ep,
		PortMap:   getPortMap(pod),
	}
	pc.c.handlers.NotifyWorkloadHandlers(workloadInstance, ev)
}

func getPortMap(pod *v1.Pod) map[string]uint32 {
	pmap := map[string]uint32{}
	for _, c := range pod.Spec.Containers {
		for _, port := range c.Ports {
			if port.Name == "" || port.Protocol != v1.ProtocolTCP {
				continue
			}
			// First port wins, per Kubernetes (https://github.com/kubernetes/kubernetes/issues/54213)
			if _, f := pmap[port.Name]; !f {
				pmap[port.Name] = uint32(port.ContainerPort)
			}
		}
	}
	return pmap
}

// deleteIP returns true if the pod and ip are really deleted.
func (pc *PodCache) deleteIP(ip string, podKey string) bool {
	pc.Lock()
	defer pc.Unlock()
	if pc.podsByIP[ip] == podKey {
		delete(pc.podsByIP, ip)
		delete(pc.IPByPods, podKey)
		return true
	}
	return false
}

func (pc *PodCache) update(ip, key string) {
	pc.Lock()
	// if the pod has been cached, return
	if key == pc.podsByIP[ip] {
		pc.Unlock()
		return
	}
	if current, f := pc.IPByPods[key]; f {
		// The pod already exists, but with another IP Address. We need to clean up that
		delete(pc.podsByIP, current)
	}
	pc.podsByIP[ip] = key
	pc.IPByPods[key] = ip

	if endpointsToUpdate, f := pc.needResync[ip]; f {
		delete(pc.needResync, ip)
		for epKey := range endpointsToUpdate {
			pc.queueEndpointEvent(epKey)
		}
		endpointsPendingPodUpdate.Record(float64(len(pc.needResync)))
	}
	pc.Unlock()

	pc.proxyUpdates(ip)
}

// queueEndpointEventOnPodArrival registers this endpoint and queues endpoint event
// when the corresponding pod arrives.
func (pc *PodCache) queueEndpointEventOnPodArrival(key, ip string) {
	pc.Lock()
	defer pc.Unlock()
	if _, f := pc.needResync[ip]; !f {
		pc.needResync[ip] = sets.New(key)
	} else {
		pc.needResync[ip].Insert(key)
	}
	endpointsPendingPodUpdate.Record(float64(len(pc.needResync)))
}

// endpointDeleted cleans up endpoint from resync endpoint list.
func (pc *PodCache) endpointDeleted(key string, ip string) {
	pc.Lock()
	defer pc.Unlock()
	delete(pc.needResync[ip], key)
	if len(pc.needResync[ip]) == 0 {
		delete(pc.needResync, ip)
	}
	endpointsPendingPodUpdate.Record(float64(len(pc.needResync)))
}

func (pc *PodCache) proxyUpdates(ip string) {
	if pc.c != nil && pc.c.opts.XDSUpdater != nil {
		pc.c.opts.XDSUpdater.ProxyUpdate(pc.c.Cluster(), ip)
	}
}

func (pc *PodCache) getPodKey(addr string) (string, bool) {
	pc.RLock()
	defer pc.RUnlock()
	key, exists := pc.podsByIP[addr]
	return key, exists
}

// getPodByIp returns the pod or nil if pod not found or an error occurred
func (pc *PodCache) getPodByIP(addr string) *v1.Pod {
	key, exists := pc.getPodKey(addr)
	if !exists {
		return nil
	}
	return pc.getPodByKey(key)
}

// getPodByKey returns the pod by key formatted `ns/name`
func (pc *PodCache) getPodByKey(key string) *v1.Pod {
	item, _, _ := pc.informer.GetIndexer().GetByKey(key)
	if item != nil {
		return item.(*v1.Pod)
	}
	return nil
}

// getPodByKey returns the pod of the proxy
func (pc *PodCache) getPodByProxy(proxy *model.Proxy) *v1.Pod {
	var pod *v1.Pod
	key := podKeyByProxy(proxy)
	if key != "" {
		pod = pc.getPodByKey(key)
		if pod != nil {
			return pod
		}
	}

	// only need to fetch the corresponding pod through the first IP, although there are multiple IP scenarios,
	// because multiple ips belong to the same pod
	proxyIP := proxy.IPAddresses[0]
	// just in case the proxy ID is bad formatted
	return pc.getPodByIP(proxyIP)
}
