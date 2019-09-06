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
package virtualservice

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/config/processor/metadata"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
)

func TestWithMatch(t *testing.T) {
	g := NewGomegaWithT(t)

	ctx := fixtures.NewContext()
	ctx.AddEntry(metadata.IstioNetworkingV1Alpha3Virtualservices, resource.NewName("default", "vs"), &v1alpha3.VirtualService{
		Gateways: []string{"gateway"},
	})
	ctx.AddEntry(metadata.IstioNetworkingV1Alpha3Gateways, resource.NewName("default", "gateway"), &v1alpha3.Gateway{})

	(&GatewayAnalyzer{}).Analyze(ctx)
	g.Expect(ctx.Reports).To(BeEmpty())
}

func TestNoMatch(t *testing.T) {
	g := NewGomegaWithT(t)

	ctx := fixtures.NewContext()
	vsName := resource.NewName("default", "vs")
	ctx.AddEntry(metadata.IstioNetworkingV1Alpha3Virtualservices, vsName, &v1alpha3.VirtualService{
		Gateways: []string{"bogus-gateway"},
	})

	(&GatewayAnalyzer{}).Analyze(ctx)
	vsReports := ctx.Reports[metadata.IstioNetworkingV1Alpha3Virtualservices]
	g.Expect(vsReports).To(HaveLen(1))
	g.Expect(vsReports[0].Origin).To(BeEquivalentTo(vsName.String()))
}
