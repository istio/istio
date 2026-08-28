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

package agentgateway

import (
	"testing"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/ambient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	"istio.io/istio/pkg/test/util/assert"
)

// fakeAmbientIndex satisfies ambient.Index for wiring tests. Only the accessors touched by
// buildAmbientCollections have meaningful bodies; the rest exist to satisfy the interface.
type fakeAmbientIndex struct {
	model.NoopAmbientIndexes
	services  krt.Collection[model.ServiceInfo]
	workloads krt.Collection[model.WorkloadInfo]
	resolver  ambient.ServiceWaypointResolver
	synced    bool
}

func (f *fakeAmbientIndex) Lookup(string) []model.AddressInfo { return nil }
func (f *fakeAmbientIndex) All() []model.AddressInfo          { return nil }
func (f *fakeAmbientIndex) AllLocalNetworkGlobalServices(model.WaypointKey) []model.ServiceInfo {
	return nil
}
func (f *fakeAmbientIndex) Services() krt.Collection[model.ServiceInfo]   { return f.services }
func (f *fakeAmbientIndex) Workloads() krt.Collection[model.WorkloadInfo] { return f.workloads }
func (f *fakeAmbientIndex) ServiceOwningWaypointNames(ctx krt.HandlerContext, svc model.ServiceInfo) []types.NamespacedName {
	if f.resolver == nil {
		return nil
	}
	return f.resolver(ctx, svc)
}
func (f *fakeAmbientIndex) Run(<-chan struct{}) {}
func (f *fakeAmbientIndex) HasSynced() bool     { return f.synced }

var _ ambient.Index = (*fakeAmbientIndex)(nil)

// TestBuildAmbientCollections_Shared pins that when a non-nil ambient.Index is wired in the
// controller reuses its Services/Workloads collections directly rather than rebuilding a local
// copy. Pointer identity is the strongest signal that no silent re-derivation is happening.
func TestBuildAmbientCollections_Shared(t *testing.T) {
	opts := krttest.Options(t)
	svcs := krt.NewStaticCollection[model.ServiceInfo](nil, nil, opts.WithName("AmbientServices")...)
	wls := krt.NewStaticCollection[model.WorkloadInfo](nil, nil, opts.WithName("AmbientWorkloads")...)
	idx := &fakeAmbientIndex{services: svcs, workloads: wls, synced: true}

	c := &Controller{ambientIndex: idx}
	gotSvcs, gotWls, gotResolver := c.buildAmbientCollections(opts)

	assert.Equal(t, gotSvcs == krt.Collection[model.ServiceInfo](svcs), true)
	assert.Equal(t, gotWls == krt.Collection[model.WorkloadInfo](wls), true)
	assert.Equal(t, gotResolver == nil, false)
}
