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
	"google.golang.org/protobuf/proto"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/protomarshal"
)

// NewRestrictedCollection exposes only trust domain and service-scope configuration.
// The optional remote source must not block synchronization: a synced snapshot bridges
// its updates into KRT while the local configuration provides the initial fallback.
func NewRestrictedCollection(
	local krt.Singleton[MeshConfigResource],
	remote krt.Singleton[MeshConfigResource],
	opts krt.OptionsBuilder,
) krt.Singleton[RestrictedMeshConfig] {
	snapshot := krt.NewStatic[MeshConfigResource](nil, true, opts.WithName("OptionalRemoteMeshConfig")...)
	if remote != nil {
		reg := remote.AsCollection().RegisterBatch(func(events []krt.Event[MeshConfigResource]) {
			snapshot.Set(remote.Get())
		}, true)
		go func() {
			<-opts.Stop()
			reg.UnregisterHandler()
		}()
	}

	return krt.NewSingleton(func(ctx krt.HandlerContext) *RestrictedMeshConfig {
		fallbackConfig := ptr.OrEmpty(krt.FetchOne(ctx, local.AsCollection())).MeshConfig
		primaryConfig := ptr.OrEmpty(krt.FetchOne(ctx, snapshot.AsCollection())).MeshConfig
		selected := primaryConfig
		if selected == nil {
			selected = fallbackConfig
		}
		trustDomain := selected.GetTrustDomain()
		if trustDomain == "" {
			trustDomain = fallbackConfig.GetTrustDomain()
		}
		return &RestrictedMeshConfig{
			TrustDomain:         trustDomain,
			ServiceScopeConfigs: slices.Map(selected.GetServiceScopeConfigs(), protomarshal.Clone),
		}
	}, opts.WithName("RestrictedMeshConfig")...)
}

// RestrictedMeshConfig is the per-cluster configuration consumers are allowed to read.
// Keeping a separate type prevents access to unrelated remote mesh settings.
type RestrictedMeshConfig struct {
	TrustDomain         string
	ServiceScopeConfigs []*meshconfig.MeshConfig_ServiceScopeConfigs
}

func (m RestrictedMeshConfig) ResourceName() string {
	return "RestrictedMeshConfig"
}

func (m RestrictedMeshConfig) Equals(other RestrictedMeshConfig) bool {
	return m.TrustDomain == other.TrustDomain &&
		slices.EqualFunc(m.ServiceScopeConfigs, other.ServiceScopeConfigs, func(a, b *meshconfig.MeshConfig_ServiceScopeConfigs) bool {
			return proto.Equal(a, b)
		})
}
