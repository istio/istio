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

package v2_test

import (
	"testing"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v2"
)

func subsetSelector(svc *model.Service, subset string) map[string]string {
	return map[string]string{"version": "v1"}
}

func TestRDS(t *testing.T) {
	services := map[string]*model.Service{
		"a.default.svc.cluster.local": &model.Service{
			Hostname: "a.default.svc.cluster.local",
			Ports: model.PortList{
				&model.Port{
					Name:     "http",
					Protocol: model.ProtocolHTTP,
				},
			},
		},
		"b.default.svc.cluster.local": &model.Service{
			Hostname: "b.default.svc.cluster.local",
			Ports: model.PortList{
				&model.Port{
					Name:     "http",
					Protocol: model.ProtocolHTTP,
				},
			},
		},
	}
	configs := []model.Config{
		model.Config{
			ConfigMeta: model.ConfigMeta{
				Name:      "rule1",
				Namespace: "default",
			},
			Spec: &v1alpha3.VirtualService{
				Hosts: []string{"b", "random.com"},
				Http: []*v1alpha3.HTTPRoute{
					&v1alpha3.HTTPRoute{
						Match: []*v1alpha3.HTTPMatchRequest{{
							Uri: &v1alpha3.StringMatch{
								MatchType: &v1alpha3.StringMatch_Prefix{
									Prefix: "/list",
								},
							},
							Headers: map[string]*v1alpha3.StringMatch{
								"test": &v1alpha3.StringMatch{MatchType: &v1alpha3.StringMatch_Exact{Exact: "id"}},
							},
							Method:    &v1alpha3.StringMatch{MatchType: &v1alpha3.StringMatch_Exact{Exact: "POST"}},
							Authority: &v1alpha3.StringMatch{MatchType: &v1alpha3.StringMatch_Prefix{Prefix: "b"}},
							Scheme:    &v1alpha3.StringMatch{MatchType: &v1alpha3.StringMatch_Regex{Regex: "h.*"}},
						}},
						Route: []*v1alpha3.DestinationWeight{
							&v1alpha3.DestinationWeight{
								Destination: &v1alpha3.Destination{Name: "a", Subset: "test"}, Weight: 50,
							},
							&v1alpha3.DestinationWeight{
								Destination: &v1alpha3.Destination{Name: "b"}, Weight: 50,
							},
						},
					},
				},
			},
		},
		model.Config{
			ConfigMeta: model.ConfigMeta{
				Name:      "rule2",
				Namespace: "default",
			},
			Spec: &v1alpha3.VirtualService{
				Hosts: []string{"google.com"},
				Http: []*v1alpha3.HTTPRoute{
					&v1alpha3.HTTPRoute{
						Redirect: &v1alpha3.HTTPRedirect{
							Authority: "us.google.com",
						},
					},
				},
			},
		},
	}
	out := v2.TranslateVirtualHosts(
		configs,
		services,
		subsetSelector,
		"svc.cluster.local")

	t.Logf("%#v", out)
}
