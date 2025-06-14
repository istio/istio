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

package model

import (
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

func setupController(t *testing.T, defaultExportTo sets.Set[visibility.Instance], objs ...config.Config) *VirtualServiceController {
	stop := test.NewStop(t)
	meshHolder := meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{
		DefaultVirtualServiceExportTo: slices.Map(defaultExportTo.UnsortedList(), func(i visibility.Instance) string {
			return string(i)
		}),
	})
	store := NewFakeStore()
	for _, o := range objs {
		store.Create(o)
	}
	controller := NewVirtualServiceController(
		store,
		VSControllerOptions{KrtDebugger: krt.GlobalDebugHandler},
		meshHolder,
	)
	go controller.Run(stop)
	kube.WaitForCacheSync("test", stop, controller.HasSynced)

	return controller
}

func dumpOnFailure(t *testing.T, debugger *krt.DebugHandler) {
	t.Cleanup(func() {
		if t.Failed() {
			b, _ := yaml.Marshal(debugger)
			t.Log(string(b))
		}
	})
}

func TestListInvalidGroupVersionKind(t *testing.T) {
	controller := setupController(t, sets.New(visibility.Public))

	typ := config.GroupVersionKind{Kind: "wrong-kind"}
	c := controller.List(typ, "ns1")
	assert.Equal(t, len(c), 0)
}

func TestControllerMergeVirtualServices(t *testing.T) {
	independentVs := config.Config{
		Meta: config.Meta{
			Name:             "virtual-service",
			Namespace:        "default",
			GroupVersionKind: gvk.VirtualService,
		},
		Spec: &v1alpha3.VirtualService{
			Hosts:    []string{"example.org"},
			Gateways: []string{"default/gateway"},
			Http: []*v1alpha3.HTTPRoute{
				{
					Route: []*v1alpha3.HTTPRouteDestination{
						{
							Destination: &v1alpha3.Destination{
								Host: "example.org",
								Port: &v1alpha3.PortSelector{
									Number: 80,
								},
							},
						},
					},
				},
			},
		},
	}

	rootVs := config.Config{
		Meta: config.Meta{
			Name:             "root-vs",
			Namespace:        "istio-system",
			GroupVersionKind: gvk.VirtualService,
		},
		Spec: &v1alpha3.VirtualService{
			Hosts:    []string{"*.org"},
			Gateways: []string{"istio-system/gateway"},
			Http: []*v1alpha3.HTTPRoute{
				{
					Match: []*v1alpha3.HTTPMatchRequest{
						{
							Uri: &v1alpha3.StringMatch{
								MatchType: &v1alpha3.StringMatch_Prefix{Prefix: "/productpage"},
							},
						},
						{
							Uri: &v1alpha3.StringMatch{
								MatchType: &v1alpha3.StringMatch_Exact{Exact: "/login"},
							},
						},
					},
					Delegate: &v1alpha3.Delegate{
						Name:      "productpage-vs",
						Namespace: "default",
					},
				},
				{
					Route: []*v1alpha3.HTTPRouteDestination{
						{
							Destination: &v1alpha3.Destination{
								Host: "example.org",
								Port: &v1alpha3.PortSelector{
									Number: 80,
								},
							},
						},
					},
				},
			},
		},
	}

	defaultVs := config.Config{
		Meta: config.Meta{
			Name:             "default-vs",
			Namespace:        "default",
			GroupVersionKind: gvk.VirtualService,
		},
		Spec: &v1alpha3.VirtualService{
			Hosts:    []string{"*.org"},
			Gateways: []string{"default/gateway"},
			Http: []*v1alpha3.HTTPRoute{
				{
					Match: []*v1alpha3.HTTPMatchRequest{
						{
							Uri: &v1alpha3.StringMatch{
								MatchType: &v1alpha3.StringMatch_Prefix{Prefix: "/productpage"},
							},
						},
						{
							Uri: &v1alpha3.StringMatch{
								MatchType: &v1alpha3.StringMatch_Exact{Exact: "/login"},
							},
						},
					},
					Delegate: &v1alpha3.Delegate{
						Name: "productpage-vs",
					},
				},
				{
					Route: []*v1alpha3.HTTPRouteDestination{
						{
							Destination: &v1alpha3.Destination{
								Host: "example.org",
								Port: &v1alpha3.PortSelector{
									Number: 80,
								},
							},
						},
					},
				},
			},
		},
	}

	oneRoot := config.Config{
		Meta: config.Meta{
			Name:             "root-vs",
			Namespace:        "istio-system",
			GroupVersionKind: gvk.VirtualService,
		},
		Spec: &v1alpha3.VirtualService{
			Hosts:    []string{"*.org"},
			Gateways: []string{"istio-system/gateway"},
			Http: []*v1alpha3.HTTPRoute{
				{
					Route: []*v1alpha3.HTTPRouteDestination{
						{
							Destination: &v1alpha3.Destination{
								Host: "example.org",
								Port: &v1alpha3.PortSelector{
									Number: 80,
								},
							},
						},
					},
				},
			},
		},
	}

	createDelegateVs := func(name, namespace string, exportTo []string) config.Config {
		return config.Config{
			Meta: config.Meta{
				Name:             name,
				Namespace:        namespace,
				GroupVersionKind: gvk.VirtualService,
			},
			Spec: &v1alpha3.VirtualService{
				Hosts:    []string{},
				Gateways: []string{namespace + "/gateway"},
				ExportTo: exportTo,
				Http: []*v1alpha3.HTTPRoute{
					{
						Match: []*v1alpha3.HTTPMatchRequest{
							{
								Uri: &v1alpha3.StringMatch{
									MatchType: &v1alpha3.StringMatch_Prefix{Prefix: "/productpage/v1"},
								},
							},
						},
						Route: []*v1alpha3.HTTPRouteDestination{
							{
								Destination: &v1alpha3.Destination{
									Host: "productpage.org",
									Port: &v1alpha3.PortSelector{
										Number: 80,
									},
									Subset: "v1",
								},
							},
						},
					},
					{
						Match: []*v1alpha3.HTTPMatchRequest{
							{
								Uri: &v1alpha3.StringMatch{
									MatchType: &v1alpha3.StringMatch_Prefix{Prefix: "/productpage/v2"},
								},
							},
						},
						Route: []*v1alpha3.HTTPRouteDestination{
							{
								Destination: &v1alpha3.Destination{
									Host: "productpage.org",
									Port: &v1alpha3.PortSelector{
										Number: 80,
									},
									Subset: "v2",
								},
							},
						},
					},
					{
						Route: []*v1alpha3.HTTPRouteDestination{
							{
								Destination: &v1alpha3.Destination{
									Host: "productpage.org",
									Port: &v1alpha3.PortSelector{
										Number: 80,
									},
									Subset: "v3",
								},
							},
						},
					},
				},
			},
		}
	}

	delegateVs := createDelegateVs("productpage-vs", "default", []string{"istio-system"})
	delegateVsExportedToAll := createDelegateVs("productpage-vs", "default", []string{})

	delegateVsNotExported := config.Config{
		Meta: config.Meta{
			Name:             "productpage-vs",
			Namespace:        "default2",
			GroupVersionKind: gvk.VirtualService,
		},
		Spec: &v1alpha3.VirtualService{
			Hosts:    []string{},
			Gateways: []string{"default2/gateway"},
			ExportTo: []string{"."},
		},
	}

	mergedVs := config.Config{
		Meta: config.Meta{
			Name:             "root-vs",
			Namespace:        "istio-system",
			GroupVersionKind: gvk.VirtualService,
		},
		Spec: &v1alpha3.VirtualService{
			Hosts:    []string{"*.org"},
			Gateways: []string{"istio-system/gateway"},
			Http: []*v1alpha3.HTTPRoute{
				{
					Match: []*v1alpha3.HTTPMatchRequest{
						{
							Uri: &v1alpha3.StringMatch{
								MatchType: &v1alpha3.StringMatch_Prefix{Prefix: "/productpage/v1"},
							},
						},
					},
					Route: []*v1alpha3.HTTPRouteDestination{
						{
							Destination: &v1alpha3.Destination{
								Host: "productpage.org",
								Port: &v1alpha3.PortSelector{
									Number: 80,
								},
								Subset: "v1",
							},
						},
					},
				},
				{
					Match: []*v1alpha3.HTTPMatchRequest{
						{
							Uri: &v1alpha3.StringMatch{
								MatchType: &v1alpha3.StringMatch_Prefix{Prefix: "/productpage/v2"},
							},
						},
					},
					Route: []*v1alpha3.HTTPRouteDestination{
						{
							Destination: &v1alpha3.Destination{
								Host: "productpage.org",
								Port: &v1alpha3.PortSelector{
									Number: 80,
								},
								Subset: "v2",
							},
						},
					},
				},
				{
					Match: []*v1alpha3.HTTPMatchRequest{
						{
							Uri: &v1alpha3.StringMatch{
								MatchType: &v1alpha3.StringMatch_Prefix{Prefix: "/productpage"},
							},
						},
						{
							Uri: &v1alpha3.StringMatch{
								MatchType: &v1alpha3.StringMatch_Exact{Exact: "/login"},
							},
						},
					},
					Route: []*v1alpha3.HTTPRouteDestination{
						{
							Destination: &v1alpha3.Destination{
								Host: "productpage.org",
								Port: &v1alpha3.PortSelector{
									Number: 80,
								},
								Subset: "v3",
							},
						},
					},
				},
				{
					Route: []*v1alpha3.HTTPRouteDestination{
						{
							Destination: &v1alpha3.Destination{
								Host: "example.org",
								Port: &v1alpha3.PortSelector{
									Number: 80,
								},
							},
						},
					},
				},
			},
		},
	}

	mergedVsInDefault := mergedVs.DeepCopy()
	mergedVsInDefault.Name = "default-vs"
	mergedVsInDefault.Namespace = "default"
	spec := mergedVsInDefault.Spec.(*v1alpha3.VirtualService)
	spec.Gateways = []string{"default/gateway"}

	// invalid delegate, match condition conflicts with root
	delegateVs2 := config.Config{
		Meta: config.Meta{
			Name:             "productpage-vs",
			Namespace:        "default",
			GroupVersionKind: gvk.VirtualService,
		},
		Spec: &v1alpha3.VirtualService{
			Hosts:    []string{},
			Gateways: []string{"default/gateway"},
			Http: []*v1alpha3.HTTPRoute{
				{
					Match: []*v1alpha3.HTTPMatchRequest{
						{
							Uri: &v1alpha3.StringMatch{
								MatchType: &v1alpha3.StringMatch_Prefix{Prefix: "/productpage/v1"},
							},
						},
					},
					Route: []*v1alpha3.HTTPRouteDestination{
						{
							Destination: &v1alpha3.Destination{
								Host: "productpage.org",
								Port: &v1alpha3.PortSelector{
									Number: 80,
								},
								Subset: "v1",
							},
						},
					},
				},
				{
					Match: []*v1alpha3.HTTPMatchRequest{
						{
							Uri: &v1alpha3.StringMatch{
								MatchType: &v1alpha3.StringMatch_Prefix{Prefix: "/productpage/v2"},
							},
						},
					},
					Route: []*v1alpha3.HTTPRouteDestination{
						{
							Destination: &v1alpha3.Destination{
								Host: "productpage.org",
								Port: &v1alpha3.PortSelector{
									Number: 80,
								},
								Subset: "v2",
							},
						},
					},
				},
				{
					// mismatch, this route will be ignored
					Match: []*v1alpha3.HTTPMatchRequest{
						{
							Name: "mismatch",
							Uri: &v1alpha3.StringMatch{
								// conflicts with root's HTTPMatchRequest
								MatchType: &v1alpha3.StringMatch_Prefix{Prefix: "/mis-match/path"},
							},
						},
					},
					Route: []*v1alpha3.HTTPRouteDestination{
						{
							Destination: &v1alpha3.Destination{
								Host: "productpage.org",
								Port: &v1alpha3.PortSelector{
									Number: 80,
								},
								Subset: "v3",
							},
						},
					},
				},
			},
		},
	}

	mergedVs2 := config.Config{
		Meta: config.Meta{
			Name:             "root-vs",
			Namespace:        "istio-system",
			GroupVersionKind: gvk.VirtualService,
		},
		Spec: &v1alpha3.VirtualService{
			Hosts:    []string{"*.org"},
			Gateways: []string{"istio-system/gateway"},
			Http: []*v1alpha3.HTTPRoute{
				{
					Match: []*v1alpha3.HTTPMatchRequest{
						{
							Uri: &v1alpha3.StringMatch{
								MatchType: &v1alpha3.StringMatch_Prefix{Prefix: "/productpage/v1"},
							},
						},
					},
					Route: []*v1alpha3.HTTPRouteDestination{
						{
							Destination: &v1alpha3.Destination{
								Host: "productpage.org",
								Port: &v1alpha3.PortSelector{
									Number: 80,
								},
								Subset: "v1",
							},
						},
					},
				},
				{
					Match: []*v1alpha3.HTTPMatchRequest{
						{
							Uri: &v1alpha3.StringMatch{
								MatchType: &v1alpha3.StringMatch_Prefix{Prefix: "/productpage/v2"},
							},
						},
					},
					Route: []*v1alpha3.HTTPRouteDestination{
						{
							Destination: &v1alpha3.Destination{
								Host: "productpage.org",
								Port: &v1alpha3.PortSelector{
									Number: 80,
								},
								Subset: "v2",
							},
						},
					},
				},
				{
					Route: []*v1alpha3.HTTPRouteDestination{
						{
							Destination: &v1alpha3.Destination{
								Host: "example.org",
								Port: &v1alpha3.PortSelector{
									Number: 80,
								},
							},
						},
					},
				},
			},
		},
	}

	// multiple routes delegate to one single sub VS
	multiRoutes := config.Config{
		Meta: config.Meta{
			Name:             "root-vs",
			Namespace:        "istio-system",
			GroupVersionKind: gvk.VirtualService,
		},
		Spec: &v1alpha3.VirtualService{
			Hosts:    []string{"*.org"},
			Gateways: []string{"istio-system/gateway"},
			Http: []*v1alpha3.HTTPRoute{
				{
					Match: []*v1alpha3.HTTPMatchRequest{
						{
							Uri: &v1alpha3.StringMatch{
								MatchType: &v1alpha3.StringMatch_Prefix{Prefix: "/productpage"},
							},
						},
					},
					Delegate: &v1alpha3.Delegate{
						Name:      "productpage-vs",
						Namespace: "default",
					},
				},
				{
					Match: []*v1alpha3.HTTPMatchRequest{
						{
							Uri: &v1alpha3.StringMatch{
								MatchType: &v1alpha3.StringMatch_Prefix{Prefix: "/legacy/path"},
							},
						},
					},
					Rewrite: &v1alpha3.HTTPRewrite{
						Uri: "/productpage",
					},
					Delegate: &v1alpha3.Delegate{
						Name:      "productpage-vs",
						Namespace: "default",
					},
				},
			},
		},
	}

	singleDelegate := config.Config{
		Meta: config.Meta{
			Name:             "productpage-vs",
			Namespace:        "default",
			GroupVersionKind: gvk.VirtualService,
		},
		Spec: &v1alpha3.VirtualService{
			Hosts:    []string{},
			Gateways: []string{"default/gateway"},
			Http: []*v1alpha3.HTTPRoute{
				{
					Route: []*v1alpha3.HTTPRouteDestination{
						{
							Destination: &v1alpha3.Destination{
								Host: "productpage.org",
								Port: &v1alpha3.PortSelector{
									Number: 80,
								},
								Subset: "v1",
							},
						},
					},
				},
			},
		},
	}

	mergedVs3 := config.Config{
		Meta: config.Meta{
			Name:             "root-vs",
			Namespace:        "istio-system",
			GroupVersionKind: gvk.VirtualService,
		},
		Spec: &v1alpha3.VirtualService{
			Hosts:    []string{"*.org"},
			Gateways: []string{"istio-system/gateway"},
			Http: []*v1alpha3.HTTPRoute{
				{
					Match: []*v1alpha3.HTTPMatchRequest{
						{
							Uri: &v1alpha3.StringMatch{
								MatchType: &v1alpha3.StringMatch_Prefix{Prefix: "/productpage"},
							},
						},
					},
					Route: []*v1alpha3.HTTPRouteDestination{
						{
							Destination: &v1alpha3.Destination{
								Host: "productpage.org",
								Port: &v1alpha3.PortSelector{
									Number: 80,
								},
								Subset: "v1",
							},
						},
					},
				},
				{
					Match: []*v1alpha3.HTTPMatchRequest{
						{
							Uri: &v1alpha3.StringMatch{
								MatchType: &v1alpha3.StringMatch_Prefix{Prefix: "/legacy/path"},
							},
						},
					},
					Rewrite: &v1alpha3.HTTPRewrite{
						Uri: "/productpage",
					},
					Route: []*v1alpha3.HTTPRouteDestination{
						{
							Destination: &v1alpha3.Destination{
								Host: "productpage.org",
								Port: &v1alpha3.PortSelector{
									Number: 80,
								},
								Subset: "v1",
							},
						},
					},
				},
			},
		},
	}

	cases := []struct {
		name                    string
		virtualServices         []config.Config
		expectedVirtualServices []config.Config
		defaultExportTo         sets.Set[visibility.Instance]
	}{
		// {
		// 	name:                    "one independent vs",
		// 	virtualServices:         []runtime.Object{independentVs},
		// 	expectedVirtualServices: []*networkingclient.VirtualService{independentVs},
		// 	defaultExportTo:         sets.New(visibility.Public),
		// },
		{
			name:                    "one root vs",
			virtualServices:         []config.Config{rootVs},
			expectedVirtualServices: []config.Config{oneRoot},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "one delegate vs",
			virtualServices:         []config.Config{delegateVs},
			expectedVirtualServices: []config.Config{},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "root and delegate vs",
			virtualServices:         []config.Config{rootVs.DeepCopy(), delegateVs},
			expectedVirtualServices: []config.Config{mergedVs},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "root and conflicted delegate vs",
			virtualServices:         []config.Config{rootVs.DeepCopy(), delegateVs2},
			expectedVirtualServices: []config.Config{mergedVs2},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "multiple routes delegate to one",
			virtualServices:         []config.Config{multiRoutes.DeepCopy(), singleDelegate},
			expectedVirtualServices: []config.Config{mergedVs3},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "root not specify delegate namespace default public",
			virtualServices:         []config.Config{defaultVs.DeepCopy(), delegateVsExportedToAll},
			expectedVirtualServices: []config.Config{mergedVsInDefault},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "delegate not exported to root vs namespace default public",
			virtualServices:         []config.Config{rootVs, delegateVsNotExported},
			expectedVirtualServices: []config.Config{oneRoot},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "root not specify delegate namespace default private",
			virtualServices:         []config.Config{defaultVs.DeepCopy(), delegateVsExportedToAll},
			expectedVirtualServices: []config.Config{mergedVsInDefault},
			defaultExportTo:         sets.New(visibility.Private),
		},
		{
			name:                    "delegate not exported to root vs namespace default private",
			virtualServices:         []config.Config{rootVs, delegateVsNotExported},
			expectedVirtualServices: []config.Config{oneRoot},
			defaultExportTo:         sets.New(visibility.Private),
		},
	}

	dumpOnFailure(t, krt.GlobalDebugHandler)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller := setupController(t, tc.defaultExportTo, tc.virtualServices...)
			got := controller.List(gvk.VirtualService, "")
			assert.Equal(t, got, tc.expectedVirtualServices)
		})
	}

	t.Run("test merge order", func(t *testing.T) {
		root := rootVs.DeepCopy()
		delegate := delegateVs.DeepCopy()
		normal := independentVs.DeepCopy()

		// make sorting results predictable.
		t0 := metav1.Now()
		root.CreationTimestamp = t0.Add(1)
		delegate.CreationTimestamp = t0.Add(2)
		normal.CreationTimestamp = t0.Add(3)

		checkOrder := func(got []config.Config) {
			gotOrder := make([]string, 0, len(got))
			for _, c := range got {
				gotOrder = append(gotOrder, fmt.Sprintf("%s/%s", c.Namespace, c.Name))
			}
			wantOrder := []string{"istio-system/root-vs", "default/virtual-service"}
			assert.Equal(t, gotOrder, wantOrder)
		}

		vses := []config.Config{root, delegate, normal}
		controller1 := setupController(t, sets.New(visibility.Public), vses...)
		got := controller1.List(gvk.VirtualService, "")
		checkOrder(got)

		vses = []config.Config{normal, delegate, root}
		controller2 := setupController(t, sets.New(visibility.Public), vses...)
		got = controller2.List(gvk.VirtualService, "")
		checkOrder(got)
	})
}
