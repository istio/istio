package auth

import (
	"istio.io/api/rbac/v1alpha1"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/processor/metadata"
	"istio.io/istio/galley/pkg/config/resource"
)

// ServiceRoleBindingAnalyzer checks the validity of service role bindings
//TODO: Unit tests
type ServiceRoleBindingAnalyzer struct{}

var _ analysis.Analyzer = &ServiceRoleBindingAnalyzer{}

// Name implements Analyzer
func (s *ServiceRoleBindingAnalyzer) Name() string {
	return "auth.ServiceRoleBindingAnalyzer"
}

// Analyze implements Analyzer
func (s *ServiceRoleBindingAnalyzer) Analyze(ctx analysis.Context) {
	ctx.ForEach(metadata.IstioRbacV1Alpha1Servicerolebindings, func(r *resource.Entry) bool {
		s.analyzeRoleBinding(r, ctx)
		return true
	})
}

func (s *ServiceRoleBindingAnalyzer) analyzeRoleBinding(r *resource.Entry, ctx analysis.Context) {
	srb := r.Item.(*v1alpha1.ServiceRoleBinding)
	ns, _ := r.Metadata.Name.InterpretAsNamespaceAndName()

	if ctx.Find(metadata.IstioRbacV1Alpha1Serviceroles, resource.NewName(ns, srb.RoleRef.Name)) == nil {
		ctx.Report(metadata.IstioRbacV1Alpha1Servicerolebindings, msg.ReferencedResourceNotFound(r, "service role", srb.RoleRef.Name))
	}
}
