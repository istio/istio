package externalcontrolplane

import (
	"fmt"
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
		Description: "Checks that the remote IstioOperator resources reference an external controle plane",
		Inputs: []config.GroupVersionKind{
			gvk.ValidatingWebhookConfiguration,
			gvk.MutatingWebhookConfiguration,
		},
	}
}

// Analyze implements Analyzer
func (s *ExternalControlPlaneAnalyzer) Analyze(c analysis.Context) {
	isRemoteCluster := c.Exists(gvk.ValidatingWebhookConfiguration, resource.NewShortOrFullName("", "istio-validator-external-istiod")) && c.Exists(gvk.MutatingWebhookConfiguration, resource.NewShortOrFullName("", "istio-sidecar-injector-external-istiod"))

	if isRemoteCluster {
		requiredValidatingWebhooks := []string{"istio-validator-external-istiod", "istiod-default-validator"}
		c.ForEach(gvk.ValidatingWebhookConfiguration, func(resource *resource.Instance) bool {
			webhookConfig := resource.Message.(*v1.ValidatingWebhookConfiguration)

			if slices.Contains(requiredValidatingWebhooks, webhookConfig.Name) {
				for _, hook := range webhookConfig.Webhooks {
					if hook.ClientConfig.URL != nil {

						webhookLintResults := lintWebhookUrl(*hook.ClientConfig.URL, hook.Name)
						if webhookLintResults != "" {
							c.Report(gvk.ValidatingWebhookConfiguration, msg.NewInvalidWebhook(resource, webhookLintResults))
							return false
						}

					} else {
						c.Report(gvk.ValidatingWebhookConfiguration, msg.NewInvalidWebhook(resource, fmt.Sprintf("The webhook (%v) does not contain a URL.", hook.Name)))
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

						webhookLintResults := lintWebhookUrl(*hook.ClientConfig.URL, hook.Name)
						if webhookLintResults != "" {
							c.Report(gvk.ValidatingWebhookConfiguration, msg.NewInvalidWebhook(resource, webhookLintResults))
							return false
						}

					} else {
						c.Report(gvk.MutatingWebhookConfiguration, msg.NewInvalidWebhook(resource, fmt.Sprintf("The webhook (%v) does not contain a URL.", hook.Name)))
						return false
					}
				}
			}

			return true
		})
	}
}

func lintWebhookUrl(webhookURL string, webhookName string) string {
	url, err := url.Parse(webhookURL)
	if err != nil {
		return fmt.Sprintf("Unable to parse the domain from the URL (%v) set in the webhook (%v).", webhookURL, webhookName)
	}

	domainName := url.Hostname()
	ips, err := net.LookupIP(domainName)
	if err != nil {
		return fmt.Sprintf("Unable to resolve the domain (%v) associated with the webhook (%v).", domainName, webhookName)
	}

	if len(ips) == 0 {
		return fmt.Sprintf("No IP addresses are associated with the domain (%v) assigned to the webhook (%v).", domainName, webhookName)
	}

	return ""
}
