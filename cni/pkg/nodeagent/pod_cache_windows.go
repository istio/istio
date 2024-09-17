//go:build windows
// +build windows

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

package nodeagent

import (
	"sync"

	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/zdsapi"
	corev1 "k8s.io/api/core/v1"
)

// Hold a cache of node local pods with their netns
// if we don't know the netns, the pod will still be here with a nil netns.
type podNetnsCache struct {
	openNetns func(nspath string) (NetnsCloser, error)

	currentPodCache map[string]WorkloadInfo
	mu              sync.RWMutex
}

var _ PodNetnsCache = &podNetnsCache{}

type workloadInfo struct {
	workload  *zdsapi.WorkloadInfo
	namespace NamespaceCloser
}

func (wi workloadInfo) Workload() *zdsapi.WorkloadInfo {
	return wi.workload
}

func (wi workloadInfo) NetnsCloser() NetnsCloser {
	return wi.namespace
}

func (p *podNetnsCache) ReadCurrentPodSnapshot() map[string]WorkloadInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	// snapshot the cache to avoid long locking
	return maps.Clone(p.currentPodCache)
}

func (p *podNetnsCache) UpsertPodCache(pod *corev1.Pod, namespace string) {

}
