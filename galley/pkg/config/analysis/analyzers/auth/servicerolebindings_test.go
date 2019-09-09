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
