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
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// ImageAnalyzer checks the image of auto-injection configured with the running proxies on pods.
type ImageAnalyzer struct{}

var _ analysis.Analyzer = &ImageAnalyzer{}

// injectionConfigMap is a snippet of the sidecar injection ConfigMap
type injectionConfigMap struct {
	Global global `json:"global"`
}

type global struct {
	Hub   string `json:"hub"`
	Tag   string `json:"tag"`
	Proxy proxy  `json:"proxy"`
}

type proxy struct {
	Image string `json:"image"`
}

// Metadata implements Analyzer.
func (a *ImageAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "injection.ImageAnalyzer",
		Description: "Checks the image of auto-injection configured with the running proxies on pods",
		Inputs: collection.Names{
			collections.K8SCoreV1Namespaces.Name(),
			collections.K8SCoreV1Pods.Name(),
			collections.K8SCoreV1Configmaps.Name(),
		},
	}
}

// Analyze implements Analyzer.
func (a *ImageAnalyzer) Analyze(c analysis.Context) {
	proxyImageMap := make(map[string]string)

	// when multiple injector configmaps exist, we may need to assess them respectively.
	c.ForEach(collections.K8SCoreV1Configmaps.Name(), func(r *resource.Instance) bool {
		cmName := r.Metadata.FullName.Name.String()
		if strings.HasPrefix(cmName, "istio-sidecar-injector") {
			cm := r.Message.(*v1.ConfigMap)
			proxyImageMap[cmName] = GetIstioProxyImage(cm)

			return true
		}
		return true
	})

	if len(proxyImageMap) == 0 {
		return
	}

	injectedNamespaces := make(map[string]string)

	// Collect the list of namespaces that have istio injection enabled.
	c.ForEach(collections.K8SCoreV1Namespaces.Name(), func(r *resource.Instance) bool {
		nsRevision, okNewInjectionLabel := r.Metadata.Labels[RevisionInjectionLabelName]
		if r.Metadata.Labels[util.InjectionLabelName] == util.InjectionLabelEnableValue || okNewInjectionLabel {
			if okNewInjectionLabel {
				injectedNamespaces[r.Metadata.FullName.String()] = nsRevision
			} else {
				injectedNamespaces[r.Metadata.FullName.String()] = "default"
			}
		} else {
			return true
		}

		return true
	})

	c.ForEach(collections.K8SCoreV1Pods.Name(), func(r *resource.Instance) bool {
		var injectionCMName string
		pod := r.Message.(*v1.PodSpec)

		if nsRevision, ok := injectedNamespaces[r.Metadata.FullName.Namespace.String()]; ok {
			// Generate the injection configmap name with different revision for every pod
			injectionCMName = util.GetInjectorConfigMapName(nsRevision)
		} else {
			return true
		}

		// If the pod has been annotated with a custom sidecar, then ignore as
		// it always overrides the injector logic.
		if r.Metadata.Annotations["sidecar.istio.io/proxyImage"] != "" {
			return true
		}

		for i, container := range pod.Containers {
			if container.Name != util.IstioProxyName {
				continue
			}

			proxyImage, okImage := proxyImageMap[injectionCMName]
			if !okImage {
				return true
			}
			if container.Image != proxyImage {
				m := msg.NewIstioProxyImageMismatch(r, container.Image, proxyImage)

				if line, ok := util.ErrorLine(r, fmt.Sprintf(util.ImageInContainer, i)); ok {
					m.Line = line
				}

				c.Report(collections.K8SCoreV1Pods.Name(), m)
			}
		}

		return true
	})
}

// GetIstioProxyImage retrieves the proxy image name defined in the sidecar injector
// configuration.
func GetIstioProxyImage(cm *v1.ConfigMap) string {
	var m injectionConfigMap
	if err := json.Unmarshal([]byte(cm.Data[util.InjectionConfigMapValue]), &m); err != nil {
		return ""
	}
	// The injector template has a similar '{ contains "/" ... }' conditional
	if strings.Contains(m.Global.Proxy.Image, "/") {
		return m.Global.Proxy.Image
	}
	return fmt.Sprintf("%s/%s:%s", m.Global.Hub, m.Global.Proxy.Image, m.Global.Tag)
}
