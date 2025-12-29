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

package kubemesh

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
)

const (
	MeshConfigKey   = "mesh"
	MeshNetworksKey = "meshNetworks"
)

// NewConfigMapSource builds a MeshConfigSource reading from ConfigMap "name" with key "key".
func NewConfigMapSource(client kube.Client, namespace, name, key string, opts krt.OptionsBuilder) meshwatcher.MeshConfigSource {
	clt := kclient.NewFiltered[*v1.ConfigMap](client, kclient.Filter{
		Namespace:     namespace,
		FieldSelector: fields.OneTermEqualSelector(metav1.ObjectNameField, name).String(),
	})
	cms := krt.WrapClient(clt, opts.WithName("ConfigMap_"+name)...)

	// Start informer immediately instead of with the rest. This is because we use configmapwatcher for
	// single types (so its never shared), and for use cases where we need the results immediately
	// during startup.
	clt.Start(opts.Stop())

	cmKey := types.NamespacedName{Namespace: namespace, Name: name}.String()
	return krt.NewSingleton(func(ctx krt.HandlerContext) *string {
		cm := ptr.Flatten(krt.FetchOne(ctx, cms, krt.FilterKey(cmKey)))
		return meshConfigMapData(cm, key)
	}, opts.WithName(fmt.Sprintf("ConfigMap_%s_%s", name, key))...)
}

func meshConfigMapData(cm *v1.ConfigMap, key string) *string {
	if cm == nil {
		return nil
	}

	cfgYaml, exists := cm.Data[key]
	if !exists {
		return nil
	}

	return &cfgYaml
}
