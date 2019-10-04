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

package deprecation

import (
	"fmt"

	"istio.io/api/networking/v1alpha3"
	"istio.io/api/rbac/v1alpha1"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
)

// FieldAnalyzer checks for deprecated Istio types and fields
type FieldAnalyzer struct{}

// Currently we don't have an Istio API that tells which Istio APIs are deprecated.
// Run `find . -name "*.proto" -exec grep -i "deprecated=true" \{\} \; -print`
// to see what is deprecated.  This analyzer is hand-crafted.

// Metadata implements analyzer.Analyzer
func (*FieldAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name: "deprecation.DeprecationAnalyzer",
		Inputs: collection.Names{
			metadata.IstioNetworkingV1Alpha3Virtualservices,
			metadata.IstioNetworkingV1Alpha3Envoyfilters,
			metadata.IstioRbacV1Alpha1Servicerolebindings,
		},
	}
}

// Analyze implements analysis.Analyzer
func (fa *FieldAnalyzer) Analyze(ctx analysis.Context) {
	ctx.ForEach(metadata.IstioNetworkingV1Alpha3Virtualservices, func(r *resource.Entry) bool {
		fa.analyzeVirtualService(r, ctx)
		return true
	})
	ctx.ForEach(metadata.IstioNetworkingV1Alpha3Envoyfilters, func(r *resource.Entry) bool {
		fa.analyzeEnvoyFilter(r, ctx)
		return true
	})
	ctx.ForEach(metadata.IstioRbacV1Alpha1Servicerolebindings, func(r *resource.Entry) bool {
		fa.analyzeServiceRoleBinding(r, ctx)
		return true
	})
}

func (*FieldAnalyzer) analyzeVirtualService(r *resource.Entry, ctx analysis.Context) {

	vs := r.Item.(*v1alpha3.VirtualService)

	for _, httpRoute := range vs.Http {
		if httpRoute.WebsocketUpgrade {
			ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices,
				msg.NewDeprecated(r, ignoredMessage("HTTPRoute.websocket_upgrade")))
		}

		if len(httpRoute.AppendHeaders) > 0 {
			ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices,
				msg.NewDeprecated(r, replacedMessage("HTTPRoute.append_headers", "HTTPRoute.headers.request.add")))
		}
		if len(httpRoute.AppendRequestHeaders) > 0 {
			ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices,
				msg.NewDeprecated(r, replacedMessage("HTTPRoute.append_request_headers", "HTTPRoute.headers.request.add")))
		}
		if len(httpRoute.RemoveRequestHeaders) > 0 {
			ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices,
				msg.NewDeprecated(r, replacedMessage("HTTPRoute.remove_request_headers", "HTTPRoute.headers.request.remove")))
		}
		if len(httpRoute.AppendResponseHeaders) > 0 {
			ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices,
				msg.NewDeprecated(r, replacedMessage("HTTPRoute.append_response_headers", "HTTPRoute.headers.response.add")))
		}
		if len(httpRoute.RemoveResponseHeaders) > 0 {
			ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices,
				msg.NewDeprecated(r, replacedMessage("HTTPRoute.remove_response_headers", "HTTPRoute.headers.response.remove")))
		}

		for _, route := range httpRoute.Route {
			if len(route.AppendRequestHeaders) > 0 {
				ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices,
					msg.NewDeprecated(r, replacedMessage("HTTPRoute.Route.append_request_headers", "HTTPRoute.route.headers.request.add")))
			}
			if len(route.RemoveRequestHeaders) > 0 {
				ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices,
					msg.NewDeprecated(r, replacedMessage("HTTPRoute.Route.remove_request_headers", "HTTPRoute.route.headers.request.remove")))
			}
			if len(route.AppendResponseHeaders) > 0 {
				ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices,
					msg.NewDeprecated(r, replacedMessage("HTTPRoute.Route.append_response_headers", "HTTPRoute.route.headers.response.add")))
			}
			if len(route.RemoveResponseHeaders) > 0 {
				ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices,
					msg.NewDeprecated(r, replacedMessage("HTTPRoute.Route.remove_response_headers", "HTTPRoute.route.headers.response.remove")))
			}
		}

		if httpRoute.Fault != nil {
			if httpRoute.Fault.Delay != nil {
				if httpRoute.Fault.Delay.Percent > 0 {
					ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices,
						msg.NewDeprecated(r, replacedMessage("HTTPRoute.fault.delay.percent", "HTTPRoute.fault.delay.percentage")))
				}
			}
			if httpRoute.Fault.Abort != nil {
				if httpRoute.Fault.Abort.Percent > 0 {
					ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices,
						msg.NewDeprecated(r, replacedMessage("HTTPRoute.fault.abort.percent", "HTTPRoute.fault.abort.percentage")))
				}
			}
		}
	}
}

func (*FieldAnalyzer) analyzeEnvoyFilter(r *resource.Entry, ctx analysis.Context) {

	ef := r.Item.(*v1alpha3.EnvoyFilter)

	if len(ef.WorkloadLabels) > 0 {
		ctx.Report(metadata.IstioNetworkingV1Alpha3Envoyfilters,
			msg.NewDeprecated(r, replacedMessage("EnvoyFilter.workloadLabels", "EnvoyFilter.workload_selector")))
	}

	if len(ef.Filters) > 0 {
		ctx.Report(metadata.IstioNetworkingV1Alpha3Envoyfilters,
			msg.NewDeprecated(r, uncertainFixMessage("EnvoyFilter.filters")))
	}
}

func (*FieldAnalyzer) analyzeServiceRoleBinding(r *resource.Entry, ctx analysis.Context) {

	srb := r.Item.(*v1alpha1.ServiceRoleBinding)

	for _, subject := range srb.Subjects {
		if subject.Group != "" {
			ctx.Report(metadata.IstioRbacV1Alpha1Servicerolebindings,
				msg.NewDeprecated(r, uncertainFixMessage("ServiceRoleBinding.subjects.group")))
		}
	}
}

func ignoredMessage(field string) string {
	return fmt.Sprintf("%s ignored", field)
}

func replacedMessage(deprecated, replacement string) string {
	return fmt.Sprintf("%s is deprecated; use %s", deprecated, replacement)
}

// uncertainFixMessage() should be used for fields we don't have a suggested replacement for.
// It is preferrable to avoid calling it and find out the replacement suggestion instead.
func uncertainFixMessage(field string) string {
	return fmt.Sprintf("%s is deprecated", field)
}
