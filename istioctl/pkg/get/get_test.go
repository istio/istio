// Copyright 2019 Istio Authors.
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

package get

import (
	"bytes"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kubernetes/pkg/kubectl/genericclioptions/resource"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

func TestPrintShort(t *testing.T) {
	testGateways := &unstructured.Unstructured{}
	testGateways.Object = make(map[string]interface{})
	testGateways.Object["spec"] = map[string]interface{}{
		"Selector": map[string]string{"istio": "ingressgateway"},
		"Servers": []*networking.Server{
			{
				Port: &networking.Port{
					Number:   80,
					Name:     "http",
					Protocol: "HTTP",
				},
				Hosts: []string{"*"},
			},
		},
	}
	info := &resource.Info{
		Mapping: &meta.RESTMapping{
			GroupVersionKind: schema.GroupVersionKind{
				Group:   model.Gateway.Group,
				Version: model.Gateway.Version,
				Kind:    model.Gateway.Type},
		},
		Name:      "details",
		Namespace: "default",
		Object:    testGateways,
	}
	var out bytes.Buffer
	printShort(&out, []*resource.Info{info})
	output := out.String()
	output = strings.Replace(output, " ", "", -1)
	output = strings.Replace(output, "\n", "", -1)
	expectedOutput := "KindNameNamespaceGroupVersionAgegatewaydetailsdefaultnetworkingv1alpha3<unknown>"
	if output != expectedOutput {
		t.Fatalf("failed to print the VirtualService %q into short", info.Name)
	}
}

func TestPrintYaml(t *testing.T) {
	testVirutalServices := &unstructured.Unstructured{}
	testVirutalServices.Object = make(map[string]interface{})
	testVirutalServices.Object["spec"] = &networking.VirtualService{
		Hosts:    []string{"*"},
		Gateways: []string{"bookinfo-gateway"},
		Http: []*networking.HTTPRoute{
			{
				Match: []*networking.HTTPMatchRequest{
					{
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Exact{Exact: "/productpage"},
						},
					},
					{
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Exact{Exact: "/login"},
						},
					},
					{
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Exact{Exact: "/logout"},
						},
					},
					{
						Uri: &networking.StringMatch{
							MatchType: &networking.StringMatch_Prefix{Prefix: "/api/v1/products"},
						},
					},
				},
				Route: []*networking.HTTPRouteDestination{
					{
						Destination: &networking.Destination{
							Host: "productpage",
							Port: &networking.PortSelector{
								Port: &networking.PortSelector_Number{Number: 80},
							},
						},
					},
				},
			},
		},
	}

	info := &resource.Info{
		Mapping: &meta.RESTMapping{
			GroupVersionKind: schema.GroupVersionKind{
				Group:   model.VirtualService.Group,
				Version: model.VirtualService.Version,
				Kind:    model.VirtualService.Type},
		},
		Name:      "bookinfo",
		Namespace: "default",
		Object:    testVirutalServices,
	}
	var out bytes.Buffer
	printYaml(&out, []*resource.Info{info})
	output := out.String()
	output = strings.Replace(output, " ", "", -1)
	output = strings.Replace(output, "\n", "", -1)
	expectedOutPut := "spec:gateways:-bookinfo-gatewayhosts:-'*'http" +
		":-match:-uri:MatchType:Exact:/productpage-uri:MatchType:Exact:" +
		"/login-uri:MatchType:Exact:/logout-uri:MatchType:Prefix:/api/v1" +
		"/productsroute:-destination:host:productpageport:Port:Number:80---"
	if output != expectedOutPut {
		t.Fatalf("failed to print the VirtualService %q into Yaml", info.Name)
	}
}
