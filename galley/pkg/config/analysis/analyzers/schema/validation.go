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
package schema

import (
	"fmt"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pkg/config/schemas"
)

// ValidationAnalyzer runs schema validation as an analyzer and reports any violations as messages
type ValidationAnalyzer struct{}

var _ analysis.Analyzer = &ValidationAnalyzer{}

// Metadata implements Analyzer
func (a *ValidationAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name: "schema.ValidationAnalyzer",
		Inputs: collection.Names{ //TODO
			metadata.IstioRbacV1Alpha1Serviceroles,
			metadata.IstioRbacV1Alpha1Servicerolebindings,
		},
	}
}

// Analyze implements Analyzer
func (a *ValidationAnalyzer) Analyze(ctx analysis.Context) {
	//TODO: How to iterate across everything in the snapshot?
	ctx.ForEach(metadata.IstioNetworkingV1Alpha3Virtualservices, func(r *resource.Entry) bool {
		//TODO: How to get the right schema for each coolection?
		// TODO: Start with a handmade map of resources to validate?
		// TODO: Or parse the schema name out of the collection name?
		schema, exists := schemas.Istio.GetByType(crd.CamelCaseToKebabCase(metadata.IstioNetworkingV1Alpha3Virtualservices.String()))
		//schemas.Istio.GetByType(crd.CamelCaseToKebabCase(un.GetKind()))
		// vs := r.Item.(*v1alpha3.VirtualService)
		ctx.Report(metadata.IstioMeshV1Alpha1MeshConfig, msg.NewInternalError(r, fmt.Sprintf("TODO: %v %v %v", metadata.IstioNetworkingV1Alpha3Virtualservices.String(), schema, exists)))

		//TODO: Collect validation errors
		name, ns := r.Metadata.Name.InterpretAsNamespaceAndName()
		if exists {
			schema.Validate(name, ns, r.Item)
		} else {
			//TODO: Shouldn't happen
		}
		return true
	})

}
