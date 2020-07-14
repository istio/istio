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
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
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
			collections.IstioNetworkingV1Alpha3Gateways.Name(),
			collections.K8SCoreV1Pods.Name(),
			collections.K8SCoreV1Secrets.Name(),
		},
	}
}

// Analyze implements analysis.Analyzer
func (a *SecretAnalyzer) Analyze(ctx analysis.Context) {
	ctx.ForEach(collections.IstioNetworkingV1Alpha3Gateways.Name(), func(r *resource.Instance) bool {
		gw := r.Message.(*v1alpha3.Gateway)

		gwNs := getGatewayNamespace(ctx, gw)

		// If we can't find a namespace for the gateway, it's because there's no matching selector. Exit early with a different message.
		if gwNs == "" {
			ctx.Report(collections.IstioNetworkingV1Alpha3Gateways.Name(),
				msg.NewReferencedResourceNotFound(r, "selector", labels.SelectorFromSet(gw.Selector).String()))
			return true
		}

		for _, srv := range gw.GetServers() {
			tls := srv.GetTls()
			if tls == nil {
				continue
			}

			cn := tls.GetCredentialName()
			if cn == "" {
				continue
			}

			if !ctx.Exists(collections.K8SCoreV1Secrets.Name(), resource.NewShortOrFullName(gwNs, cn)) {
				ctx.Report(collections.IstioNetworkingV1Alpha3Gateways.Name(), msg.NewReferencedResourceNotFound(r, "credentialName", cn))
			}
		}
		return true
	})
}

// Gets the namespace for the gateway (in terms of the actual workload selected by the gateway, NOT the namespace of the Gateway CRD)
// Assumes that all selected workloads are in the same namespace, if this is not the case which one's namespace gets returned is undefined.
func getGatewayNamespace(ctx analysis.Context, gw *v1alpha3.Gateway) resource.Namespace {
	var ns resource.Namespace

	gwSelector := labels.SelectorFromSet(gw.Selector)
	ctx.ForEach(collections.K8SCoreV1Pods.Name(), func(rPod *resource.Instance) bool {
		pod := rPod.Message.(*v1.Pod)
		if gwSelector.Matches(labels.Set(pod.ObjectMeta.Labels)) {
			ns = rPod.Metadata.FullName.Namespace
			return false
		}
		return true
	})

	return ns
}
