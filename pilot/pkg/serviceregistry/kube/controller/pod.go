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
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/annotation"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

// PodCache is an eventually consistent pod cache
type PodCache struct {
	pods kclient.Client[*v1.Pod]

	sync.RWMutex
	// podsByIP maintains stable pod IP to name key mapping
	// this allows us to retrieve the latest status by pod IP.
	// This should only contain RUNNING or PENDING pods with an allocated IP.
	podsByIP map[string]sets.Set[types.NamespacedName]
	// ipByPods is a reverse map of podsByIP. This exists to allow us to prune stale entries in the
	// pod cache if a pod changes IP.
	ipByPods map[types.NamespacedName]string

	// needResync is map of IP to endpoint namespace/name. This is used to requeue endpoint
	// events when pod event comes. This typically happens when pod is not available
	// in podCache when endpoint event comes.
	needResync         map[string]sets.Set[types.NamespacedName]
	queueEndpointEvent func(types.NamespacedName)

	c *Controller
}

func newPodCache(c *Controller, pods kclient.Client[*v1.Pod], queueEndpointEvent func(types.NamespacedName)) *PodCache {
	out := &PodCache{
		pods:               pods,
		c:                  c,
		podsByIP:           make(map[string]sets.Set[types.NamespacedName]),
		ipByPods:           make(map[types.NamespacedName]string),
		needResync:         make(map[string]sets.Set[types.NamespacedName]),
		queueEndpointEvent: queueEndpointEvent,
	}

	return out
}

// Copied from kubernetes/kubernetes/pkg/controller/util/endpoint/controller_utils.go
//
// shouldPodBeInEndpoints returns true if a specified pod should be in an
// Endpoints or EndpointSlice resource. Terminating pods are not included.
func shouldPodBeInEndpoints(pod *v1.Pod) bool {
	// "Terminal" describes when a Pod is complete (in a succeeded or failed phase).
	// This is distinct from the "Terminating" condition which represents when a Pod
	// is being terminated (metadata.deletionTimestamp is non nil).
	if isPodPhaseTerminal(pod.Status.Phase) {
		return false
	}

	if len(pod.Status.PodIP) == 0 && len(pod.Status.PodIPs) == 0 {
		return false
	}

	if pod.DeletionTimestamp != nil {
		return false
	}

	return true
}

// isPodPhaseTerminal returns true if the pod's phase is terminal.
func isPodPhaseTerminal(phase v1.PodPhase) bool {
	return phase == v1.PodFailed || phase == v1.PodSucceeded
}

func IsPodRunning(pod *v1.Pod) bool {
	return pod.Status.Phase == v1.PodRunning
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

func (pc *PodCache) labelFilter(old, cur *v1.Pod) bool {
	// If labels/annotations updated, trigger proxy push
	labelsChanged := !maps.Equal(old.Labels, cur.Labels)
	// Annotations are only used in endpoints in one case, so just compare that one
	relevantAnnotationsChanged := old.Annotations[annotation.AmbientRedirection.Name] != cur.Annotations[annotation.AmbientRedirection.Name]
	changed := labelsChanged || relevantAnnotationsChanged
	return changed
}

// onEvent updates the IP-based index (pc.podsByIP).
func (pc *PodCache) onEvent(old, pod *v1.Pod, ev model.Event) error {
	ip := pod.Status.PodIP
	// PodIP will be empty when pod is just created, but before the IP is assigned
	// via UpdateStatus.
	if len(ip) == 0 {
		// However, in the case of an Eviction, the event that marks the pod as Failed may *also* have removed the IP.
		// If the pod *used to* have an IP, then we need to actually delete it.
		ip = pc.getIPByPod(config.NamespacedName(pod))
		if len(ip) == 0 {
			log.Debugf("Pod %s has no IP", config.NamespacedName(pod).String())
			return nil
		}
		log.Debugf("Pod %s has no IP, but was in the cache (%s), continue so we can delete it", config.NamespacedName(pod).String(), ip)
	}

	key := config.NamespacedName(pod)
	switch ev {
	case model.EventAdd:
		if shouldPodBeInEndpoints(pod) && IsPodReady(pod) {
			pc.addPod(pod, ip, key, false)
		} else {
			return nil
		}
	case model.EventUpdate:
		if !shouldPodBeInEndpoints(pod) || !IsPodReady(pod) {
			// delete only if this pod was in the cache
			if !pc.deleteIP(ip, key) {
				return nil
			}
			ev = model.EventDelete
		} else if shouldPodBeInEndpoints(pod) && IsPodReady(pod) {
			labelUpdated := pc.labelFilter(old, pod)
			pc.addPod(pod, ip, key, labelUpdated)
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
	pc.notifyWorkloadHandlers(pod, ev, ip)
	return nil
}

// notifyWorkloadHandlers fire workloadInstance handlers for pod
func (pc *PodCache) notifyWorkloadHandlers(pod *v1.Pod, ev model.Event, ip string) {
	// if no workload handler registered, skip building WorkloadInstance
	if len(pc.c.handlers.GetWorkloadHandlers()) == 0 {
		return
	}
	// fire instance handles for workload
	ep := pc.c.NewEndpointBuilder(pod).buildIstioEndpoint(
		ip,
		0,
		"",
		model.AlwaysDiscoverable,
		model.Healthy,
		features.GlobalSendUnhealthyEndpoints.Load(),
	)
	// If pod is dual stack, handle all IPs
	if features.EnableDualStack && len(pod.Status.PodIPs) > 1 {
		ep.Addresses = slices.Map(pod.Status.PodIPs, func(e v1.PodIP) string {
			return e.IP
		})
	}
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
func (pc *PodCache) deleteIP(ip string, podKey types.NamespacedName) bool {
	pc.Lock()
	defer pc.Unlock()
	if pc.podsByIP[ip].Contains(podKey) {
		sets.DeleteCleanupLast(pc.podsByIP, ip, podKey)
		delete(pc.ipByPods, podKey)
		return true
	}
	return false
}

func (pc *PodCache) addPod(pod *v1.Pod, ip string, key types.NamespacedName, labelUpdated bool) {
	pc.Lock()
	// if the pod has been cached, return
	if pc.podsByIP[ip].Contains(key) {
		pc.Unlock()
		if labelUpdated {
			pc.proxyUpdates(pod, true)
		}
		return
	}
	if current, f := pc.ipByPods[key]; f {
		// The pod already exists, but with another IP Address. We need to clean up that
		sets.DeleteCleanupLast(pc.podsByIP, current, key)
	}
	sets.InsertOrNew(pc.podsByIP, ip, key)
	pc.ipByPods[key] = ip

	if endpointsToUpdate, f := pc.needResync[ip]; f {
		delete(pc.needResync, ip)
		for epKey := range endpointsToUpdate {
			pc.queueEndpointEvent(epKey)
		}
		endpointsPendingPodUpdate.Record(float64(len(pc.needResync)))
	}
	pc.Unlock()

	pc.proxyUpdates(pod, false)
}

// queueEndpointEventOnPodArrival registers this endpoint and queues endpoint event
// when the corresponding pod arrives.
func (pc *PodCache) queueEndpointEventOnPodArrival(key types.NamespacedName, ip string) {
	pc.Lock()
	defer pc.Unlock()
	sets.InsertOrNew(pc.needResync, ip, key)
	endpointsPendingPodUpdate.Record(float64(len(pc.needResync)))
}

// endpointDeleted cleans up endpoint from resync endpoint list.
func (pc *PodCache) endpointDeleted(key types.NamespacedName, ip string) {
	pc.Lock()
	defer pc.Unlock()
	sets.DeleteCleanupLast(pc.needResync, ip, key)
	endpointsPendingPodUpdate.Record(float64(len(pc.needResync)))
}

func (pc *PodCache) proxyUpdates(pod *v1.Pod, isPodUpdate bool) {
	if pc.c != nil {
		if pc.c.opts.XDSUpdater != nil {
			ip := pod.Status.PodIP
			pc.c.opts.XDSUpdater.ProxyUpdate(pc.c.Cluster(), ip)
		}
		if isPodUpdate {
			// Recompute service(s) due to pod label change.
			// If it is a new pod, no need to recompute, as it yet computed for the first time yet.
			pc.c.recomputeServiceForPod(pod)
		}
	}
}

func (pc *PodCache) getPodKeys(addr string) []types.NamespacedName {
	pc.RLock()
	defer pc.RUnlock()
	return pc.podsByIP[addr].UnsortedList()
}

// getIPByPod returns the pod IP or empty string if pod not found.
func (pc *PodCache) getIPByPod(key types.NamespacedName) string {
	pc.RLock()
	defer pc.RUnlock()
	return pc.ipByPods[key]
}

// getPodByIp returns the pod or nil if pod not found or an error occurred
func (pc *PodCache) getPodsByIP(addr string) []*v1.Pod {
	keys := pc.getPodKeys(addr)
	if keys == nil {
		return nil
	}
	res := make([]*v1.Pod, 0, len(keys))
	for _, key := range keys {
		p := pc.getPodByKey(key)
		// Subtle race condition. getPodKeys is our cache over pods, while getPodByKey hits the informer cache.
		// if these are out of sync, p may be nil (pod was deleted).
		if p != nil {
			res = append(res, p)
		}
	}
	return res
}

// getPodByKey returns the pod by key
func (pc *PodCache) getPodByKey(key types.NamespacedName) *v1.Pod {
	return pc.pods.Get(key.Name, key.Namespace)
}

// getPodByKey returns the pod of the proxy
func (pc *PodCache) getPodByProxy(proxy *model.Proxy) *v1.Pod {
	key := podKeyByProxy(proxy)
	if key.Name != "" {
		pod := pc.getPodByKey(key)
		if pod != nil {
			return pod
		}
	}

	return nil
}
