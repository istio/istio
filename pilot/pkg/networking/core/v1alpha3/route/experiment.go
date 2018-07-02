// Copyright 2018 Aspen Mesh Authors
//
// No part of this software may be reproduced or transmitted in any
// form or by any means, electronic or mechanical, for any purpose,
// without express written permission of F5 Networks, Inc.

package route

import (
	"sort"

	aspenmeshconfig "github.com/aspenmesh/aspenmesh-crd/pkg/apis/config/v1alpha1"
	envoyroute "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/gogo/protobuf/types"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
)

const (
	xB3TraceIDHeader      = "x-b3-traceid"
	cookieHeader          = "cookie"
	devExperimentCookie   = "dev-experiment"
	trafficShiftingSuffix = "-experiment-traffic-shifting"
	traceIDSuffix         = "-traceid"
	cookieSuffix          = "-cookie"
	numHeaderMatchRoutes  = 2
)

// { key: <exp-svc-name.exp-svc-namespace> value: prodTrafficLoad }
type weightedClusterProdWeight map[string]uint32

func convertTokenToTraceIDMatch(token string) (string, *networking.StringMatch) {
	truncatedToken := token
	if len(truncatedToken) > 8 {
		truncatedToken = truncatedToken[0:8]
	}
	ex := &networking.StringMatch_Regex{Regex: ".*" + truncatedToken + "$"}
	sm := &networking.StringMatch{MatchType: ex}
	return xB3TraceIDHeader, sm
}

func convertTokenToCookieMatch(token string) (string, *networking.StringMatch) {
	ex := &networking.StringMatch_Regex{
		Regex: "^(.*?;)?(" + devExperimentCookie + "=" + token + ")(;.*)?$"}
	sm := &networking.StringMatch{MatchType: ex}
	return cookieHeader, sm
}

func createClusterName(hostname string, originalClusterName string) string {
	direction, subsetName, _, port := model.ParseSubsetKey(originalClusterName)
	return model.BuildSubsetKey(direction, subsetName, model.Hostname(hostname), port)
}

func createExperimentClusterName(exp *aspenmeshconfig.ExperimentSpec,
	selectorIdx int, originalClusterName string) string {
	ss := exp.GetSpec().GetServices()
	if len(ss) > selectorIdx {
		es := ss[selectorIdx].GetExperiment()
		return createClusterName(model.ConvertExpServiceShortNameToFqdn(es.GetName()+"."+es.GetNamespace()),
			originalClusterName)
	}
	return ""
}

func createExperimentOperation(exp *aspenmeshconfig.ExperimentSpec,
	selectorIdx int, isRouteCookie bool) string {
	suffix := traceIDSuffix
	if isRouteCookie {
		suffix = cookieSuffix
	}
	ss := exp.GetSpec().GetServices()
	if len(ss) > selectorIdx {
		os := ss[selectorIdx].GetOriginal()
		es := ss[selectorIdx].GetExperiment()
		return os.GetName() + "-experiment-" + exp.GetId() + "-" +
			es.GetName() + suffix
	}
	return ""
}

func createProdShiftingOperation(cluster, currentOperation string) string {
	if cluster != "" {
		_, _, hostname, _ := model.ParseSubsetKey(cluster)
		return string(hostname) + trafficShiftingSuffix
	}
	return currentOperation + trafficShiftingSuffix
}

func createHeaderMatchRoutes(exp *aspenmeshconfig.ExperimentSpec,
	selectorIdx int, route *envoyroute.Route,
	originalClusterName string, clusterIdx int) []envoyroute.Route {
	out := []envoyroute.Route{}
	for i := 0; i < numHeaderMatchRoutes; i++ {
		newRoute := *route
		newRoute.Match.Headers = []*envoyroute.HeaderMatcher{}
		newRoute.Match.Headers = append(newRoute.Match.Headers, route.GetMatch().Headers...)
		var name string
		var sm *networking.StringMatch
		if i == 0 {
			name, sm = convertTokenToTraceIDMatch(exp.GetToken())
		} else {
			name, sm = convertTokenToCookieMatch(exp.GetToken())
		}
		hm := translateHeaderMatch(name, sm)
		newRoute.Match.Headers = append(newRoute.Match.Headers, &hm)
		// guarantee ordering of headers
		sort.Slice(newRoute.Match.Headers, func(i, j int) bool {
			if newRoute.Match.Headers[i].Name == newRoute.Match.Headers[j].Name {
				return newRoute.Match.Headers[i].Value < newRoute.Match.Headers[j].Value
			}
			return newRoute.Match.Headers[i].Name < newRoute.Match.Headers[j].Name
		})
		newRoute.Decorator = &envoyroute.Decorator{}
		if i == 0 {
			newRoute.Decorator.Operation = createExperimentOperation(exp, selectorIdx, false)
		} else {
			newRoute.Decorator.Operation = createExperimentOperation(exp, selectorIdx, true)
		}
		// Make a shallow copy of input route action and overwrite the cluster(s)
		newRouteAction := *route.GetRoute()
		// single cluster route
		if newRouteAction.GetCluster() != "" {
			newRouteAction.ClusterSpecifier = &envoyroute.RouteAction_Cluster{
				Cluster: createExperimentClusterName(exp, selectorIdx,
					originalClusterName),
			}
		} else if newRouteAction.GetWeightedClusters() != nil {
			// weighted cluster route
			wc := []*envoyroute.WeightedCluster_ClusterWeight{}
			wc = append(wc, newRouteAction.GetWeightedClusters().GetClusters()...)
			// Overwrite the cluster belonging to experiment
			if clusterIdx < len(wc) {
				wc[clusterIdx] = &envoyroute.WeightedCluster_ClusterWeight{
					Name:   createExperimentClusterName(exp, selectorIdx, originalClusterName),
					Weight: wc[clusterIdx].GetWeight(),
				}
			}
			newRouteAction.ClusterSpecifier = &envoyroute.RouteAction_WeightedClusters{
				WeightedClusters: &envoyroute.WeightedCluster{
					Clusters: wc,
				},
			}
		}
		newRoute.Action = &envoyroute.Route_Route{&newRouteAction}
		out = append(out, newRoute)
	}
	return out
}

func computeAdjustedWeights(prodWeightMap map[string]uint32) uint32 {
	var sum uint32
	for _, w := range prodWeightMap {
		sum += w
	}
	var rw uint32
	if sum <= 100 {
		rw = 100 - sum
	} else {
		var newSum uint32
		for k, w := range prodWeightMap {
			prodWeightMap[k] = uint32((w * 100) / sum)
			newSum = newSum + prodWeightMap[k]
		}
		rw = 100 - newSum
	}
	return rw
}

func computeBasePercentWeight(w uint32, baseWeight uint32) uint32 {
	basePercentWeight := uint32(w * baseWeight / uint32(100))
	return basePercentWeight
}

func createWeightedCluster(clusterWeightMap map[string]weightedClusterProdWeight,
	clusterName string, baseWeight uint32) []*envoyroute.WeightedCluster_ClusterWeight {
	weighted := []*envoyroute.WeightedCluster_ClusterWeight{}
	_, _, hostname, _ := model.ParseSubsetKey(clusterName)
	if prodWeightMap, ok := clusterWeightMap[string(hostname)]; ok && len(prodWeightMap) > 0 {
		rw := computeAdjustedWeights(prodWeightMap)
		var sumWeights uint32

		for svc, w := range prodWeightMap {
			bpw := computeBasePercentWeight(w, baseWeight)
			if bpw > 0 {
				sumWeights += bpw
				weighted = append(weighted, &envoyroute.WeightedCluster_ClusterWeight{
					Name:   createClusterName(svc, clusterName),
					Weight: &types.UInt32Value{Value: bpw},
				})
			}
		}

		if rw > 0 {
			bpw := computeBasePercentWeight(rw, baseWeight)
			if bpw > 0 {
				sumWeights += bpw
				weighted = append(weighted, &envoyroute.WeightedCluster_ClusterWeight{
					Name:   clusterName,
					Weight: &types.UInt32Value{Value: bpw},
				})
			}
		}

		if sumWeights != baseWeight {
			weighted[len(weighted)-1].Weight = &types.UInt32Value{
				Value: uint32(weighted[len(weighted)-1].Weight.Value +
					uint32(baseWeight-sumWeights))}
		}
	}
	return weighted
}

func createProdShiftingRoute(route *envoyroute.Route, clusterName string,
	clusterWeightMap map[string]weightedClusterProdWeight) *envoyroute.Route {
	weighted := []*envoyroute.WeightedCluster_ClusterWeight{}
	if len(clusterWeightMap) > 0 {
		out := *route
		if route.GetRoute() != nil && route.GetRoute().GetCluster() != "" {
			wc := createWeightedCluster(clusterWeightMap, route.GetRoute().GetCluster(), 100)
			weighted = append(weighted, wc...)
		} else if route.GetRoute() != nil && route.GetRoute().GetWeightedClusters() != nil {
			for _, cluster := range route.GetRoute().GetWeightedClusters().GetClusters() {
				wc := createWeightedCluster(clusterWeightMap, cluster.GetName(),
					cluster.GetWeight().GetValue())
				if len(wc) > 0 {
					weighted = append(weighted, wc...)
				} else {
					weighted = append(weighted, cluster)
				}
			}
		}

		// Make a shallow copy of input route action and overwrite the cluster
		if len(weighted) > 0 {
			inOperation := ""
			if route.GetDecorator() != nil {
				inOperation = route.GetDecorator().GetOperation()
			}
			out.Decorator = &envoyroute.Decorator{
				Operation: createProdShiftingOperation(clusterName, inOperation),
			}
			if len(weighted) == 1 {
				newRouteAction := *route.GetRoute()
				newRouteAction.ClusterSpecifier = &envoyroute.RouteAction_Cluster{
					Cluster: weighted[0].Name,
				}
				out.Action = &envoyroute.Route_Route{&newRouteAction}
			} else {
				newRouteAction := *route.GetRoute()
				newRouteAction.ClusterSpecifier = &envoyroute.RouteAction_WeightedClusters{
					WeightedClusters: &envoyroute.WeightedCluster{
						Clusters: weighted,
					},
				}
				out.Action = &envoyroute.Route_Route{&newRouteAction}
			}
		}
		return &out
	}
	return route
}

func createRoutes(route *envoyroute.Route,
	clusterWeightMap map[string]weightedClusterProdWeight,
	amExperiments map[string][]*aspenmeshconfig.ExperimentSpec,
	clusterName string, clusterIdx int) []envoyroute.Route {
	out := []envoyroute.Route{}
	_, _, hostname, _ := model.ParseSubsetKey(clusterName)
	if exps, ok := amExperiments[string(hostname)]; ok {
		clusterWeightMap[string(hostname)] = weightedClusterProdWeight{}
		prodWeightMap := clusterWeightMap[string(hostname)]
		for _, exp := range exps {
			for idx, ss := range exp.GetSpec().GetServices() {
				os := ss.GetOriginal()
				osFqdn := model.ConvertExpServiceToFqdn(os)
				if osFqdn == string(hostname) {
					if ss.GetProdTrafficLoad() > 0 {
						es := ss.GetExperiment()
						key := model.ConvertExpServiceToFqdn(es)
						if _, ok := prodWeightMap[key]; !ok {
							prodWeightMap[key] = uint32(ss.GetProdTrafficLoad())
						} else {
							prodWeightMap[key] += uint32(ss.GetProdTrafficLoad())
						}
					}
					out = append(out, createHeaderMatchRoutes(exp, idx, route, clusterName,
						clusterIdx)...)
					// Original service is only allowed once in an experiment
					break
				}
			}
		}
	}
	return out
}

func createSingleClusterRoutes(route *envoyroute.Route,
	amExperiments map[string][]*aspenmeshconfig.ExperimentSpec,
	cluster string) []envoyroute.Route {
	out := []envoyroute.Route{}
	if cluster == "" || cluster == util.BlackHoleCluster {
		out = append(out, *route)
		return out
	}
	clusterWeightMap := map[string]weightedClusterProdWeight{}
	out = append(out, createRoutes(route,
		clusterWeightMap, amExperiments, cluster, -1)...)
	out = append(out, *createProdShiftingRoute(route, cluster, clusterWeightMap))
	return out
}

func createWeightedClusterRoutes(route *envoyroute.Route,
	amExperiments map[string][]*aspenmeshconfig.ExperimentSpec,
	wc *envoyroute.WeightedCluster) []envoyroute.Route {
	out := []envoyroute.Route{}
	// { key: <cluster-hostname> }
	clusterWeightMap := map[string]weightedClusterProdWeight{}
	for clusterIdx, cluster := range wc.GetClusters() {
		name := cluster.GetName()
		if name == "" || name == util.BlackHoleCluster {
			continue
		}
		out = append(out, createRoutes(route,
			clusterWeightMap, amExperiments, name, clusterIdx)...)
	}
	out = append(out, *createProdShiftingRoute(route, "", clusterWeightMap))
	return out
}

// AddExperimentRoutes adds routes if applicable based on experiments
func AddExperimentRoutes(route *envoyroute.Route,
	amExperiments map[string][]*aspenmeshconfig.ExperimentSpec) []envoyroute.Route {
	out := []envoyroute.Route{}
	ra := route.GetRoute()
	if ra != nil {
		if cluster := ra.GetCluster(); cluster != "" {
			out = append(out, createSingleClusterRoutes(route, amExperiments, cluster)...)
		} else if wc := ra.GetWeightedClusters(); wc != nil {
			out = append(out, createWeightedClusterRoutes(route, amExperiments, wc)...)
		} else {
			out = append(out, *route)
		}
	} else {
		out = append(out, *route)
	}
	return out
}
