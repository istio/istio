// Copyright 2019 Istio Authors
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

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// VersionAnalyzer checks the version of auto-injection configured with the running proxies on pods.
type VersionAnalyzer struct{}

var _ analysis.Analyzer = &VersionAnalyzer{}

const sidecarInjectorConfigName = "istio-sidecar-injector"

// injectionConfigMap is a snippet of the sidecar injection ConfigMap
type injectionConfigMap struct {
	Global global `json:"global"`
}

type global struct {
	Tag string `json:"tag"`
}

// Metadata implements Analyzer.
func (a *VersionAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "injection.VersionAnalyzer",
		Description: "Checks the version of auto-injection configured with the running proxies on pods",
		Inputs: collection.Names{
			collections.K8SCoreV1Namespaces.Name(),
			collections.K8SCoreV1Pods.Name(),
			collections.K8SCoreV1Configmaps.Name(),
		},
	}
}

// Analyze implements Analyzer.
func (a *VersionAnalyzer) Analyze(c analysis.Context) {
	var injectionVersion string

	// relying on just istio-system namespace for now as it align with the implementation.
	r := c.Find(collections.K8SCoreV1Configmaps.Name(), resource.NewFullName("istio-system", sidecarInjectorConfigName))
	if r != nil {
		cm := r.Message.(*v1.ConfigMap)

		injectionVersion = getIstioImageTag(cm)
	}

	if injectionVersion == "" {
		return
	}

	injectedNamespaces := make(map[string]struct{})

	// Collect the list of namespaces that have istio injection enabled.
	c.ForEach(collections.K8SCoreV1Namespaces.Name(), func(r *resource.Instance) bool {
		if r.Metadata.Labels[InjectionLabelName] == InjectionLabelEnableValue {
			injectedNamespaces[r.Metadata.FullName.String()] = struct{}{}
		}

		return true
	})

	c.ForEach(collections.K8SCoreV1Pods.Name(), func(r *resource.Instance) bool {
		pod := r.Message.(*v1.Pod)

		if _, ok := injectedNamespaces[pod.GetNamespace()]; !ok {
			return true
		}

		// If the pod has been annotated with a custom sidecar, then ignore as
		// it always overrides the injector logic.
		if r.Metadata.Annotations["sidecar.istio.io/proxyImage"] != "" {
			return true
		}

		for _, container := range pod.Spec.Containers {
			if container.Name != istioProxyName {
				continue
			}
			// Attempt to parse out the version of the proxy.
			v := getContainerNameVersion(&container)
			// We can't check anything without a version; skip the pod.
			if v == "" {
				continue
			}

			if v != injectionVersion {
				c.Report(collections.K8SCoreV1Pods.Name(), msg.NewIstioProxyVersionMismatch(r, v, injectionVersion))
			}
		}

		return true
	})
}

// getIstioImageTag retrieves the image tag defined in the sidecar injector
// configuration.
func getIstioImageTag(cm *v1.ConfigMap) string {
	var m injectionConfigMap
	if err := json.Unmarshal([]byte(cm.Data["values"]), &m); err != nil {
		return ""
	}
	return m.Global.Tag
}

// getContainerNameVersion parses the name and version from a container image.
// If the version is not specified or can't be found, version is the empty
// string.
func getContainerNameVersion(c *v1.Container) (version string) {
	parts := strings.Split(c.Image, ":")
	if len(parts) != 2 {
		return ""
	}
	version = parts[1]
	return
}
