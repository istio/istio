//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package galley

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/yml"
)

func TestResourceLifecycle(t *testing.T) {
	g := NewGomegaWithT(t)

	ctx := framework.NewContext(t)

	ns := namespace.NewOrFail(t, ctx, "fake", false)

	c := NewOrFail(t, ctx, Config{}).(*nativeComponent)

	gateway := `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: some-ingress
spec:
  servers:
  - port:
      number: 80
      name: http
      protocol: http
    hosts:
    - "*.example.com"
`

	virtualService := `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: route-for-myapp
spec:
  hosts:
  - some.example.com
  gateways:
  - some-ingress
  http:
  - route:
    - destination:
        host: some.example.internal
`

	// Create resources.
	c.ApplyConfigOrFail(t, ns, yml.JoinString(gateway, virtualService))
	fileInfos, err := readFileInfos(c.configDir)
	g.Expect(err).To(BeNil())
	g.Expect(len(fileInfos)).To(Equal(2))
	g.Expect(len(fileInfos[0].resources)).To(Equal(1))
	g.Expect(len(fileInfos[1].resources)).To(Equal(2))
	g.Expect(fileInfos[0].resources[0].descriptor).To(Equal(yml.Descriptor{
		Kind:       "attributemanifest",
		APIVersion: "config.istio.io/v1alpha2",
		Metadata: yml.Metadata{
			Name:      "istioproxy",
			Namespace: "istio-system",
		},
	}))
	g.Expect(fileInfos[1].resources[0].descriptor).To(Equal(yml.Descriptor{
		Kind:       "Gateway",
		APIVersion: "networking.istio.io/v1alpha3",
		Metadata: yml.Metadata{
			Name:      "some-ingress",
			Namespace: ns.Name(),
		},
	}))
	g.Expect(fileInfos[1].resources[1].descriptor).To(Equal(yml.Descriptor{
		Kind:       "VirtualService",
		APIVersion: "networking.istio.io/v1alpha3",
		Metadata: yml.Metadata{
			Name:      "route-for-myapp",
			Namespace: ns.Name(),
		},
	}))

	gateway = `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: some-ingress
spec:
  servers:
  - port:
      number: 90
      name: http
      protocol: http
    hosts:
    - "*.example.com"
`

	// Update the gateway resource.
	c.ApplyConfigOrFail(t, ns, yml.JoinString(gateway, virtualService))
	fileInfos, err = readFileInfos(c.configDir)
	g.Expect(err).To(BeNil())
	g.Expect(len(fileInfos)).To(Equal(2))
	g.Expect(len(fileInfos[0].resources)).To(Equal(1))
	g.Expect(len(fileInfos[1].resources)).To(Equal(2))
	gatewayResource := newFileResourceMap(fileInfos)[yml.Descriptor{
		Kind:       "Gateway",
		APIVersion: "networking.istio.io/v1alpha3",
		Metadata: yml.Metadata{
			Name:      "some-ingress",
			Namespace: ns.Name(),
		},
	}]
	g.Expect(gatewayResource).ToNot(BeNil())
	g.Expect(gatewayResource.content).To(ContainSubstring("number: 90"))

	// Delete the gateway resource
	c.DeleteConfigOrFail(t, ns, gateway)
	fileInfos, err = readFileInfos(c.configDir)
	g.Expect(err).To(BeNil())
	g.Expect(len(fileInfos)).To(Equal(2))
	g.Expect(len(fileInfos[0].resources)).To(Equal(1))
	g.Expect(len(fileInfos[1].resources)).To(Equal(1))
	g.Expect(fileInfos[1].resources[0].descriptor).To(Equal(yml.Descriptor{
		Kind:       "VirtualService",
		APIVersion: "networking.istio.io/v1alpha3",
		Metadata: yml.Metadata{
			Name:      "route-for-myapp",
			Namespace: ns.Name(),
		},
	}))
}

func TestMain(m *testing.M) {
	framework.
		NewSuite("galley_component_unit_tests", m).
		RequireEnvironment(environment.Native).
		Run()
}
