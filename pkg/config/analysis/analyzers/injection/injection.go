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

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// Analyzer checks conditions related to Istio sidecar injection.
type Analyzer struct{}

var _ analysis.Analyzer = &Analyzer{}

// We assume that enablement is via an istio-injection=enabled or istio.io/rev namespace label
// In theory, there can be alternatives using Mutatingwebhookconfiguration, but they're very uncommon
// See https://istio.io/docs/ops/troubleshooting/injection/ for more info.
var (
	RevisionInjectionLabelName = label.IoIstioRev.Name
)

// Metadata implements Analyzer
func (a *Analyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "injection.Analyzer",
		Description: "Checks conditions related to Istio sidecar injection",
		Inputs: collection.Names{
			collections.K8SCoreV1Namespaces.Name(),
			collections.K8SCoreV1Pods.Name(),
			collections.K8SCoreV1Configmaps.Name(),
		},
	}
}

// Analyze implements Analyzer
func (a *Analyzer) Analyze(c analysis.Context) {
	enableNamespacesByDefault := false
	injectedNamespaces := make(map[string]bool)

	c.ForEach(collections.K8SCoreV1Namespaces.Name(), func(r *resource.Instance) bool {
		if r.Metadata.FullName.String() == constants.IstioSystemNamespace {
			return true
		}

		ns := r.Metadata.FullName.String()
		if util.IsSystemNamespace(resource.Namespace(ns)) {
			return true
		}

		injectionLabel := r.Metadata.Labels[util.InjectionLabelName]
		nsRevision, okNewInjectionLabel := r.Metadata.Labels[RevisionInjectionLabelName]

		// verify the enableNamespacesByDefault flag in injection configmaps
		c.ForEach(collections.K8SCoreV1Configmaps.Name(), func(r *resource.Instance) bool {
			injectionCMName := util.GetInjectorConfigMapName(nsRevision)
			if r.Metadata.FullName.Name.String() == injectionCMName {
				cm := r.Message.(*v1.ConfigMap)
				enableNamespacesByDefault = GetEnableNamespacesByDefaultFromInjectedConfigMap(cm)
				return false
			}
			return true
		})

		if injectionLabel == "" && !okNewInjectionLabel {
			// if Istio is installed with sidecarInjectorWebhook.enableNamespacesByDefault=true
			// (in the istio-sidecar-injector configmap), we need to reverse this logic and treat this as an injected namespace
			if enableNamespacesByDefault {
				m := msg.NewNamespaceInjectionEnabledByDefault(r)
				c.Report(collections.K8SCoreV1Namespaces.Name(), m)
				return true
			}

			m := msg.NewNamespaceNotInjected(r, ns, ns)

			if line, ok := util.ErrorLine(r, fmt.Sprintf(util.MetadataName)); ok {
				m.Line = line
			}

			c.Report(collections.K8SCoreV1Namespaces.Name(), m)
			return true
		}

		if okNewInjectionLabel {
			if injectionLabel != "" {

				m := msg.NewNamespaceMultipleInjectionLabels(r, ns, ns)

				if line, ok := util.ErrorLine(r, fmt.Sprintf(util.MetadataName)); ok {
					m.Line = line
				}

				c.Report(collections.K8SCoreV1Namespaces.Name(), m)
				return true
			}
		} else if injectionLabel != util.InjectionLabelEnableValue {
			// If legacy label has any value other than the enablement value, they are deliberately not injecting it, so ignore
			return true
		}

		injectedNamespaces[ns] = true

		return true
	})

	c.ForEach(collections.K8SCoreV1Pods.Name(), func(r *resource.Instance) bool {
		pod := r.Message.(*v1.PodSpec)

		if !injectedNamespaces[r.Metadata.FullName.Namespace.String()] {
			return true
		}

		// If a pod has injection explicitly disabled, no need to check further
		inj := r.Metadata.Annotations[annotation.SidecarInject.Name]
		if v, ok := r.Metadata.Labels[annotation.SidecarInject.Name]; ok {
			inj = v
		}
		if strings.EqualFold(inj, "false") {
			return true
		}

		if pod.HostNetwork {
			return true
		}

		proxyImage := ""
		for _, container := range pod.Containers {
			if container.Name == util.IstioProxyName {
				proxyImage = container.Image
				break
			}
		}

		if proxyImage == "" {
			c.Report(collections.K8SCoreV1Pods.Name(), msg.NewPodMissingProxy(r, r.Metadata.FullName.String()))
		}

		return true
	})
}

// GetInjectedConfigMapValuesStruct retrieves value of sidecarInjectorWebhook.enableNamespacesByDefault
// defined in the sidecar injector configuration.
func GetEnableNamespacesByDefaultFromInjectedConfigMap(cm *v1.ConfigMap) bool {
	var injectedCMValues map[string]any
	if err := json.Unmarshal([]byte(cm.Data[util.InjectionConfigMapValue]), &injectedCMValues); err != nil {
		return false
	}

	injectionEnable := injectedCMValues[util.InjectorWebhookConfigKey].(map[string]any)[util.InjectorWebhookConfigValue]
	return injectionEnable.(bool)
}
