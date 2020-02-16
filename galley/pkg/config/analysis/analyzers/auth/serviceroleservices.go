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
	"strings"

	"istio.io/api/rbac/v1alpha1"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// ServiceRoleServicesAnalyzer checks the validity of services referred in a service role
type ServiceRoleServicesAnalyzer struct{}

var _ analysis.Analyzer = &ServiceRoleServicesAnalyzer{}

// Metadata implements Analyzer
func (s *ServiceRoleServicesAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "auth.ServiceRoleServicesAnalyzer",
		Description: "Checks the validity of services referred in a service role",
		Inputs: collection.Names{
			collections.IstioRbacV1Alpha1Serviceroles.Name(),
			collections.K8SCoreV1Services.Name(),
		},
	}
}

// Analyze implements Analyzer
func (s *ServiceRoleServicesAnalyzer) Analyze(ctx analysis.Context) {
	nsm := s.buildNamespaceServiceMap(ctx)
	ctx.ForEach(collections.IstioRbacV1Alpha1Serviceroles.Name(), func(r *resource.Instance) bool {
		s.analyzeServiceRoleServices(r, ctx, nsm)
		return true
	})
}

// analyzeRoleBinding apply analysis for the service field of the given ServiceRole
func (s *ServiceRoleServicesAnalyzer) analyzeServiceRoleServices(r *resource.Instance, ctx analysis.Context,
	nsm map[resource.Namespace][]util.ScopedFqdn) {

	sr := r.Message.(*v1alpha1.ServiceRole)
	ns := r.Metadata.FullName.Namespace

	for _, rs := range sr.Rules {
		for _, svc := range rs.Services {
			if svc != "*" && !s.existMatchingService(svc, nsm[ns]) {
				// Report when the specific service doesn't exist
				ctx.Report(collections.IstioRbacV1Alpha1Serviceroles.Name(),
					msg.NewReferencedResourceNotFound(r, "service", svc))
			}
		}
	}
}

// buildNamespaceServiceMap returns a map where the index is a namespace and the boolean
func (s *ServiceRoleServicesAnalyzer) buildNamespaceServiceMap(ctx analysis.Context) map[resource.Namespace][]util.ScopedFqdn {
	nsm := map[resource.Namespace][]util.ScopedFqdn{}

	ctx.ForEach(collections.K8SCoreV1Services.Name(), func(r *resource.Instance) bool {
		rns := r.Metadata.FullName.Namespace
		rs := r.Metadata.FullName.Name
		nsm[rns] = append(nsm[rns], util.NewScopedFqdn(string(rns), rns, string(rs)))
		return true
	})

	return nsm
}

func (s *ServiceRoleServicesAnalyzer) existMatchingService(exp string, nsa []util.ScopedFqdn) bool {
	for _, svc := range nsa {
		if serviceMatch(exp, svc) {
			return true
		}
	}
	return false
}

func serviceMatch(expr string, sfqdn util.ScopedFqdn) bool {
	_, fqdn := sfqdn.GetScopeAndFqdn()
	return expr == fqdn || strings.HasPrefix(fqdn, expr) || strings.HasSuffix(fqdn, expr)
}
