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

package meshwatcher

import (
	"testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestRestrictedCollection(t *testing.T) {
	stop := test.NewStop(t)
	local := NewTestWatcher(&meshconfig.MeshConfig{TrustDomain: "local", RootNamespace: "private"})
	remote := krt.NewStatic[MeshConfigResource](nil, false)
	restricted := NewRestrictedCollection(local, remote, krt.NewOptionsBuilder(stop, "test", nil))
	assert.EventuallyEqual(t, restricted.AsCollection().HasSynced, true)
	assert.EventuallyEqual(t, restricted.Get, &RestrictedMeshConfig{TrustDomain: "local"})
	local.Set(&meshconfig.MeshConfig{TrustDomain: "updated-local"})
	assert.EventuallyEqual(t, restricted.Get, &RestrictedMeshConfig{TrustDomain: "updated-local"})
	remote.Set(&MeshConfigResource{&meshconfig.MeshConfig{TrustDomain: "remote", RootNamespace: "forbidden",
		ServiceScopeConfigs: []*meshconfig.MeshConfig_ServiceScopeConfigs{{}},
	}})
	remote.MarkSynced()
	assert.EventuallyEqual(t, restricted.Get, &RestrictedMeshConfig{TrustDomain: "remote",
		ServiceScopeConfigs: []*meshconfig.MeshConfig_ServiceScopeConfigs{{}},
	})
	// An empty remote trust domain falls back, while an explicitly empty service
	// scope remains remote-owned (matching the previous watcher contract).
	local.Set(&meshconfig.MeshConfig{TrustDomain: "updated-local", ServiceScopeConfigs: []*meshconfig.MeshConfig_ServiceScopeConfigs{{}}})
	remote.Set(&MeshConfigResource{&meshconfig.MeshConfig{}})
	assert.EventuallyEqual(t, restricted.Get, &RestrictedMeshConfig{TrustDomain: "updated-local"})
	remote.Set(nil)
	assert.EventuallyEqual(t, restricted.Get, &RestrictedMeshConfig{TrustDomain: "updated-local", ServiceScopeConfigs: []*meshconfig.MeshConfig_ServiceScopeConfigs{{}}})
}
