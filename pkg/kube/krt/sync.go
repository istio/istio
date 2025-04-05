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

package krt

import (
	"istio.io/istio/pkg/kube"
)

type Syncer interface {
	WaitUntilSynced(stop <-chan struct{}) bool
	HasSynced() bool
}

var (
	_ Syncer = channelSyncer{}
	_ Syncer = pollSyncer{}
	_ Syncer = multiSyncer{}
)

type channelSyncer struct {
	name   string
	synced <-chan struct{}
}

func (c channelSyncer) WaitUntilSynced(stop <-chan struct{}) bool {
	return waitForCacheSync(c.name, stop, c.synced)
}

func (c channelSyncer) HasSynced() bool {
	select {
	case <-c.synced:
		return true
	default:
		return false
	}
}

type pollSyncer struct {
	name string
	f    func() bool
}

func (c pollSyncer) WaitUntilSynced(stop <-chan struct{}) bool {
	return kube.WaitForCacheSync(c.name, stop, c.f)
}

func (c pollSyncer) HasSynced() bool {
	return c.f()
}

type alwaysSynced struct{}

func (c alwaysSynced) WaitUntilSynced(stop <-chan struct{}) bool {
	return true
}

func (c alwaysSynced) HasSynced() bool {
	return true
}

type multiSyncer struct {
	syncers []Syncer
}

func (c multiSyncer) WaitUntilSynced(stop <-chan struct{}) bool {
	for _, s := range c.syncers {
		if !s.WaitUntilSynced(stop) {
			return false
		}
	}
	return true
}

func (c multiSyncer) HasSynced() bool {
	for _, s := range c.syncers {
		if !s.HasSynced() {
			return false
		}
	}
	return true
}
