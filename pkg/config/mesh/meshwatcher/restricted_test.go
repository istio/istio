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
	"fmt"
	"testing"
	"time"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestRestrictedCollection(t *testing.T) {
	stop := test.NewStop(t)
	local := NewTestWatcher(&meshconfig.MeshConfig{TrustDomain: "local", RootNamespace: "private"})
	remote := krt.NewStatic[MeshConfigResource](nil, false)
	restricted := NewRestrictedCollection(local, remote, 20*time.Millisecond, krt.NewOptionsBuilder(stop, "test", nil))
	assert.EventuallyEqual(t, restricted.AsCollection().HasSynced, true)
	assert.EventuallyEqual(t, restricted.Get, &RestrictedMeshConfig{TrustDomain: "local"})
	local.Set(&meshconfig.MeshConfig{TrustDomain: "updated-local"})
	assert.EventuallyEqual(t, restricted.Get, &RestrictedMeshConfig{TrustDomain: "updated-local"})
	remote.Set(&MeshConfigResource{&meshconfig.MeshConfig{
		TrustDomain: "remote", RootNamespace: "forbidden",
		ServiceScopeConfigs: []*meshconfig.MeshConfig_ServiceScopeConfigs{{}},
	}})
	remote.MarkSynced()
	assert.EventuallyEqual(t, restricted.Get, &RestrictedMeshConfig{
		TrustDomain:         "remote",
		ServiceScopeConfigs: []*meshconfig.MeshConfig_ServiceScopeConfigs{{}},
	})
	// An empty remote trust domain falls back, while an explicitly empty service
	// scope remains remote-owned (matching the previous watcher contract).
	local.Set(&meshconfig.MeshConfig{TrustDomain: "updated-local", ServiceScopeConfigs: []*meshconfig.MeshConfig_ServiceScopeConfigs{{}}})
	remote.Set(&MeshConfigResource{&meshconfig.MeshConfig{}})
	assert.EventuallyEqual(t, restricted.Get, &RestrictedMeshConfig{TrustDomain: "updated-local"})
	remote.Set(nil)
	assert.EventuallyEqual(t, restricted.Get, &RestrictedMeshConfig{
		TrustDomain:         "updated-local",
		ServiceScopeConfigs: []*meshconfig.MeshConfig_ServiceScopeConfigs{{}},
	})
}

func TestRestrictedCollectionInitialSync(t *testing.T) {
	for _, timeout := range []time.Duration{0, time.Hour} {
		t.Run(timeout.String(), func(t *testing.T) {
			stop := test.NewStop(t)
			local := NewTestWatcher(&meshconfig.MeshConfig{TrustDomain: "local"})
			remote := krt.NewStatic[MeshConfigResource](nil, false)
			restricted := NewRestrictedCollection(local, remote, timeout, krt.NewOptionsBuilder(stop, "test", nil))
			// Allow the transformation to start; it must not sync with the fallback
			// while the remote source is pending and its deadline has not elapsed.
			time.Sleep(20 * time.Millisecond)
			assert.Equal(t, restricted.AsCollection().HasSynced(), false)
			remote.Set(&MeshConfigResource{&meshconfig.MeshConfig{TrustDomain: "remote"}})
			remote.MarkSynced()
			assert.EventuallyEqual(t, restricted.AsCollection().HasSynced, true)
			assert.Equal(t, restricted.Get(), &RestrictedMeshConfig{TrustDomain: "remote"})
		})
	}
}

func TestRestrictedCollectionEmptySource(t *testing.T) {
	for _, withRemote := range []bool{false, true} {
		t.Run(fmt.Sprint(withRemote), func(t *testing.T) {
			stop := test.NewStop(t)
			local := NewTestWatcher(&meshconfig.MeshConfig{TrustDomain: "local"})
			var remote krt.Singleton[MeshConfigResource]
			if withRemote {
				remote = krt.NewStatic[MeshConfigResource](nil, true)
			}
			restricted := NewRestrictedCollection(local, remote, time.Hour, krt.NewOptionsBuilder(stop, "test", nil))
			assert.EventuallyEqual(t, restricted.AsCollection().HasSynced, true)
			assert.Equal(t, restricted.Get(), &RestrictedMeshConfig{TrustDomain: "local"})
		})
	}
}

func TestRestrictedCollectionStopBeforeSync(t *testing.T) {
	stop := make(chan struct{})
	local := NewTestWatcher(nil)
	remote := krt.NewStatic[MeshConfigResource](nil, false)
	restricted := NewRestrictedCollection(local, remote, time.Hour, krt.NewOptionsBuilder(stop, "test", nil))
	close(stop)
	assert.Equal(t, restricted.AsCollection().HasSynced(), false)
}
