// Copyright Istio Authors
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

package gateway

import (
	"testing"

	. "github.com/onsi/gomega"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/aggregate/fakes"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
	"k8s.io/client-go/kubernetes/fake"
	svc "sigs.k8s.io/service-apis/api/v1alpha1"
)

var (
	gatewayClassSpec = &svc.GatewayClassSpec{
		Controller: ControllerName,
	}
	gatewaySpec = &svc.GatewaySpec{
		Class: "gateway-class",
	}
	gateway = &networking.Gateway{
		Servers: []*networking.Server{
			{
				Port: &networking.Port{
					Number:   443,
					Name:     "https",
					Protocol: "HTTP",
				},
				Hosts: []string{"*.secure.example.com"},
			},
		},
	}
	httpRouteSpec = &svc.HTTPRouteSpec{}
)

func TestListInvalidGroupVersionKind(t *testing.T) {
	g := NewGomegaWithT(t)
	clientSet := fake.NewSimpleClientset()
	store := &fakes.ConfigStoreCache{}
	controller := NewController(clientSet, store)

	typ := resource.GroupVersionKind{Kind: "wrong-kind"}
	c, err := controller.List(typ, "ns1")
	g.Expect(c).To(HaveLen(0))
	g.Expect(err).To(HaveOccurred())
}

func TestList(t *testing.T) {
	g := NewGomegaWithT(t)

	clientSet := fake.NewSimpleClientset()
	store := &fakes.ConfigStoreCache{}
	controller := NewController(clientSet, store)

	gwClassType := collections.K8SServiceApisV1Alpha1Gatewayclasses.Resource()
	gwSpecType := collections.K8SServiceApisV1Alpha1Gateways.Resource()
	gwType := collections.IstioNetworkingV1Alpha3Gateways.Resource()

	store.ListReturnsOnCall(0, []model.Config{
		{
			ConfigMeta: model.ConfigMeta{
				Type:      gwClassType.GroupVersionKind().Kind,
				Group:     gwClassType.GroupVersionKind().Group,
				Version:   gwClassType.GroupVersionKind().Version,
				Name:      "gateway-class",
				Namespace: "ns1",
			},
			Spec: gatewayClassSpec,
		},
	}, nil)
	store.ListReturnsOnCall(1, []model.Config{
		{
			ConfigMeta: model.ConfigMeta{
				Type:      gwSpecType.GroupVersionKind().Kind,
				Group:     gwSpecType.GroupVersionKind().Group,
				Version:   gwSpecType.GroupVersionKind().Version,
				Name:      "gateway-spec",
				Namespace: "ns1",
			},
			Spec: gatewaySpec,
		},
	}, nil)
	c, err := controller.List(gwType.GroupVersionKind(), "ns1")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(c).To(HaveLen(1))
}

func TestGetInvalidGroupVersionKind(t *testing.T) {
	g := NewGomegaWithT(t)
	clientSet := fake.NewSimpleClientset()
	store := &fakes.ConfigStoreCache{}
	controller := NewController(clientSet, store)

	typ := resource.GroupVersionKind{Kind: "wrong-kind"}
	c := controller.Get(typ, "name", "ns1")
	g.Expect(c).To(BeNil())
}

func TestGet(t *testing.T) {
	g := NewGomegaWithT(t)

	clientSet := fake.NewSimpleClientset()
	store := &fakes.ConfigStoreCache{}
	controller := NewController(clientSet, store)

	gwClassType := collections.K8SServiceApisV1Alpha1Gatewayclasses.Resource()
	gwSpecType := collections.K8SServiceApisV1Alpha1Gateways.Resource()
	gwType := collections.IstioNetworkingV1Alpha3Gateways.Resource()
	k8sHTTPRouteType := collections.K8SServiceApisV1Alpha1Httproutes.Resource()
	k8sTCPRouteType := collections.K8SServiceApisV1Alpha1Tcproutes.Resource()
	k8sTrafficSplitType := collections.K8SServiceApisV1Alpha1Trafficsplits.Resource()

	store.GetReturnsOnCall(0, &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      gwClassType.GroupVersionKind().Kind,
			Group:     gwClassType.GroupVersionKind().Group,
			Version:   gwClassType.GroupVersionKind().Version,
			Name:      "gateway-class",
			Namespace: "ns1",
		},
		Spec: gatewayClassSpec,
	})
	store.GetReturnsOnCall(1, &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      gwSpecType.GroupVersionKind().Kind,
			Group:     gwSpecType.GroupVersionKind().Group,
			Version:   gwSpecType.GroupVersionKind().Version,
			Name:      "gateway-spec",
			Namespace: "ns1",
		},
		Spec: gatewaySpec,
	})
	store.GetReturnsOnCall(2, &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      k8sHTTPRouteType.GroupVersionKind().Kind,
			Group:     k8sHTTPRouteType.GroupVersionKind().Group,
			Version:   k8sHTTPRouteType.GroupVersionKind().Version,
			Name:      "http-route",
			Namespace: "ns1",
		},
		Spec: httpRouteSpec,
	})
	store.GetReturnsOnCall(3, &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      k8sTCPRouteType.GroupVersionKind().Kind,
			Group:     k8sTCPRouteType.GroupVersionKind().Group,
			Version:   k8sTCPRouteType.GroupVersionKind().Version,
			Name:      "tcp-route",
			Namespace: "ns1",
		},
	})
	store.GetReturnsOnCall(4, &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      k8sTrafficSplitType.GroupVersionKind().Kind,
			Group:     k8sTrafficSplitType.GroupVersionKind().Group,
			Version:   k8sTrafficSplitType.GroupVersionKind().Version,
			Name:      "traffic-split",
			Namespace: "ns1",
		},
	})
	c := controller.Get(gwType.GroupVersionKind(), "", "ns1")
	g.Expect(c).NotTo(BeNil())
}
