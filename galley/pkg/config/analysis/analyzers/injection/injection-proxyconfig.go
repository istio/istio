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

package injection

import (
	"encoding/json"
	"strings"

	v1 "k8s.io/api/core/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/util/gogoprotomarshal"
)

// ProxyConfigEnvAnalyzer checks environment variables between configured in mesh ConfigMap and the running proxies on pods.
type ProxyConfigEnvAnalyzer struct{}

var _ analysis.Analyzer = &ProxyConfigEnvAnalyzer{}

// Metadata implements Analyzer.
func (a *ProxyConfigEnvAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "injection.ProxyConfigEnvAnalyzer",
		Description: "Checks environment variables between configured in mesh ConfigMap and the running proxies on pods",
		Inputs: collection.Names{
			collections.K8SCoreV1Namespaces.Name(),
			collections.K8SCoreV1Pods.Name(),
			collections.K8SCoreV1Configmaps.Name(),
		},
	}
}

// Analyze implements Analyzer.
func (a *ProxyConfigEnvAnalyzer) Analyze(c analysis.Context) {
	injectedNamespaces := make(map[string]string)
	// Collect the list of namespaces that have mesh configmap enabled.
	c.ForEach(collections.K8SCoreV1Namespaces.Name(), func(r *resource.Instance) bool {
		nsRevision, okNewInjectionLabel := r.Metadata.Labels[RevisionInjectionLabelName]
		if r.Metadata.Labels[util.InjectionLabelName] == util.InjectionLabelEnableValue && okNewInjectionLabel {
			injectedNamespaces[r.Metadata.FullName.String()] = nsRevision
		} else {
			injectedNamespaces[r.Metadata.FullName.String()] = "default"
		}
		return true
	})

	// need to add istio-system for regular case
	injectedNamespaces[constants.IstioSystemNamespace] = "default"
	proxyConfigMap := make(map[string]*v1.ConfigMap)
	// when multiple mesh configmaps exist, we may need to assess them respectively.
	c.ForEach(collections.K8SCoreV1Configmaps.Name(), func(r *resource.Instance) bool {
		cmName := r.Metadata.FullName.Name.String()
		cmNamespace := r.Metadata.FullName.Namespace.String()
		if nsRevision, ok := injectedNamespaces[cmNamespace]; ok {
			meshCMName := util.GetMeshConfigMapName(nsRevision)
			if cmName == meshCMName {
				proxyConfigMap[meshCMName] = r.Message.(*v1.ConfigMap)
			}
		}
		return true
	})

	c.ForEach(collections.K8SCoreV1Pods.Name(), func(r *resource.Instance) bool {
		pod := r.Message.(*v1.Pod)
		if nsRevision, ok := injectedNamespaces[pod.GetNamespace()]; ok {
			meshCMName := util.GetMeshConfigMapName(nsRevision)
			if cm, cmOK := proxyConfigMap[meshCMName]; cmOK {
				proxyConfig, err := loadMeshConfig(cm)
				if err != nil {
					return true
				}
				for _, container := range pod.Spec.Containers {
					if container.Name != util.IstioProxyName {
						continue
					}
					for _, envItem := range container.Env {
						if pcElem, pcOK := proxyConfig[envItem.Name]; pcOK {
							containerEnvVal := strings.Trim(envItem.Value, "\n")
							if containerEnvVal == "" || containerEnvVal == "{}" {
								continue
							}
							if pcElem == containerEnvVal {
								continue
							} else {
								m := msg.NewIstioProxyConfigMismatch(r, containerEnvVal, pcElem)
								c.Report(collections.K8SCoreV1Pods.Name(), m)
							}
						}
					}
				}
			}
		}
		return true
	})
}

func loadMeshConfig(meshCM *v1.ConfigMap) (map[string]string, error) {
	meshConfig := &meshconfig.MeshConfig{
		DefaultConfig: &meshconfig.ProxyConfig{
			ProxyMetadata: map[string]string{},
		},
	}
	if err := gogoprotomarshal.ApplyYAML(meshCM.Data[util.MeshConfig], meshConfig); err != nil {
		return nil, err
	}
	// Only support to validate environment variable in ProxyMetadata and PROXY_CONFIG
	proxyConfig := meshConfig.DefaultConfig.ProxyMetadata
	bTracing, err := json.Marshal(meshConfig.DefaultConfig.GetTracing())
	if err != nil {
		return nil, err
	}
	// Handle the different field name between environment variable and mesh configmap
	strTracing := strings.Replace(string(bTracing), "Tracer", "tracing", -1)
	sTracing := strings.Trim(strTracing, "\n")
	proxyConfig[util.ProxyConfigEnv] = sTracing

	return proxyConfig, nil
}
