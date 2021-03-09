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

package clioptions

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-multierror"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/version"
)

func GetMeshConfigFromConfigMap(client kube.Client, istioNamespace, revision string) (*meshconfig.MeshConfig, error) {
	defaultMeshConfigMapName := "istio"
	configMapKey := "mesh"

	meshConfigMapName := defaultMeshConfigMapName
	if revision != "" {
		meshConfigMapName = fmt.Sprintf("%s-%s", defaultMeshConfigMapName, revision)
	}
	meshConfigMap, err := client.CoreV1().ConfigMaps(istioNamespace).Get(context.TODO(), meshConfigMapName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf(`could not read valid configmap %q from namespace %q: %v
Use --meshConfigFile or run with -i <istioSystemNamespace> and ensure configmap %q exists`,
			meshConfigMapName, istioNamespace, err, defaultMeshConfigMapName)
	}

	// values in the data are strings, while proto might use a
	// different data type.  therefore, we have to get a value by a
	// key
	configYaml, exists := meshConfigMap.Data[configMapKey]
	if !exists {
		return nil, fmt.Errorf("missing configuration map key %q", configMapKey)
	}
	cfg, err := mesh.ApplyMeshConfigDefaults(configYaml)
	if err != nil {
		err = multierror.Append(err, fmt.Errorf("istioctl version %s cannot parse mesh config",
			version.Info.Version))
	}
	return cfg, err
}
