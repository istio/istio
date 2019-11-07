//  Copyright 2018 Istio Authors
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

package conversions

import (
	"testing"

	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/intstr"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/resource"
)

func TestIngressConversion(t *testing.T) {
	ingress := v1beta1.IngressSpec{
		Rules: []v1beta1.IngressRule{
			{
				Host: "my.host.com",
				IngressRuleValue: v1beta1.IngressRuleValue{
					HTTP: &v1beta1.HTTPIngressRuleValue{
						Paths: []v1beta1.HTTPIngressPath{
							{
								Path: "/test",
								Backend: v1beta1.IngressBackend{
									ServiceName: "foo",
									ServicePort: intstr.IntOrString{IntVal: 8000},
								},
							},
						},
					},
				},
			},
			{
				Host: "my2.host.com",
				IngressRuleValue: v1beta1.IngressRuleValue{
					HTTP: &v1beta1.HTTPIngressRuleValue{
						Paths: []v1beta1.HTTPIngressPath{
							{
								Path: "/test1.*",
								Backend: v1beta1.IngressBackend{
									ServiceName: "bar",
									ServicePort: intstr.IntOrString{IntVal: 8000},
								},
							},
						},
					},
				},
			},
			{
				Host: "my3.host.com",
				IngressRuleValue: v1beta1.IngressRuleValue{
					HTTP: &v1beta1.HTTPIngressRuleValue{
						Paths: []v1beta1.HTTPIngressPath{
							{
								Path: "/test/*",
								Backend: v1beta1.IngressBackend{
									ServiceName: "bar",
									ServicePort: intstr.IntOrString{IntVal: 8000},
								},
							},
						},
					},
				},
			},
		},
	}
	key := resource.VersionedKey{
		Key: resource.Key{
			Collection: metadata.K8sExtensionsV1beta1Ingresses.Collection,
			FullName:   resource.FullNameFromNamespaceAndName("mock", "i1"),
		},
	}

	ingress2 := v1beta1.IngressSpec{
		Rules: []v1beta1.IngressRule{
			{
				Host: "my.host.com",
				IngressRuleValue: v1beta1.IngressRuleValue{
					HTTP: &v1beta1.HTTPIngressRuleValue{
						Paths: []v1beta1.HTTPIngressPath{
							{
								Path: "/test2",
								Backend: v1beta1.IngressBackend{
									ServiceName: "foo",
									ServicePort: intstr.IntOrString{IntVal: 8000},
								},
							},
						},
					},
				},
			},
		},
	}
	key2 := resource.VersionedKey{
		Key: resource.Key{
			Collection: metadata.K8sExtensionsV1beta1Ingresses.Collection,
			FullName:   resource.FullNameFromNamespaceAndName("mock", "i1"),
		},
	}

	cfgs := map[string]resource.Entry{}
	IngressToVirtualService(key, resource.Metadata{}, &ingress, "mydomain", cfgs)
	IngressToVirtualService(key2, resource.Metadata{}, &ingress2, "mydomain", cfgs)

	if len(cfgs) != 3 {
		t.Error("VirtualServices, expected 3 got ", len(cfgs))
	}

	for n, cfg := range cfgs {
		// Not clear if this is right - should probably be under input ns
		ns, _ := cfg.ID.FullName.InterpretAsNamespaceAndName()
		if ns != "istio-system" {
			t.Errorf("Expected istio-system namespace: %s", ns)
		}

		vs := cfg.Item.(*networking.VirtualService)

		t.Log(vs)
		if n == "my.host.com" {
			if vs.Hosts[0] != "my.host.com" {
				t.Error("Unexpected host", vs)
			}
			if len(vs.Http) != 2 {
				t.Error("Unexpected rules", vs.Http)
			}
		}
	}
}
