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

package model

import (
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/sets"
)

// FakeEndpointIndexUpdater is an updater that will keep an EndpointIndex in sync. This is intended for tests only.
type FakeEndpointIndexUpdater struct {
	Index *EndpointIndex
	// Optional; if set, we will trigger ConfigUpdates in response to EDS updates as appropriate
	ConfigUpdateFunc func(req *PushRequest)
}

var _ XDSUpdater = &FakeEndpointIndexUpdater{}

func NewEndpointIndexUpdater(ei *EndpointIndex) *FakeEndpointIndexUpdater {
	return &FakeEndpointIndexUpdater{Index: ei}
}

func (f *FakeEndpointIndexUpdater) ConfigUpdate(*PushRequest) {}

func (f *FakeEndpointIndexUpdater) EDSUpdate(shard ShardKey, serviceName string, namespace string, eps []*IstioEndpoint) {
	pushType := f.Index.UpdateServiceEndpoints(shard, serviceName, namespace, eps, true)
	if f.ConfigUpdateFunc != nil && (pushType == IncrementalPush || pushType == FullPush) {
		// Trigger a push
		f.ConfigUpdateFunc(&PushRequest{
			Full:           pushType == FullPush,
			ConfigsUpdated: sets.New(ConfigKey{Kind: kind.ServiceEntry, Name: serviceName, Namespace: namespace}),
			Reason:         NewReasonStats(EndpointUpdate),
		})
	}
}

func (f *FakeEndpointIndexUpdater) EDSCacheUpdate(shard ShardKey, serviceName string, namespace string, eps []*IstioEndpoint) {
	f.Index.UpdateServiceEndpoints(shard, serviceName, namespace, eps, false)
}

func (f *FakeEndpointIndexUpdater) SvcUpdate(shard ShardKey, hostname string, namespace string, event Event) {
	if event == EventDelete {
		f.Index.DeleteServiceShard(shard, hostname, namespace, false)
	}
}

func (f *FakeEndpointIndexUpdater) ProxyUpdate(_ cluster.ID, _ string) {}

func (f *FakeEndpointIndexUpdater) RemoveShard(shardKey ShardKey) {
	f.Index.DeleteShard(shardKey)
}
