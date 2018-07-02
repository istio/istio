// Copyright 2018 Aspen Mesh Authors
//
// No part of this software may be reproduced or transmitted in any
// form or by any means, electronic or mechanical, for any purpose,
// without express written permission of F5 Networks, Inc.

package route_test

import (
	"testing"

	aspenmeshconfig "github.com/aspenmesh/aspenmesh-crd/pkg/apis/config/v1alpha1"
	envoyroute "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/gogo/protobuf/types"
	"github.com/onsi/gomega"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/route"
)

const (
	svcSuffix         = ".svc.cluster.local"
	testSvcPort       = 9090
	testSvc1Name      = "svc1"
	testSvc1Namespace = "foo"
	testSvc1Weight    = 45
	testSvc1Hostname  = testSvc1Name + "." + testSvc1Namespace + svcSuffix
	testSvc2Name      = "svc2"
	testSvc2Namespace = "bar"
	testSvc2Weight    = 55
	testSvc2Hostname  = testSvc2Name + "." + testSvc2Namespace + svcSuffix
	testSvc3Name      = "svc3"
	testSvc3Namespace = "baz"
	testSvc3Weight    = 40
	testSvc3Hostname  = testSvc3Name + "." + testSvc3Namespace + svcSuffix
	testSvc4Name      = "svc4"
	testSvc4Namespace = "bax"
	testSvc4Weight    = 65
	testSvc4Hostname  = testSvc4Name + "." + testSvc4Namespace + svcSuffix
)

func createTestWeightedCluster() []*envoyroute.WeightedCluster_ClusterWeight {
	weighted := []*envoyroute.WeightedCluster_ClusterWeight{
		&envoyroute.WeightedCluster_ClusterWeight{
			Name:   model.BuildSubsetKey("outbound", "", testSvc1Hostname, testSvcPort),
			Weight: &types.UInt32Value{Value: uint32(testSvc1Weight)},
		},
		&envoyroute.WeightedCluster_ClusterWeight{
			Name:   model.BuildSubsetKey("outbound", "", testSvc2Hostname, testSvcPort),
			Weight: &types.UInt32Value{Value: uint32(testSvc2Weight)},
		},
	}
	return weighted
}

func TestDefaultRouteWithNoExperiments(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	clusterName := model.BuildSubsetKey("outbound", "", testSvc1Hostname,
		testSvcPort)
	expRoutes := []envoyroute.Route{
		envoyroute.Route{
			Match: envoyroute.RouteMatch{
				PathSpecifier: &envoyroute.RouteMatch_Prefix{
					Prefix: "/"},
			},
			Decorator: &envoyroute.Decorator{
				Operation: "default-route",
			},
			Action: &envoyroute.Route_Route{
				Route: &envoyroute.RouteAction{
					ClusterSpecifier: &envoyroute.RouteAction_Cluster{Cluster: clusterName},
				},
			},
		},
	}
	out := route.BuildDefaultHTTPRoute(clusterName, nil)
	g.Expect(out).To(gomega.Equal(expRoutes))
}

func TestWeightedClusterRouteWithNoExperiment(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	in := envoyroute.Route{
		Match: envoyroute.RouteMatch{
			PathSpecifier: &envoyroute.RouteMatch_Prefix{
				Prefix: "/"},
		},
		Decorator: &envoyroute.Decorator{
			Operation: "default-route",
		},
		Action: &envoyroute.Route_Route{
			Route: &envoyroute.RouteAction{
				ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
					WeightedClusters: &envoyroute.WeightedCluster{
						Clusters: createTestWeightedCluster(),
					},
				},
			},
		},
	}
	out := route.AddExperimentRoutes(&in, nil)
	g.Expect(len(out)).To(gomega.Equal(1))
	g.Expect(out[0]).To(gomega.Equal(in))
}

// TestSingleClusterWithExperiments validates merging of experiment routes with
// single cluster routes defined by users i.e. like default routes with no
// weights defined.
func TestSingleClusterWithExperiments(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	clusterName := model.BuildSubsetKey("outbound", "", testSvc1Hostname,
		testSvcPort)
	in := envoyroute.Route{
		Match: envoyroute.RouteMatch{
			PathSpecifier: &envoyroute.RouteMatch_Prefix{
				Prefix: "/mypath"},
		},
		Decorator: &envoyroute.Decorator{
			Operation: "my-route",
		},
		Action: &envoyroute.Route_Route{
			Route: &envoyroute.RouteAction{
				ClusterSpecifier: &envoyroute.RouteAction_Cluster{Cluster: clusterName},
			},
		},
	}
	inExps := map[string][]*aspenmeshconfig.ExperimentSpec{
		testSvc1Hostname: []*aspenmeshconfig.ExperimentSpec{
			&aspenmeshconfig.ExperimentSpec{
				Id:    "pr-1",
				Token: "abcd1234efgh5678",
				Spec: &aspenmeshconfig.ServiceSpec{
					Services: []*aspenmeshconfig.ServiceSelector{
						&aspenmeshconfig.ServiceSelector{
							Original: &aspenmeshconfig.Service{
								Name:      testSvc1Name,
								Namespace: testSvc1Namespace,
							},
							Experiment: &aspenmeshconfig.Service{
								Name:      testSvc3Name,
								Namespace: testSvc3Namespace,
							},
							ProdTrafficLoad: float32(20),
						},
						&aspenmeshconfig.ServiceSelector{
							Original: &aspenmeshconfig.Service{
								Name:      testSvc2Name,
								Namespace: testSvc2Namespace,
							},
							Experiment: &aspenmeshconfig.Service{
								Name:      testSvc4Name,
								Namespace: testSvc4Namespace,
							},
							ProdTrafficLoad: float32(10),
						},
					},
				},
			},
			&aspenmeshconfig.ExperimentSpec{
				Id:    "pr-2",
				Token: "ijkl2468mnop1012",
				Spec: &aspenmeshconfig.ServiceSpec{
					Services: []*aspenmeshconfig.ServiceSelector{
						&aspenmeshconfig.ServiceSelector{
							Original: &aspenmeshconfig.Service{
								Name:      testSvc2Name,
								Namespace: testSvc2Namespace,
							},
							Experiment: &aspenmeshconfig.Service{
								Name:      testSvc4Name,
								Namespace: testSvc4Namespace,
							},
						},
						&aspenmeshconfig.ServiceSelector{
							Original: &aspenmeshconfig.Service{
								Name:      testSvc1Name,
								Namespace: testSvc1Namespace,
							},
							Experiment: &aspenmeshconfig.Service{
								Name:      testSvc4Name,
								Namespace: testSvc4Namespace,
							},
						},
					},
				},
			},
			&aspenmeshconfig.ExperimentSpec{
				Id:    "pr-3",
				Token: "12345678abcdefgh",
				Spec: &aspenmeshconfig.ServiceSpec{
					Services: []*aspenmeshconfig.ServiceSelector{
						&aspenmeshconfig.ServiceSelector{
							Original: &aspenmeshconfig.Service{
								Name:      testSvc1Name,
								Namespace: testSvc1Namespace,
							},
							Experiment: &aspenmeshconfig.Service{
								Name:      testSvc3Name,
								Namespace: testSvc3Namespace,
							},
							ProdTrafficLoad: float32(35),
						},
					},
				},
			},
		},
	}
	expRoutes := []envoyroute.Route{
		envoyroute.Route{
			Match: envoyroute.RouteMatch{
				PathSpecifier: &envoyroute.RouteMatch_Prefix{
					Prefix: "/mypath",
				},
				Headers: []*envoyroute.HeaderMatcher{
					&envoyroute.HeaderMatcher{
						Name:  "x-b3-traceid",
						Value: ".*abcd1234$",
						Regex: &types.BoolValue{Value: true},
					},
				},
			},
			Decorator: &envoyroute.Decorator{
				Operation: testSvc1Name + "-experiment-" + "pr-1" +
					"-" + testSvc3Name + "-traceid",
			},
			Action: &envoyroute.Route_Route{
				Route: &envoyroute.RouteAction{
					ClusterSpecifier: &envoyroute.RouteAction_Cluster{
						Cluster: model.BuildSubsetKey("outbound", "",
							testSvc3Hostname, testSvcPort)},
				},
			},
		},
		envoyroute.Route{
			Match: envoyroute.RouteMatch{
				PathSpecifier: &envoyroute.RouteMatch_Prefix{
					Prefix: "/mypath",
				},
				Headers: []*envoyroute.HeaderMatcher{
					&envoyroute.HeaderMatcher{
						Name:  "cookie",
						Value: "^(.*?;)?(dev-experiment=abcd1234efgh5678)(;.*)?$",
						Regex: &types.BoolValue{Value: true},
					},
				},
			},
			Decorator: &envoyroute.Decorator{
				Operation: testSvc1Name + "-experiment-" + "pr-1" +
					"-" + testSvc3Name + "-cookie",
			},
			Action: &envoyroute.Route_Route{
				Route: &envoyroute.RouteAction{
					ClusterSpecifier: &envoyroute.RouteAction_Cluster{
						Cluster: model.BuildSubsetKey("outbound", "",
							testSvc3Hostname, testSvcPort)},
				},
			},
		},
		envoyroute.Route{
			Match: envoyroute.RouteMatch{
				PathSpecifier: &envoyroute.RouteMatch_Prefix{
					Prefix: "/mypath",
				},
				Headers: []*envoyroute.HeaderMatcher{
					&envoyroute.HeaderMatcher{
						Name:  "x-b3-traceid",
						Value: ".*ijkl2468$",
						Regex: &types.BoolValue{Value: true},
					},
				},
			},
			Decorator: &envoyroute.Decorator{
				Operation: testSvc1Name + "-experiment-" + "pr-2" +
					"-" + testSvc4Name + "-traceid",
			},
			Action: &envoyroute.Route_Route{
				Route: &envoyroute.RouteAction{
					ClusterSpecifier: &envoyroute.RouteAction_Cluster{
						Cluster: model.BuildSubsetKey("outbound", "",
							testSvc4Hostname, testSvcPort)},
				},
			},
		},
		envoyroute.Route{
			Match: envoyroute.RouteMatch{
				PathSpecifier: &envoyroute.RouteMatch_Prefix{
					Prefix: "/mypath",
				},
				Headers: []*envoyroute.HeaderMatcher{
					&envoyroute.HeaderMatcher{
						Name:  "cookie",
						Value: "^(.*?;)?(dev-experiment=ijkl2468mnop1012)(;.*)?$",
						Regex: &types.BoolValue{Value: true},
					},
				},
			},
			Decorator: &envoyroute.Decorator{
				Operation: testSvc1Name + "-experiment-" + "pr-2" +
					"-" + testSvc4Name + "-cookie",
			},
			Action: &envoyroute.Route_Route{
				Route: &envoyroute.RouteAction{
					ClusterSpecifier: &envoyroute.RouteAction_Cluster{
						Cluster: model.BuildSubsetKey("outbound", "",
							testSvc4Hostname, testSvcPort)},
				},
			},
		},
		envoyroute.Route{
			Match: envoyroute.RouteMatch{
				PathSpecifier: &envoyroute.RouteMatch_Prefix{
					Prefix: "/mypath",
				},
				Headers: []*envoyroute.HeaderMatcher{
					&envoyroute.HeaderMatcher{
						Name:  "x-b3-traceid",
						Value: ".*12345678$",
						Regex: &types.BoolValue{Value: true},
					},
				},
			},
			Decorator: &envoyroute.Decorator{
				Operation: testSvc1Name + "-experiment-" + "pr-3" +
					"-" + testSvc3Name + "-traceid",
			},
			Action: &envoyroute.Route_Route{
				Route: &envoyroute.RouteAction{
					ClusterSpecifier: &envoyroute.RouteAction_Cluster{
						Cluster: model.BuildSubsetKey("outbound", "",
							testSvc3Hostname, testSvcPort)},
				},
			},
		},
		envoyroute.Route{
			Match: envoyroute.RouteMatch{
				PathSpecifier: &envoyroute.RouteMatch_Prefix{
					Prefix: "/mypath",
				},
				Headers: []*envoyroute.HeaderMatcher{
					&envoyroute.HeaderMatcher{
						Name:  "cookie",
						Value: "^(.*?;)?(dev-experiment=12345678abcdefgh)(;.*)?$",
						Regex: &types.BoolValue{Value: true},
					},
				},
			},
			Decorator: &envoyroute.Decorator{
				Operation: testSvc1Name + "-experiment-" + "pr-3" +
					"-" + testSvc3Name + "-cookie",
			},
			Action: &envoyroute.Route_Route{
				Route: &envoyroute.RouteAction{
					ClusterSpecifier: &envoyroute.RouteAction_Cluster{
						Cluster: model.BuildSubsetKey("outbound", "",
							testSvc3Hostname, testSvcPort)},
				},
			},
		},
		envoyroute.Route{
			Match: envoyroute.RouteMatch{
				PathSpecifier: &envoyroute.RouteMatch_Prefix{
					Prefix: "/mypath",
				},
			},
			Decorator: &envoyroute.Decorator{
				Operation: testSvc1Hostname + "-experiment-traffic-shifting",
			},
			Action: &envoyroute.Route_Route{
				Route: &envoyroute.RouteAction{
					ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
						WeightedClusters: &envoyroute.WeightedCluster{
							Clusters: []*envoyroute.WeightedCluster_ClusterWeight{
								&envoyroute.WeightedCluster_ClusterWeight{
									Name:   model.BuildSubsetKey("outbound", "", testSvc3Hostname, testSvcPort),
									Weight: &types.UInt32Value{Value: uint32(55)},
								},
								&envoyroute.WeightedCluster_ClusterWeight{
									Name:   model.BuildSubsetKey("outbound", "", testSvc1Hostname, testSvcPort),
									Weight: &types.UInt32Value{Value: uint32(45)},
								},
							},
						},
					},
				},
			},
		},
	}
	out := route.AddExperimentRoutes(&in, inExps)
	g.Expect(out).To(gomega.Equal(expRoutes))
}

// Tests experiments interaction with user defined weighted cluster virtual
// service routes
func TestWeightedClusterWithExperiments(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	// Note that percent of traffic to testSvc1Hostname is 80 which will get
	// shifted based on experiments. The percent of traffic to testSvc4Hostname
	// will remain the same at 20 as there are no experiments for that service.
	in := envoyroute.Route{
		Match: envoyroute.RouteMatch{
			PathSpecifier: &envoyroute.RouteMatch_Prefix{
				Prefix: "/mypath"},
		},
		Decorator: &envoyroute.Decorator{
			Operation: "my-route",
		},
		Action: &envoyroute.Route_Route{
			Route: &envoyroute.RouteAction{
				ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
					WeightedClusters: &envoyroute.WeightedCluster{
						Clusters: []*envoyroute.WeightedCluster_ClusterWeight{
							&envoyroute.WeightedCluster_ClusterWeight{
								Name:   model.BuildSubsetKey("outbound", "", testSvc4Hostname, testSvcPort),
								Weight: &types.UInt32Value{Value: uint32(20)},
							},
							&envoyroute.WeightedCluster_ClusterWeight{
								Name:   model.BuildSubsetKey("outbound", "", testSvc1Hostname, testSvcPort),
								Weight: &types.UInt32Value{Value: uint32(80)},
							},
						},
					},
				},
			},
		},
	}
	inExps := map[string][]*aspenmeshconfig.ExperimentSpec{
		testSvc1Hostname: []*aspenmeshconfig.ExperimentSpec{
			// Shifts 20 percent prod traffic destined for testSvc1 to testSvc3
			// i.e. 20 percent of 80 percent
			// The second service selector is not useful in this test but is kept to
			// validate that multiple service selectors in an experiment are handled
			// correctly
			&aspenmeshconfig.ExperimentSpec{
				Id:    "pr-1",
				Token: "abcd1234efgh5678",
				Spec: &aspenmeshconfig.ServiceSpec{
					Services: []*aspenmeshconfig.ServiceSelector{
						&aspenmeshconfig.ServiceSelector{
							Original: &aspenmeshconfig.Service{
								Name:      testSvc1Name,
								Namespace: testSvc1Namespace,
							},
							Experiment: &aspenmeshconfig.Service{
								Name:      testSvc3Name,
								Namespace: testSvc3Namespace,
							},
							ProdTrafficLoad: float32(20),
						},
						&aspenmeshconfig.ServiceSelector{
							Original: &aspenmeshconfig.Service{
								Name:      testSvc2Name,
								Namespace: testSvc2Namespace,
							},
							Experiment: &aspenmeshconfig.Service{
								Name:      testSvc4Name,
								Namespace: testSvc4Namespace,
							},
							ProdTrafficLoad: float32(10),
						},
					},
				},
			},
			// This case is interesting as it directs traffic from testSvc1 to
			// testSvc4 which means 100 percent of traffic from testSvc1 will not be
			// directed to testSv4 but in 2 weighted clusters of 20 and 80.
			&aspenmeshconfig.ExperimentSpec{
				Id:    "pr-2",
				Token: "ijkl2468mnop1012",
				Spec: &aspenmeshconfig.ServiceSpec{
					Services: []*aspenmeshconfig.ServiceSelector{
						&aspenmeshconfig.ServiceSelector{
							Original: &aspenmeshconfig.Service{
								Name:      testSvc2Name,
								Namespace: testSvc2Namespace,
							},
							Experiment: &aspenmeshconfig.Service{
								Name:      testSvc4Name,
								Namespace: testSvc4Namespace,
							},
						},
						&aspenmeshconfig.ServiceSelector{
							Original: &aspenmeshconfig.Service{
								Name:      testSvc1Name,
								Namespace: testSvc1Namespace,
							},
							Experiment: &aspenmeshconfig.Service{
								Name:      testSvc4Name,
								Namespace: testSvc4Namespace,
							},
						},
					},
				},
			},
			// Shifts 35 percent prod traffic destined for testSvc1 to testSvc3
			// i.e. 35 percent of 80 percent. In total 55 percent of traffic destined
			// for testSvc1 (80 percent) is directed to testSvc3 i.e. 44 percent to
			// testSvc3 and remaining 36 precent to testSvc1.
			&aspenmeshconfig.ExperimentSpec{
				Id:    "pr-3",
				Token: "12345678abcdefgh",
				Spec: &aspenmeshconfig.ServiceSpec{
					Services: []*aspenmeshconfig.ServiceSelector{
						&aspenmeshconfig.ServiceSelector{
							Original: &aspenmeshconfig.Service{
								Name:      testSvc1Name,
								Namespace: testSvc1Namespace,
							},
							Experiment: &aspenmeshconfig.Service{
								Name:      testSvc3Name,
								Namespace: testSvc3Namespace,
							},
							ProdTrafficLoad: float32(35),
						},
					},
				},
			},
		},
	}
	expRoutes := []envoyroute.Route{
		envoyroute.Route{
			Match: envoyroute.RouteMatch{
				PathSpecifier: &envoyroute.RouteMatch_Prefix{
					Prefix: "/mypath",
				},
				Headers: []*envoyroute.HeaderMatcher{
					&envoyroute.HeaderMatcher{
						Name:  "x-b3-traceid",
						Value: ".*abcd1234$",
						Regex: &types.BoolValue{Value: true},
					},
				},
			},
			Decorator: &envoyroute.Decorator{
				Operation: testSvc1Name + "-experiment-" + "pr-1" +
					"-" + testSvc3Name + "-traceid",
			},
			Action: &envoyroute.Route_Route{
				Route: &envoyroute.RouteAction{
					ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
						WeightedClusters: &envoyroute.WeightedCluster{
							Clusters: []*envoyroute.WeightedCluster_ClusterWeight{
								&envoyroute.WeightedCluster_ClusterWeight{
									Name:   model.BuildSubsetKey("outbound", "", testSvc4Hostname, testSvcPort),
									Weight: &types.UInt32Value{Value: uint32(20)},
								},
								&envoyroute.WeightedCluster_ClusterWeight{
									Name:   model.BuildSubsetKey("outbound", "", testSvc3Hostname, testSvcPort),
									Weight: &types.UInt32Value{Value: uint32(80)},
								},
							},
						},
					},
				},
			},
		},
		envoyroute.Route{
			Match: envoyroute.RouteMatch{
				PathSpecifier: &envoyroute.RouteMatch_Prefix{
					Prefix: "/mypath",
				},
				Headers: []*envoyroute.HeaderMatcher{
					&envoyroute.HeaderMatcher{
						Name:  "cookie",
						Value: "^(.*?;)?(dev-experiment=abcd1234efgh5678)(;.*)?$",
						Regex: &types.BoolValue{Value: true},
					},
				},
			},
			Decorator: &envoyroute.Decorator{
				Operation: testSvc1Name + "-experiment-" + "pr-1" +
					"-" + testSvc3Name + "-cookie",
			},
			Action: &envoyroute.Route_Route{
				Route: &envoyroute.RouteAction{
					ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
						WeightedClusters: &envoyroute.WeightedCluster{
							Clusters: []*envoyroute.WeightedCluster_ClusterWeight{
								&envoyroute.WeightedCluster_ClusterWeight{
									Name:   model.BuildSubsetKey("outbound", "", testSvc4Hostname, testSvcPort),
									Weight: &types.UInt32Value{Value: uint32(20)},
								},
								&envoyroute.WeightedCluster_ClusterWeight{
									Name:   model.BuildSubsetKey("outbound", "", testSvc3Hostname, testSvcPort),
									Weight: &types.UInt32Value{Value: uint32(80)},
								},
							},
						},
					},
				},
			},
		},
		envoyroute.Route{
			Match: envoyroute.RouteMatch{
				PathSpecifier: &envoyroute.RouteMatch_Prefix{
					Prefix: "/mypath",
				},
				Headers: []*envoyroute.HeaderMatcher{
					&envoyroute.HeaderMatcher{
						Name:  "x-b3-traceid",
						Value: ".*ijkl2468$",
						Regex: &types.BoolValue{Value: true},
					},
				},
			},
			Decorator: &envoyroute.Decorator{
				Operation: testSvc1Name + "-experiment-" + "pr-2" +
					"-" + testSvc4Name + "-traceid",
			},
			Action: &envoyroute.Route_Route{
				Route: &envoyroute.RouteAction{
					ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
						WeightedClusters: &envoyroute.WeightedCluster{
							Clusters: []*envoyroute.WeightedCluster_ClusterWeight{
								&envoyroute.WeightedCluster_ClusterWeight{
									Name:   model.BuildSubsetKey("outbound", "", testSvc4Hostname, testSvcPort),
									Weight: &types.UInt32Value{Value: uint32(20)},
								},
								&envoyroute.WeightedCluster_ClusterWeight{
									Name:   model.BuildSubsetKey("outbound", "", testSvc4Hostname, testSvcPort),
									Weight: &types.UInt32Value{Value: uint32(80)},
								},
							},
						},
					},
				},
			},
		},
		envoyroute.Route{
			Match: envoyroute.RouteMatch{
				PathSpecifier: &envoyroute.RouteMatch_Prefix{
					Prefix: "/mypath",
				},
				Headers: []*envoyroute.HeaderMatcher{
					&envoyroute.HeaderMatcher{
						Name:  "cookie",
						Value: "^(.*?;)?(dev-experiment=ijkl2468mnop1012)(;.*)?$",
						Regex: &types.BoolValue{Value: true},
					},
				},
			},
			Decorator: &envoyroute.Decorator{
				Operation: testSvc1Name + "-experiment-" + "pr-2" +
					"-" + testSvc4Name + "-cookie",
			},
			// Note that both the clusters name are same and sum up to 100
			Action: &envoyroute.Route_Route{
				Route: &envoyroute.RouteAction{
					ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
						WeightedClusters: &envoyroute.WeightedCluster{
							Clusters: []*envoyroute.WeightedCluster_ClusterWeight{
								&envoyroute.WeightedCluster_ClusterWeight{
									Name:   model.BuildSubsetKey("outbound", "", testSvc4Hostname, testSvcPort),
									Weight: &types.UInt32Value{Value: uint32(20)},
								},
								&envoyroute.WeightedCluster_ClusterWeight{
									Name:   model.BuildSubsetKey("outbound", "", testSvc4Hostname, testSvcPort),
									Weight: &types.UInt32Value{Value: uint32(80)},
								},
							},
						},
					},
				},
			},
		},
		envoyroute.Route{
			Match: envoyroute.RouteMatch{
				PathSpecifier: &envoyroute.RouteMatch_Prefix{
					Prefix: "/mypath",
				},
				Headers: []*envoyroute.HeaderMatcher{
					&envoyroute.HeaderMatcher{
						Name:  "x-b3-traceid",
						Value: ".*12345678$",
						Regex: &types.BoolValue{Value: true},
					},
				},
			},
			Decorator: &envoyroute.Decorator{
				Operation: testSvc1Name + "-experiment-" + "pr-3" +
					"-" + testSvc3Name + "-traceid",
			},
			Action: &envoyroute.Route_Route{
				Route: &envoyroute.RouteAction{
					ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
						WeightedClusters: &envoyroute.WeightedCluster{
							Clusters: []*envoyroute.WeightedCluster_ClusterWeight{
								&envoyroute.WeightedCluster_ClusterWeight{
									Name:   model.BuildSubsetKey("outbound", "", testSvc4Hostname, testSvcPort),
									Weight: &types.UInt32Value{Value: uint32(20)},
								},
								&envoyroute.WeightedCluster_ClusterWeight{
									Name:   model.BuildSubsetKey("outbound", "", testSvc3Hostname, testSvcPort),
									Weight: &types.UInt32Value{Value: uint32(80)},
								},
							},
						},
					},
				},
			},
		},
		envoyroute.Route{
			Match: envoyroute.RouteMatch{
				PathSpecifier: &envoyroute.RouteMatch_Prefix{
					Prefix: "/mypath",
				},
				Headers: []*envoyroute.HeaderMatcher{
					&envoyroute.HeaderMatcher{
						Name:  "cookie",
						Value: "^(.*?;)?(dev-experiment=12345678abcdefgh)(;.*)?$",
						Regex: &types.BoolValue{Value: true},
					},
				},
			},
			Decorator: &envoyroute.Decorator{
				Operation: testSvc1Name + "-experiment-" + "pr-3" +
					"-" + testSvc3Name + "-cookie",
			},
			Action: &envoyroute.Route_Route{
				Route: &envoyroute.RouteAction{
					ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
						WeightedClusters: &envoyroute.WeightedCluster{
							Clusters: []*envoyroute.WeightedCluster_ClusterWeight{
								&envoyroute.WeightedCluster_ClusterWeight{
									Name:   model.BuildSubsetKey("outbound", "", testSvc4Hostname, testSvcPort),
									Weight: &types.UInt32Value{Value: uint32(20)},
								},
								&envoyroute.WeightedCluster_ClusterWeight{
									Name:   model.BuildSubsetKey("outbound", "", testSvc3Hostname, testSvcPort),
									Weight: &types.UInt32Value{Value: uint32(80)},
								},
							},
						},
					},
				},
			},
		},
		envoyroute.Route{
			Match: envoyroute.RouteMatch{
				PathSpecifier: &envoyroute.RouteMatch_Prefix{
					Prefix: "/mypath",
				},
			},
			Decorator: &envoyroute.Decorator{
				Operation: "my-route-experiment-traffic-shifting",
			},
			// Note the split in percent traffic is calculated based on sum of all
			// experiments.
			Action: &envoyroute.Route_Route{
				Route: &envoyroute.RouteAction{
					ClusterSpecifier: &envoyroute.RouteAction_WeightedClusters{
						WeightedClusters: &envoyroute.WeightedCluster{
							Clusters: []*envoyroute.WeightedCluster_ClusterWeight{
								&envoyroute.WeightedCluster_ClusterWeight{
									Name:   model.BuildSubsetKey("outbound", "", testSvc4Hostname, testSvcPort),
									Weight: &types.UInt32Value{Value: uint32(20)},
								},
								&envoyroute.WeightedCluster_ClusterWeight{
									Name:   model.BuildSubsetKey("outbound", "", testSvc3Hostname, testSvcPort),
									Weight: &types.UInt32Value{Value: uint32(44)},
								},
								&envoyroute.WeightedCluster_ClusterWeight{
									Name:   model.BuildSubsetKey("outbound", "", testSvc1Hostname, testSvcPort),
									Weight: &types.UInt32Value{Value: uint32(36)},
								},
							},
						},
					},
				},
			},
		},
	}
	out := route.AddExperimentRoutes(&in, inExps)
	g.Expect(out).To(gomega.Equal(expRoutes))
}
