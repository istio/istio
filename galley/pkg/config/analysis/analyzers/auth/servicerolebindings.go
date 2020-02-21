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

package auth

import (
	"istio.io/api/rbac/v1alpha1"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// ServiceRoleBindingAnalyzer checks the validity of service role bindings
type ServiceRoleBindingAnalyzer struct{}

var _ analysis.Analyzer = &ServiceRoleBindingAnalyzer{}

// Metadata implements Analyzer
func (s *ServiceRoleBindingAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "auth.ServiceRoleBindingAnalyzer",
		Description: "Checks the validity of service role bindings",
		Inputs: collection.Names{
			collections.IstioRbacV1Alpha1Serviceroles.Name(),
			collections.IstioRbacV1Alpha1Servicerolebindings.Name(),
		},
	}
}

// Analyze implements Analyzer
func (s *ServiceRoleBindingAnalyzer) Analyze(ctx analysis.Context) {
	ctx.ForEach(collections.IstioRbacV1Alpha1Servicerolebindings.Name(), func(r *resource.Instance) bool {
		s.analyzeRoleBinding(r, ctx)
		return true
	})
}

func (s *ServiceRoleBindingAnalyzer) analyzeRoleBinding(r *resource.Instance, ctx analysis.Context) {
	srb := r.Message.(*v1alpha1.ServiceRoleBinding)
	ns := r.Metadata.FullName.Namespace

	// If no servicerole is defined at all, just skip. The field is required, but that should be enforced elsewhere.
	if srb.RoleRef == nil {
		return
	}

	if !ctx.Exists(collections.IstioRbacV1Alpha1Serviceroles.Name(), resource.NewFullName(ns, resource.LocalName(srb.RoleRef.Name))) {
		ctx.Report(collections.IstioRbacV1Alpha1Servicerolebindings.Name(), msg.NewReferencedResourceNotFound(r, "service role", srb.RoleRef.Name))
	}
}
