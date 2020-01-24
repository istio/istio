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
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// FieldAnalyzer checks for deprecated Istio types and fields
type FieldAnalyzer struct{}

// Currently we don't have an Istio API that tells which Istio APIs are deprecated.
// Run `find . -name "*.proto" -exec grep -i "deprecated=true" \{\} \; -print`
// to see what is deprecated.  This analyzer is hand-crafted.

// Metadata implements analyzer.Analyzer
func (*FieldAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "deprecation.DeprecationAnalyzer",
		Description: "Checks for deprecated Istio types and fields",
		Inputs: collection.Names{
			collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
			collections.IstioNetworkingV1Alpha3Envoyfilters.Name(),
			collections.IstioRbacV1Alpha1Servicerolebindings.Name(),
		},
	}
}

// Analyze implements analysis.Analyzer
func (fa *FieldAnalyzer) Analyze(ctx analysis.Context) {
	ctx.ForEach(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), func(r *resource.Instance) bool {
		fa.analyzeVirtualService(r, ctx)
		return true
	})
	ctx.ForEach(collections.IstioNetworkingV1Alpha3Envoyfilters.Name(), func(r *resource.Instance) bool {
		fa.analyzeEnvoyFilter(r, ctx)
		return true
	})
	ctx.ForEach(collections.IstioRbacV1Alpha1Servicerolebindings.Name(), func(r *resource.Instance) bool {
		fa.analyzeServiceRoleBinding(r, ctx)
		return true
	})
}

func (*FieldAnalyzer) analyzeVirtualService(r *resource.Instance, ctx analysis.Context) {

	vs := r.Message.(*v1alpha3.VirtualService)

	for _, httpRoute := range vs.Http {
		if httpRoute.Fault != nil {
			if httpRoute.Fault.Delay != nil {
				if httpRoute.Fault.Delay.Percent > 0 {
					ctx.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
						msg.NewDeprecated(r, replacedMessage("HTTPRoute.fault.delay.percent", "HTTPRoute.fault.delay.percentage")))
				}
			}
		}
	}
}

func (*FieldAnalyzer) analyzeEnvoyFilter(r *resource.Instance, ctx analysis.Context) {

	ef := r.Message.(*v1alpha3.EnvoyFilter)

	if len(ef.WorkloadLabels) > 0 {
		ctx.Report(collections.IstioNetworkingV1Alpha3Envoyfilters.Name(),
			msg.NewDeprecated(r, replacedMessage("EnvoyFilter.workloadLabels", "EnvoyFilter.workload_selector")))
	}

	if len(ef.Filters) > 0 {
		ctx.Report(collections.IstioNetworkingV1Alpha3Envoyfilters.Name(),
			msg.NewDeprecated(r, uncertainFixMessage("EnvoyFilter.filters")))
	}
}

func (*FieldAnalyzer) analyzeServiceRoleBinding(r *resource.Instance, ctx analysis.Context) {

	srb := r.Message.(*v1alpha1.ServiceRoleBinding)

	for _, subject := range srb.Subjects {
		if subject.Group != "" {
			ctx.Report(collections.IstioRbacV1Alpha1Servicerolebindings.Name(),
				msg.NewDeprecated(r, uncertainFixMessage("ServiceRoleBinding.subjects.group")))
		}
	}
}

func replacedMessage(deprecated, replacement string) string {
	return fmt.Sprintf("%s is deprecated; use %s", deprecated, replacement)
}

// uncertainFixMessage() should be used for fields we don't have a suggested replacement for.
// It is preferable to avoid calling it and find out the replacement suggestion instead.
func uncertainFixMessage(field string) string {
	return fmt.Sprintf("%s is deprecated", field)
}
