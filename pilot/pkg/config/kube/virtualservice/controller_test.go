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

package virtualservice

import (
	"fmt"
	"testing"

	"gopkg.in/yaml.v2"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/gateway-api/pkg/consts"
)

func setupController(t *testing.T, defaultExportTo sets.Set[visibility.Instance], objs ...runtime.Object) *Controller {
	kc := kube.NewFakeClient(objs...)
	setupClientCRDs(t, kc)
	stop := test.NewStop(t)
	meshHolder := meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{
		DefaultVirtualServiceExportTo: slices.Map(defaultExportTo.UnsortedList(), func(i visibility.Instance) string {
			return string(i)
		}),
	})
	controller := NewController(
		kc,
		controller.Options{KrtDebugger: krt.GlobalDebugHandler},
		meshHolder,
		nil)
	kc.RunAndWait(stop)
	go controller.Run(stop)
	kube.WaitForCacheSync("test", stop, controller.HasSynced)

	return controller
}

func setupClientCRDs(t *testing.T, kc kube.CLIClient) {
	for _, crd := range []schema.GroupVersionResource{
		gvr.VirtualService,
	} {
		clienttest.MakeCRDWithAnnotations(t, kc, crd, map[string]string{
			consts.BundleVersionAnnotation: "v1.1.0",
		})
	}
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

func TestMergeVirtualServices(t *testing.T) {
	independentVs := &networkingclient.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "virtual-service",
			Namespace: "default",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.VirtualService.Kind,
			APIVersion: gvk.VirtualService.GroupVersion(),
		},
		Spec: v1alpha3.VirtualService{
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

	rootVs := &networkingclient.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "root-vs",
			Namespace: "istio-system",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.VirtualService.Kind,
			APIVersion: gvk.VirtualService.GroupVersion(),
		},
		Spec: v1alpha3.VirtualService{
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

	defaultVs := &networkingclient.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default-vs",
			Namespace: "default",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.VirtualService.Kind,
			APIVersion: gvk.VirtualService.GroupVersion(),
		},
		Spec: v1alpha3.VirtualService{
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

	oneRoot := &networkingclient.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "root-vs",
			Namespace: "istio-system",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.VirtualService.Kind,
			APIVersion: gvk.VirtualService.GroupVersion(),
		},
		Spec: v1alpha3.VirtualService{
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

	createDelegateVs := func(name, namespace string, exportTo []string) *networkingclient.VirtualService {
		return &networkingclient.VirtualService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       gvk.VirtualService.Kind,
				APIVersion: gvk.VirtualService.GroupVersion(),
			},
			Spec: v1alpha3.VirtualService{
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

	delegateVsNotExported := &networkingclient.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "productpage-vs",
			Namespace: "default2",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.VirtualService.Kind,
			APIVersion: gvk.VirtualService.GroupVersion(),
		},
		Spec: v1alpha3.VirtualService{
			Hosts:    []string{},
			Gateways: []string{"default2/gateway"},
			ExportTo: []string{"."},
		},
	}

	mergedVs := &networkingclient.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "root-vs",
			Namespace: "istio-system",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.VirtualService.Kind,
			APIVersion: gvk.VirtualService.GroupVersion(),
		},
		Spec: v1alpha3.VirtualService{
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
	mergedVsInDefault.Spec.Gateways = []string{"default/gateway"}

	// invalid delegate, match condition conflicts with root
	delegateVs2 := &networkingclient.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "productpage-vs",
			Namespace: "default",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.VirtualService.Kind,
			APIVersion: gvk.VirtualService.GroupVersion(),
		},
		Spec: v1alpha3.VirtualService{
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

	mergedVs2 := &networkingclient.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "root-vs",
			Namespace: "istio-system",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.VirtualService.Kind,
			APIVersion: gvk.VirtualService.GroupVersion(),
		},
		Spec: v1alpha3.VirtualService{
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
	multiRoutes := &networkingclient.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "root-vs",
			Namespace: "istio-system",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.VirtualService.Kind,
			APIVersion: gvk.VirtualService.GroupVersion(),
		},
		Spec: v1alpha3.VirtualService{
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

	singleDelegate := &networkingclient.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "productpage-vs",
			Namespace: "default",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.VirtualService.Kind,
			APIVersion: gvk.VirtualService.GroupVersion(),
		},
		Spec: v1alpha3.VirtualService{
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

	mergedVs3 := &networkingclient.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "root-vs",
			Namespace: "istio-system",
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.VirtualService.Kind,
			APIVersion: gvk.VirtualService.GroupVersion(),
		},
		Spec: v1alpha3.VirtualService{
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
		virtualServices         []runtime.Object
		expectedVirtualServices []*networkingclient.VirtualService
		defaultExportTo         sets.Set[visibility.Instance]
	}{
		{
			name:                    "one independent vs",
			virtualServices:         []runtime.Object{independentVs},
			expectedVirtualServices: []*networkingclient.VirtualService{independentVs},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "one root vs",
			virtualServices:         []runtime.Object{rootVs},
			expectedVirtualServices: []*networkingclient.VirtualService{oneRoot},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "one delegate vs",
			virtualServices:         []runtime.Object{delegateVs},
			expectedVirtualServices: []*networkingclient.VirtualService{},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "root and delegate vs",
			virtualServices:         []runtime.Object{rootVs.DeepCopy(), delegateVs},
			expectedVirtualServices: []*networkingclient.VirtualService{mergedVs},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "root and conflicted delegate vs",
			virtualServices:         []runtime.Object{rootVs.DeepCopy(), delegateVs2},
			expectedVirtualServices: []*networkingclient.VirtualService{mergedVs2},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "multiple routes delegate to one",
			virtualServices:         []runtime.Object{multiRoutes.DeepCopy(), singleDelegate},
			expectedVirtualServices: []*networkingclient.VirtualService{mergedVs3},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "root not specify delegate namespace default public",
			virtualServices:         []runtime.Object{defaultVs.DeepCopy(), delegateVsExportedToAll},
			expectedVirtualServices: []*networkingclient.VirtualService{mergedVsInDefault},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "delegate not exported to root vs namespace default public",
			virtualServices:         []runtime.Object{rootVs, delegateVsNotExported},
			expectedVirtualServices: []*networkingclient.VirtualService{oneRoot},
			defaultExportTo:         sets.New(visibility.Public),
		},
		{
			name:                    "root not specify delegate namespace default private",
			virtualServices:         []runtime.Object{defaultVs.DeepCopy(), delegateVsExportedToAll},
			expectedVirtualServices: []*networkingclient.VirtualService{mergedVsInDefault},
			defaultExportTo:         sets.New(visibility.Private),
		},
		{
			name:                    "delegate not exported to root vs namespace default private",
			virtualServices:         []runtime.Object{rootVs, delegateVsNotExported},
			expectedVirtualServices: []*networkingclient.VirtualService{oneRoot},
			defaultExportTo:         sets.New(visibility.Private),
		},
	}

	dumpOnFailure(t, krt.GlobalDebugHandler)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller := setupController(t, tc.defaultExportTo, tc.virtualServices...)
			got := controller.List(gvk.VirtualService, "")
			expectedConfigs := make([]config.Config, 0, len(tc.expectedVirtualServices))
			for _, vs := range tc.expectedVirtualServices {
				expectedConfigs = append(expectedConfigs, vsToConfig(vs))
			}

			assert.Equal(t, got, expectedConfigs)
		})
	}

	t.Run("test merge order", func(t *testing.T) {
		root := rootVs.DeepCopy()
		delegate := delegateVs.DeepCopy()
		normal := independentVs.DeepCopy()

		// make sorting results predictable.
		t0 := metav1.Now()
		root.CreationTimestamp = metav1.NewTime(t0.Add(1))
		delegate.CreationTimestamp = metav1.NewTime(t0.Add(2))
		normal.CreationTimestamp = metav1.NewTime(t0.Add(3))

		checkOrder := func(got []config.Config) {
			gotOrder := make([]string, 0, len(got))
			for _, c := range got {
				gotOrder = append(gotOrder, fmt.Sprintf("%s/%s", c.Namespace, c.Name))
			}
			wantOrder := []string{"istio-system/root-vs", "default/virtual-service"}
			assert.Equal(t, gotOrder, wantOrder)
		}

		vses := []runtime.Object{root, delegate, normal}
		controller1 := setupController(t, sets.New(visibility.Public), vses...)
		got := controller1.List(gvk.VirtualService, "")
		checkOrder(got)

		vses = []runtime.Object{normal, delegate, root}
		controller2 := setupController(t, sets.New(visibility.Public), vses...)
		got = controller2.List(gvk.VirtualService, "")
		checkOrder(got)
	})
}
