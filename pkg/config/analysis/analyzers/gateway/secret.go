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

package gateway

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/credentials/kube"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
)

// SecretAnalyzer checks a gateway's referenced secrets for correctness
type SecretAnalyzer struct{}

var _ analysis.Analyzer = &SecretAnalyzer{}

// Metadata implements analysis.Analyzer
func (a *SecretAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "gateway.SecretAnalyzer",
		Description: "Checks a gateway's referenced secrets for correctness",
		Inputs: []config.GroupVersionKind{
			gvk.Gateway,
			gvk.Pod,
			gvk.Secret,
		},
	}
}

// Analyze implements analysis.Analyzer
func (a *SecretAnalyzer) Analyze(ctx analysis.Context) {
	ctx.ForEach(gvk.Gateway, func(r *resource.Instance) bool {
		gw := r.Message.(*v1alpha3.Gateway)

		gwNs := getGatewayNamespace(ctx, gw)

		// If we can't find a namespace for the gateway, it's because there's no matching selector. Exit early with a different message.
		if gwNs == "" {

			gwSelector := labels.SelectorFromSet(gw.Selector)
			m := msg.NewReferencedResourceNotFound(r, "selector", labels.SelectorFromSet(gw.Selector).String())

			label := util.ExtractLabelFromSelectorString(gwSelector.String())
			if line, ok := util.ErrorLine(r, fmt.Sprintf(util.GatewaySelector, label)); ok {
				m.Line = line
			}

			ctx.Report(gvk.Gateway, m)
			return true
		}

		for i, srv := range gw.GetServers() {
			tls := srv.GetTls()
			if tls == nil {
				continue
			}

			cn := tls.GetCredentialName()
			if cn == "" {
				continue
			}

			secret := ctx.Find(gvk.Secret, resource.NewShortOrFullName(gwNs, cn))
			if secret == nil {
				m := msg.NewReferencedResourceNotFound(r, "credentialName", cn)

				if line, ok := util.ErrorLine(r, fmt.Sprintf(util.CredentialName, i)); ok {
					m.Line = line
				}

				ctx.Report(gvk.Gateway, m)
				continue
			}
			if !isValidSecret(secret) {
				m := msg.NewInvalidGatewayCredential(r, r.Metadata.FullName.Name.String(), gwNs.String())

				if line, ok := util.ErrorLine(r, fmt.Sprintf(util.CredentialName, i)); ok {
					m.Line = line
				}

				ctx.Report(gvk.Secret, m)
			}
		}
		return true
	})
}

func isValidSecret(secret *resource.Instance) bool {
	s, ok := secret.Message.(*corev1.Secret)
	if !ok {
		return false
	}
	certs, err := kube.ExtractCertInfo(s)
	if err != nil {
		return false
	}
	if err = xds.ValidateCertificate(certs.Cert); err != nil {
		return false
	}
	return true
}

// Gets the namespace for the gateway (in terms of the actual workload selected by the gateway, NOT the namespace of the Gateway CRD)
// Assumes that all selected workloads are in the same namespace, if this is not the case which one's namespace gets returned is undefined.
func getGatewayNamespace(ctx analysis.Context, gw *v1alpha3.Gateway) resource.Namespace {
	var ns resource.Namespace

	gwSelector := labels.SelectorFromSet(gw.Selector)
	ctx.ForEach(gvk.Pod, func(rPod *resource.Instance) bool {
		if gwSelector.Matches(labels.Set(rPod.Metadata.Labels)) {
			ns = rPod.Metadata.FullName.Namespace
			return false
		}
		return true
	})

	return ns
}
