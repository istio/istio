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

package gateway

import (
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
)

// SecretAnalyzer checks a gateway's referenced secrets for correctness
type SecretAnalyzer struct{}

var _ analysis.Analyzer = &SecretAnalyzer{}

// Metadata implements analysis.Analyzer
func (a *SecretAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "gateway.SecretAnalyzer",
		Description: "Checks a gateway's referenced secrets for correctness",
		Inputs: collection.Names{
			metadata.IstioNetworkingV1Alpha3Gateways,
			metadata.K8SCoreV1Secrets,
		},
	}
}

// Analyze implements analysis.Analyzer
func (a *SecretAnalyzer) Analyze(ctx analysis.Context) {
	ctx.ForEach(metadata.IstioNetworkingV1Alpha3Gateways, func(r *resource.Entry) bool {
		gw := r.Item.(*v1alpha3.Gateway)

		gwNs, _ := r.Metadata.Name.InterpretAsNamespaceAndName()

		for _, srv := range gw.GetServers() {
			tls := srv.GetTls()
			if tls == nil {
				continue
			}
			//TODO: Is cross-namespace even allowed? Does it require ns/name syntax?
			cn := tls.GetCredentialName()
			if !ctx.Exists(metadata.K8SCoreV1Secrets, resource.NewShortOrFullName(gwNs, cn)) {
				ctx.Report(metadata.IstioNetworkingV1Alpha3Gateways, msg.NewReferencedResourceNotFound(r, "credentialName", cn))
			}
		}
		return true
	})
}
