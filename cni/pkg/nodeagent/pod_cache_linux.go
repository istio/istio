//go:build linux
// +build linux

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
	"fmt"
	"path/filepath"
	"runtime"
	"sync"

	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/zdsapi"
)

// Hold a cache of node local pods with their netns
// if we don't know the netns, the pod will still be here with a nil netns.
type podNetnsCache struct {
	openNetns func(nspath string) (NetnsCloser, error)

	currentPodCache map[string]WorkloadInfo
	mu              sync.RWMutex
}

type workloadInfo struct {
	workload *zdsapi.WorkloadInfo
	netns    NetnsCloser
}

func (wi workloadInfo) Workload() *zdsapi.WorkloadInfo {
	return wi.workload
}

func (wi workloadInfo) NetnsCloser() NetnsCloser {
	return wi.netns
}

var _ PodNetnsCache = &podNetnsCache{}

func newPodNetnsCache(openNetns func(nspath string) (NetnsCloser, error)) *podNetnsCache {
	return &podNetnsCache{
		openNetns:       openNetns,
		currentPodCache: map[string]WorkloadInfo{},
	}
}

func (p *podNetnsCache) UpsertPodCache(pod *corev1.Pod, nspath string) (Netns, error) {
	newnetns, err := p.openNetns(nspath)
	if err != nil {
		return nil, err
	}
	wl := workloadInfo{
		workload: podToWorkload(pod),
		netns:    newnetns,
	}
	return p.UpsertPodCacheWithNetns(string(pod.UID), wl), nil
}

// Update the cache with the given Netns. If there is already a Netns for the given uid, we return it, and close the one provided.
func (p *podNetnsCache) UpsertPodCacheWithNetns(uid string, workload WorkloadInfo) Netns {
	// lock current snapshot pod map
	p.mu.Lock()
	defer p.mu.Unlock()
	if existing := p.currentPodCache[uid]; existing != nil && existing.NetnsCloser() != nil {
		if existing.NetnsCloser().Inode() == workload.NetnsCloser().Inode() {
			workload.NetnsCloser().Close()
			// Replace the workload, but keep the old Netns
			p.currentPodCache[uid] = workloadInfo{
				workload: workload.Workload(),
				netns:    existing.NetnsCloser(),
			}
			// already in cache
			return existing.NetnsCloser()
		}
		log.Debug("netns inode mismatch, using the new one")
	}

	p.addToCacheUnderLock(uid, workload)
	return workload.NetnsCloser()
}

// Get the netns if it's in the cache
func (p *podNetnsCache) Get(uid string) Netns {
	// lock current snapshot pod map
	p.mu.RLock()
	defer p.mu.RUnlock()
	if info, f := p.currentPodCache[uid]; f {
		return info.NetnsCloser()
	}
	return nil
}

// make sure uid is in the cache, even if we don't have a netns
func (p *podNetnsCache) Ensure(uid string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.currentPodCache[uid]; !ok {
		p.currentPodCache[uid] = workloadInfo{}
	}
}

func (p *podNetnsCache) addToCacheUnderLock(uid string, workload WorkloadInfo) {
	runtime.SetFinalizer(workload.NetnsCloser(), closeNetns)
	p.currentPodCache[uid] = workload
}

func closeNetns(netns NetnsCloser) {
	netns.Close()
}

func (p *podNetnsCache) ReadCurrentPodSnapshot() map[string]WorkloadInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	// snapshot the cache to avoid long locking
	return maps.Clone(p.currentPodCache)
}

// Remove and return the Netns for the given uid
// No need to return NetnsCloser here it will be closed automatically on GC.
// (it may be used in parallel by other parts of the code, so we want it to be used only when not used)
func (p *podNetnsCache) Take(uid string) Netns {
	// lock current pod map
	p.mu.Lock()
	defer p.mu.Unlock()
	if ns, ok := p.currentPodCache[uid]; ok {
		delete(p.currentPodCache, uid)
		// already in cache
		return ns.NetnsCloser().(Netns)
	}

	return nil
}

func openNetnsInRoot(hostMountsPath string) func(nspath string) (NetnsCloser, error) {
	return func(nspath string) (NetnsCloser, error) {
		nspathInContainer := filepath.Join(hostMountsPath, nspath)
		ns, err := OpenNetns(nspathInContainer)
		if err != nil {
			err = fmt.Errorf("failed to open netns: %w. Make sure that the netns host path %s is mounted in under %s in the container", err, nspath, hostMountsPath)
			log.Error(err.Error())
		}
		return ns, err
	}
}
