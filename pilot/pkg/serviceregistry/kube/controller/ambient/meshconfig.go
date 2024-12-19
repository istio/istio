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

// nolint: gocritic
package ambient

import (
	"google.golang.org/protobuf/proto"
	"istio.io/istio/pkg/ptr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	meshapi "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
)

type MeshConfig struct {
	*meshapi.MeshConfig
}

func (m MeshConfig) ResourceName() string { return "MeshConfig" }

func (m MeshConfig) Equals(other MeshConfig) bool { return proto.Equal(m.MeshConfig, other.MeshConfig) }

func MeshConfigCollection(configMaps krt.Collection[*v1.ConfigMap], options Options, opts KrtOptions) krt.Singleton[MeshConfig] {
	cmName := "istio"
	if options.Revision != "" && options.Revision != "default" {
		cmName = cmName + "-" + options.Revision
	}
	return krt.NewSingleton[MeshConfig](
		func(ctx krt.HandlerContext) *MeshConfig {
			meshCfg := mesh.DefaultMeshConfig()
			cms := []*v1.ConfigMap{}
			if features.SharedMeshConfig != "" {
				cms = AppendNonNil(cms, krt.FetchOne(ctx, configMaps,
					krt.FilterObjectName(types.NamespacedName{Name: features.SharedMeshConfig, Namespace: options.SystemNamespace})))
			}
			cms = AppendNonNil(cms, krt.FetchOne(ctx, configMaps,
				krt.FilterObjectName(types.NamespacedName{Name: cmName, Namespace: options.SystemNamespace})))

			for _, c := range cms {
				n, err := mesh.ApplyMeshConfig(meshConfigMapData(c), meshCfg)
				if err != nil {
					log.Error(err)
					// TODO: retain last good configuration
					continue
				}
				meshCfg = n
			}
			return &MeshConfig{meshCfg}
		}, opts.WithName("MeshConfig")...,
	)
}

func meshConfigMapData(cm *v1.ConfigMap) string {
	if cm == nil {
		return ""
	}

	cfgYaml, exists := cm.Data["mesh"]
	if !exists {
		return ""
	}

	return cfgYaml
}


type MeshNetworks struct {
	*meshapi.MeshNetworks
}

func (m MeshNetworks) ResourceName() string { return "MeshNetworks" }

func (m MeshNetworks) Equals(other MeshNetworks) bool { return proto.Equal(m.MeshNetworks, other.MeshNetworks) }

func MeshNetworksCollection(configMaps krt.Collection[*v1.ConfigMap], options Options, opts KrtOptions) krt.Singleton[MeshNetworks] {
	cmName := "istio"
	if options.Revision != "" && options.Revision != "default" {
		cmName = cmName + "-" + options.Revision
	}
	return krt.NewSingleton[MeshNetworks](
		func(ctx krt.HandlerContext) *MeshNetworks {
			cm := ptr.Flatten(krt.FetchOne(ctx, configMaps,
				krt.FilterObjectName(types.NamespacedName{Name: cmName, Namespace: options.SystemNamespace})))
			if cm != nil {
				mn, err := mesh.ParseMeshNetworks(meshNetworksMapData(cm))
				if err != nil {
					log.Errorf("failed to read meshNetworks config from ConfigMap: %v", err)
					// TODO: retain last good configuration
					return &MeshNetworks{ptr.Of(mesh.EmptyMeshNetworks())}
				}
				return &MeshNetworks{mn}
			}
			return &MeshNetworks{ptr.Of(mesh.EmptyMeshNetworks())}
		}, opts.WithName("MeshNetworks")...,
	)
}

func meshNetworksMapData(cm *v1.ConfigMap) string {
	if cm == nil {
		return ""
	}

	cfgYaml, exists := cm.Data["meshNetworks"]
	if !exists {
		return ""
	}

	return cfgYaml
}
