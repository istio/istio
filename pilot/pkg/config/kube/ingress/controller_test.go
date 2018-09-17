// Copyright 2018 Istio Authors
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

// Package ingress provides a read-only view of Kubernetes ingress resources
// as an ingress rule configuration type store
package ingress

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/onsi/gomega"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
)

const (
	resync           = 1 * time.Second
	defaultNamespace = "default"
	domainSuffix     = "foo.com"
)

func makeFakeKubeAPIController(mesh *meshconfig.MeshConfig) (model.ConfigStoreCache, *controller) {
	clientSet := fake.NewSimpleClientset()
	configStore := NewController(clientSet, mesh, kube.ControllerOptions{
		WatchedNamespace: defaultNamespace,
		ResyncPeriod:     resync,
		DomainSuffix:     domainSuffix,
	})
	ctl := configStore.(*controller)
	return configStore, ctl
}

func TestControllerGet(t *testing.T) {
	mesh := &meshconfig.MeshConfig{
		// TODO: need add test case for different ingress controller mode
		IngressControllerMode: meshconfig.MeshConfig_DEFAULT,
	}

	testCases := []struct {
		name         string
		ingressName  string
		getNamespace string
		getType      string
		getName      string
		expectResult *model.Config
	}{
		{"wrong ingress namespace", "ingress1", "none",
			model.Gateway.Type, "ingress1", nil},
		{"wrong ingress name", "ingress1", defaultNamespace,
			model.Gateway.Type, "ingress2", nil},
		{"wrong get type", "ingress1", defaultNamespace,
			"none", "ingress1", nil},
		{"none ingress", "", defaultNamespace,
			model.Gateway.Type, "ingress1", nil},
		{"get ingress with GateWay type", "ingress1", defaultNamespace,
			model.Gateway.Type, "ingress1", &model.Config{
				ConfigMeta: model.ConfigMeta{
					Type:      model.Gateway.Type,
					Group:     model.Gateway.Group,
					Version:   model.Gateway.Version,
					Name:      "ingress1" + "-" + model.IstioIngressGatewayName,
					Namespace: model.IstioIngressNamespace,
					Domain:    domainSuffix,
				},
				Spec: &networking.Gateway{
					Selector: model.IstioIngressWorkloadLabels,
					Servers: []*networking.Server{{
						Port: &networking.Port{
							Number:   80,
							Protocol: string(model.ProtocolHTTP),
							Name:     "http-80-ingress-ingress1-default",
						},
						Hosts: []string{"*"},
					}},
				},
			},
		},
		{"get ingress with VirtualService type", "ingress1", defaultNamespace,
			model.VirtualService.Type, "ingress1", &model.Config{
				ConfigMeta: model.ConfigMeta{
					Type:      model.VirtualService.Type,
					Group:     model.VirtualService.Group,
					Version:   model.VirtualService.Version,
					Name:      "foo-com-ingress1" + "-" + model.IstioIngressGatewayName,
					Namespace: model.IstioIngressNamespace,
					Domain:    domainSuffix,
				},
				Spec: &networking.VirtualService{
					Hosts:    []string{"foo.com"},
					Gateways: []string{model.IstioIngressGatewayName},
					Http: []*networking.HTTPRoute{{
						Match: []*networking.HTTPMatchRequest{{
							Uri: &networking.StringMatch{
								MatchType: &networking.StringMatch_Exact{
									Exact: "/foo",
								},
							},
						}},
						Route: []*networking.DestinationWeight{{
							Destination: &networking.Destination{
								Host: "foo.default.svc.foo.com",
								Port: &networking.PortSelector{
									Port: &networking.PortSelector_Number{
										Number: 80,
									},
								},
							},
							Weight: 100,
						}},
					}},
				},
			}},
	}
	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			configStore, ctl := makeFakeKubeAPIController(mesh)
			if c.ingressName != "" {
				ing := generateIngress(c.ingressName)
				ctl.informer.GetStore().Add(ing)
			}
			res := configStore.Get(c.getType, c.getName, c.getNamespace)
			if !reflect.DeepEqual(res, c.expectResult) {
				t.Errorf("\nexpect: %+v, \ngot: %+v", c.expectResult, res)
			}
		})
	}
}

func TestControllerList(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	mesh := &meshconfig.MeshConfig{
		// TODO: need add test case for different ingress ConfigStore mode
		IngressControllerMode: meshconfig.MeshConfig_DEFAULT,
	}

	testCases := []struct {
		name          string
		ingressNum    int
		listNamespace string
		listType      string
		expectNum     int
		expectErr     bool
	}{
		{"wrong namespace", 1, "none", model.Gateway.Type, 0, false},
		{"none ingress", 0, defaultNamespace, model.Gateway.Type, 0, false},
		{"wrong type", 1, defaultNamespace, "none", 0, true},
		{"single ingress with gateway type", 1, defaultNamespace, model.Gateway.Type, 1, false},
		{"single ingress with virtualservice type", 1, defaultNamespace, model.VirtualService.Type, 1, false},
		{"mutil ingress with gateway type", 5, defaultNamespace, model.Gateway.Type, 5, false},
		{"mutil ingress with virtualservice type", 5, defaultNamespace, model.VirtualService.Type, 1, false},
	}

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			configStore, ctl := makeFakeKubeAPIController(mesh)
			if c.ingressNum != 0 {
				createMutilIngress(t, ctl, c.ingressNum)
			}
			res, err := configStore.List(c.listType, c.listNamespace)
			if err != nil && !c.expectErr {
				t.Errorf("Unexpected err: %v", err)
			}
			g.Expect(len(res)).To(gomega.Equal(c.expectNum))
			if len(res) != 0 {
				typ := res[0].Type
				g.Expect(typ).To(gomega.Equal(c.listType))
			}
		})
	}
}

func createMutilIngress(t *testing.T, ctl *controller, ingressNum int) {
	if ingressNum > 0 {
		for i := 0; i < ingressNum; i++ {
			ingressName := fmt.Sprintf("ingress%d", i)
			err := ctl.informer.GetStore().Add(generateIngress(ingressName))
			if err != nil {
				t.Errorf("error add ingress to store: %v", err)
			}
		}
	}
}

func generateIngress(ingressName string) *extensionsv1beta1.Ingress {
	return &extensionsv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ingressName,
			Namespace: defaultNamespace,
		},
		Spec: extensionsv1beta1.IngressSpec{
			Backend: &extensionsv1beta1.IngressBackend{
				ServiceName: "foo",
				ServicePort: intstr.FromInt(80),
			},
			Rules: []extensionsv1beta1.IngressRule{
				{
					Host: "foo.com",
					IngressRuleValue: extensionsv1beta1.IngressRuleValue{
						HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
							Paths: []extensionsv1beta1.HTTPIngressPath{
								{
									Path: "/foo",
									Backend: extensionsv1beta1.IngressBackend{
										ServiceName: "foo",
										ServicePort: intstr.FromInt(80),
									},
								},
							},
						},
					},
				},
			},
		},
	}
}
func addIngress(t *testing.T, cc model.ConfigStoreCache, ingressName string) {
	ingress := &extensionsv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ingressName,
			Namespace: defaultNamespace,
		},
		Spec: extensionsv1beta1.IngressSpec{
			Backend: &extensionsv1beta1.IngressBackend{
				ServiceName: "foo",
				ServicePort: intstr.FromInt(80),
			},
			Rules: []extensionsv1beta1.IngressRule{
				{
					Host: "foo.com",
					IngressRuleValue: extensionsv1beta1.IngressRuleValue{
						HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
							Paths: []extensionsv1beta1.HTTPIngressPath{
								{
									Path: "/foo",
									Backend: extensionsv1beta1.IngressBackend{
										ServiceName: "foo",
										ServicePort: intstr.FromInt(80),
									},
								},
							},
						},
					},
				},
			},
		},
	}
	controller, ok := cc.(*controller)
	if !ok {
		t.Fatalf("wrong interface %v", cc)
	}
	if err := controller.informer.GetStore().Add(ingress); err != nil {
		t.Errorf("Add ingress to cache err: %v", err)
	}
}
