// Copyright 2019 Istio Authors
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

package pod

import (
	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pkg/spiffe"

	coreV1 "k8s.io/api/core/v1"
)

var _ Cache = cacheImpl{}
var _ processing.Handler = cacheImpl{}

// Info for a Pod.
type Info struct {
	FullName resource.FullName
	NodeName string

	// ServiceAccountName the Spiffe name for the Pod service account.
	ServiceAccountName string
}

// Cache for pod Info.
type Cache interface {
	GetPodByIP(ip string) (Info, bool)
}

// NewCache creates a cache and its update handler
func NewCache() (Cache, processing.Handler) {
	c := make(cacheImpl)
	return c, c
}

type cacheImpl map[string]Info

func (pc cacheImpl) GetPodByIP(ip string) (Info, bool) {
	pod, ok := pc[ip]
	return pod, ok
}

func (pc cacheImpl) Handle(event resource.Event) {
	if event.Entry.ID.Collection != metadata.K8sCoreV1Pods.Collection {
		return
	}

	switch event.Kind {
	case resource.Added, resource.Updated:
		pod := event.Entry.Item.(*coreV1.Pod)

		ip := pod.Status.PodIP
		if ip == "" {
			// PodIP will be empty when pod is just created, but before the IP is assigned
			// via UpdateStatus.
			return
		}

		switch pod.Status.Phase {
		case coreV1.PodPending, coreV1.PodRunning:
			// add to cache if the pod is running or pending
			pc[ip] = Info{
				FullName:           event.Entry.ID.FullName,
				NodeName:           pod.Spec.NodeName,
				ServiceAccountName: kubeToIstioServiceAccount(pod.Spec.ServiceAccountName, pod.Namespace),
			}
		default:
			// delete if the pod switched to other states and is in the cache
			delete(pc, ip)
		}
	case resource.Deleted:
		var ip string
		pod, ok := event.Entry.Item.(*coreV1.Pod)
		if ok {
			ip = pod.Status.PodIP
		} else {
			// The resource was either not available or failed parsing. Look it up by brute force.
			for infoIP, info := range pc {
				if info.FullName == event.Entry.ID.FullName {
					ip = infoIP
					break
				}
			}
		}

		// delete only if this pod was in the cache
		delete(pc, ip)
	}
}

// kubeToIstioServiceAccount converts a K8s service account to an Istio service account
func kubeToIstioServiceAccount(saname string, ns string) string {
	return spiffe.MustGenSpiffeURI(ns, saname)
}
