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

func TestValidHostAndSubset(t *testing.T) {
	g := NewGomegaWithT(t)

	ctx := fixtures.NewContext()
	ctx.AddEntry(metadata.IstioNetworkingV1Alpha3SyntheticServiceentries, resource.NewName("default", "host1"), &v1alpha3.ServiceEntry{})
	ctx.AddEntry(metadata.IstioNetworkingV1Alpha3Serviceentries, resource.NewName("default", "external1"), &v1alpha3.ServiceEntry{Hosts: []string{"example.com"}})
	ctx.AddEntry(metadata.IstioNetworkingV1Alpha3Destinationrules, resource.NewName("default", "rule"), createDestinationRule("host1", "v1"))

	// Short name with match should succeed
	vsValid := resource.NewName("default", "vsValid")
	ctx.AddEntry(metadata.IstioNetworkingV1Alpha3Virtualservices, vsValid, createVirtualService("host1", "v1"))

	// FQDN with match should succeed
	vsValidFqdn := resource.NewName("default", "vsValidFqdn")
	ctx.AddEntry(metadata.IstioNetworkingV1Alpha3Virtualservices, vsValidFqdn, createVirtualService("host1.default.svc.cluster.local", "v1"))

	// External service (defined as a ServiceEntry) match should succeed
	vsValidExternalService := resource.NewName("default", "vsValidExternalService")
	ctx.AddEntry(metadata.IstioNetworkingV1Alpha3Virtualservices, vsValidExternalService, createVirtualService("example.com", ""))

	(&DestinationAnalyzer{}).Analyze(ctx)
	g.Expect(ctx.Reports).To(BeEmpty())
}

func TestMissingHost(t *testing.T) {
	g := NewGomegaWithT(t)

	ctx := fixtures.NewContext()
	vsName := resource.NewName("default", "vs")
	ctx.AddEntry(metadata.IstioNetworkingV1Alpha3Virtualservices, vsName, createVirtualService("bogus-host", "v1"))

	(&DestinationAnalyzer{}).Analyze(ctx)
	vsReports := ctx.Reports[metadata.IstioNetworkingV1Alpha3Virtualservices]
	g.Expect(vsReports).To(HaveLen(1))
	g.Expect(vsReports[0].Origin).To(BeEquivalentTo(vsName.String()))
}

func TestMissingSubset(t *testing.T) {
	g := NewGomegaWithT(t)

	ctx := fixtures.NewContext()
	vsName := resource.NewName("default", "vs")
	ctx.AddEntry(metadata.IstioNetworkingV1Alpha3Virtualservices, vsName, createVirtualService("host1", "bogus-subset"))
	ctx.AddEntry(metadata.IstioNetworkingV1Alpha3SyntheticServiceentries, resource.NewName("default", "host1"), &v1alpha3.ServiceEntry{})
	ctx.AddEntry(metadata.IstioNetworkingV1Alpha3Destinationrules, resource.NewName("default", "rule"), createDestinationRule("host1", "v1"))

	(&DestinationAnalyzer{}).Analyze(ctx)
	vsReports := ctx.Reports[metadata.IstioNetworkingV1Alpha3Virtualservices]
	g.Expect(vsReports).To(HaveLen(1))
	g.Expect(vsReports[0].Origin).To(BeEquivalentTo(vsName.String()))
}

func createVirtualService(host, subset string) *v1alpha3.VirtualService {
	return &v1alpha3.VirtualService{
		Http: []*v1alpha3.HTTPRoute{
			{
				Route: []*v1alpha3.HTTPRouteDestination{
					{
						Destination: &v1alpha3.Destination{
							Host:   host,
							Subset: subset,
						},
					},
				},
			},
		},
	}
}

func createDestinationRule(host string, subsetNames ...string) *v1alpha3.DestinationRule {
	subsets := make([]*v1alpha3.Subset, 0)
	for _, name := range subsetNames {
		subsets = append(subsets, &v1alpha3.Subset{Name: name})
	}

	return &v1alpha3.DestinationRule{
		Host:    host,
		Subsets: subsets,
	}
}
