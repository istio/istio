package auth

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/api/rbac/v1alpha1"
	"istio.io/istio/galley/pkg/config/processor/metadata"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
)

func TestWithMatch(t *testing.T) {
	g := NewGomegaWithT(t)

	ctx := fixtures.NewContext()
	ctx.AddEntry(metadata.IstioRbacV1Alpha1Servicerolebindings, resource.NewName("default", "srb"), &v1alpha1.ServiceRoleBinding{
		RoleRef: &v1alpha1.RoleRef{Name: "service_role"},
	})
	ctx.AddEntry(metadata.IstioRbacV1Alpha1Serviceroles, resource.NewName("default", "service_role"), &v1alpha1.ServiceRole{})

	(&ServiceRoleBindingAnalyzer{}).Analyze(ctx)
	g.Expect(ctx.Reports).To(BeEmpty())
}

func TestNoMatch(t *testing.T) {
	g := NewGomegaWithT(t)

	ctx := fixtures.NewContext()
	srbName := resource.NewName("default", "srb")
	ctx.AddEntry(metadata.IstioRbacV1Alpha1Servicerolebindings, resource.NewName("default", "srb"), &v1alpha1.ServiceRoleBinding{
		RoleRef: &v1alpha1.RoleRef{Name: "bogus_service_role"},
	})

	(&ServiceRoleBindingAnalyzer{}).Analyze(ctx)
	srbReports := ctx.Reports[metadata.IstioRbacV1Alpha1Servicerolebindings]
	g.Expect(srbReports).To(HaveLen(1))
	g.Expect(srbReports[0].Origin).To(BeEquivalentTo(srbName.String()))
}
