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

package externalcontrolplane

import (
	"net"
	"net/url"

	v1 "k8s.io/api/admissionregistration/v1"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/slices"
)

type ExternalControlPlaneAnalyzer struct{}

// Compile-time check that this Analyzer correctly implements the interface
var _ analysis.Analyzer = &ExternalControlPlaneAnalyzer{}

// Metadata implements Analyzer
func (s *ExternalControlPlaneAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "externalcontrolplane.ExternalControlPlaneAnalyzer",
		Description: "Checks that the remote IstioOperator resources reference an external control plane",
		Inputs: []config.GroupVersionKind{
			gvk.ValidatingWebhookConfiguration,
			gvk.MutatingWebhookConfiguration,
		},
	}
}

// Analyze implements Analyzer
func (s *ExternalControlPlaneAnalyzer) Analyze(c analysis.Context) {
	isRemoteCluster := c.Exists(gvk.ValidatingWebhookConfiguration, resource.NewShortOrFullName("", "istio-validator-external-istiod")) &&
		c.Exists(gvk.MutatingWebhookConfiguration, resource.NewShortOrFullName("", "istio-sidecar-injector-external-istiod"))

	if isRemoteCluster {
		requiredValidatingWebhooks := []string{"istio-validator-external-istiod", "istiod-default-validator"}
		c.ForEach(gvk.ValidatingWebhookConfiguration, func(resource *resource.Instance) bool {
			webhookConfig := resource.Message.(*v1.ValidatingWebhookConfiguration)

			if slices.Contains(requiredValidatingWebhooks, webhookConfig.Name) {
				for _, hook := range webhookConfig.Webhooks {
					if hook.ClientConfig.URL != nil {

						webhookLintResults := lintWebhookURL(*hook.ClientConfig.URL)

						switch webhookLintResults {
						case "":
							return true

						case "is an IP address instead of a hostname":
							c.Report(gvk.ValidatingWebhookConfiguration, msg.NewExternalControlPlaneAddressIsNotAHostname(resource, *hook.ClientConfig.URL, hook.Name))
							return false

						default:
							c.Report(gvk.ValidatingWebhookConfiguration, msg.NewInvalidExternalControlPlaneConfig(resource, *hook.ClientConfig.URL, hook.Name, webhookLintResults))
							return false
						}

					} else {
						c.Report(gvk.ValidatingWebhookConfiguration, msg.NewInvalidExternalControlPlaneConfig(resource, "", hook.Name, "is blank"))
						return false
					}
				}
			}

			return true
		})

		requiredMutatingWebhooks := []string{"istio-sidecar-injector-external-istiod"}
		c.ForEach(gvk.MutatingWebhookConfiguration, func(resource *resource.Instance) bool {
			webhookConfig := resource.Message.(*v1.MutatingWebhookConfiguration)

			if slices.Contains(requiredMutatingWebhooks, webhookConfig.Name) {
				for _, hook := range webhookConfig.Webhooks {
					if hook.ClientConfig.URL != nil {

						webhookLintResults := lintWebhookURL(*hook.ClientConfig.URL)

						switch webhookLintResults {
						case "":
							return true

						case "is an IP address instead of a hostname":
							c.Report(gvk.ValidatingWebhookConfiguration, msg.NewExternalControlPlaneAddressIsNotAHostname(resource, *hook.ClientConfig.URL, hook.Name))
							return false

						default:
							c.Report(gvk.ValidatingWebhookConfiguration, msg.NewInvalidExternalControlPlaneConfig(resource, *hook.ClientConfig.URL, hook.Name, webhookLintResults))
							return false
						}

					} else {
						c.Report(gvk.ValidatingWebhookConfiguration, msg.NewInvalidExternalControlPlaneConfig(resource, "", hook.Name, "is blank"))
						return false
					}
				}
			}

			return true
		})
	}
}

func lintWebhookURL(webhookURL string) string {
	parsedWebhookURL, err := url.Parse(webhookURL)
	if err != nil {
		return "was provided in an invalid format"
	}

	parsedHostname := parsedWebhookURL.Hostname()
	if net.ParseIP(parsedHostname) != nil {
		return "is an IP address instead of a hostname"
	}

	return ""
}
