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
	"fmt"
	"net"
	"net/url"
	"strings"

	v1 "k8s.io/api/admissionregistration/v1"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
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

const (
	defaultIstioValidatingWebhookName = "istiod-default-validator"
	istioValidatingWebhookNamePrefix  = "istio-validator"
	istioMutatingWebhookNamePrefix    = "istio-sidecar-injector"
)

// Analyze implements Analyzer
func (s *ExternalControlPlaneAnalyzer) Analyze(c analysis.Context) {
	reportWebhookURL := func(r *resource.Instance, hName string, clientConf v1.WebhookClientConfig) {
		// If defined, it means that an external istiod has been adopted
		if clientConf.URL != nil {
			result, err := lintWebhookURL(*clientConf.URL)
			if err != nil {
				c.Report(gvk.ValidatingWebhookConfiguration, msg.NewInvalidExternalControlPlaneConfig(r, *clientConf.URL, hName, err.Error()))
				return
			}
			if result.isIP() {
				c.Report(gvk.ValidatingWebhookConfiguration, msg.NewExternalControlPlaneAddressIsNotAHostname(r, *clientConf.URL, hName))
			}
		} else if clientConf.Service == nil {
			c.Report(gvk.ValidatingWebhookConfiguration, msg.NewInvalidExternalControlPlaneConfig(r, "", hName, "is blank"))
		}
	}

	c.ForEach(gvk.ValidatingWebhookConfiguration, func(resource *resource.Instance) bool {
		webhookConfig := resource.Message.(*v1.ValidatingWebhookConfiguration)

		// 1. ValidatingWebhookConfiguration: istio-validator or istiod-default-validator(default)
		//			istio-validator{{- if not (eq .Values.revision "") }}-{{ .Values.revision }}{{- end }}-{{ .Values.global.istioNamespace }}
		if webhookConfig.GetName() != "" &&
			(webhookConfig.Name == defaultIstioValidatingWebhookName ||
				strings.HasPrefix(webhookConfig.Name, istioValidatingWebhookNamePrefix)) {
			for _, hook := range webhookConfig.Webhooks {
				reportWebhookURL(resource, hook.Name, hook.ClientConfig)
			}
		}

		return true
	})

	c.ForEach(gvk.MutatingWebhookConfiguration, func(resource *resource.Instance) bool {
		webhookConfig := resource.Message.(*v1.MutatingWebhookConfiguration)

		// 2. MutatingWebhookConfiguration: istio-sidecar-injector
		//            {{- if eq .Release.Namespace "istio-system"}}
		//              name: istio-sidecar-injector{{- if not (eq .Values.revision "") }}-{{ .Values.revision }}{{- end }}
		//            {{- else }}
		//              name: istio-sidecar-injector{{- if not (eq .Values.revision "") }}-{{ .Values.revision }}{{- end }}-{{ .Release.Namespace }}
		//            {{- end }}
		if strings.HasPrefix(webhookConfig.Name, istioMutatingWebhookNamePrefix) {
			for _, hook := range webhookConfig.Webhooks {
				reportWebhookURL(resource, hook.Name, hook.ClientConfig)
			}
		}

		return true
	})
}

type webhookURLResult struct {
	ip       net.IP
	hostName string

	resolvesIPs []net.IP
}

func (r *webhookURLResult) isIP() bool {
	if r == nil {
		return false
	}
	return r.ip != nil
}

func lintWebhookURL(webhookURL string) (result *webhookURLResult, err error) {
	result = &webhookURLResult{}
	parsedWebhookURL, err := url.Parse(webhookURL)
	if err != nil {
		return result, fmt.Errorf("was provided in an invalid format")
	}

	parsedHostname := parsedWebhookURL.Hostname()
	if ip := net.ParseIP(parsedHostname); ip != nil {
		result.ip = ip
		return result, nil
	}

	result.hostName = parsedHostname
	ips, err := net.LookupIP(parsedHostname)
	if err != nil {
		return result, fmt.Errorf("cannot be resolved via a DNS lookup")
	}
	result.resolvesIPs = ips
	if len(ips) == 0 {
		return result, fmt.Errorf("resolves with zero IP addresses")
	}

	return result, nil
}
