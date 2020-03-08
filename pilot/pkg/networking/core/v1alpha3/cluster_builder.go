// Copyright 2020 Istio Authors. All Rights Reserved.
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

package v1alpha3

import (
	apiv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config/labels"
)

var (
	defaultDestinationRule = networking.DestinationRule{}
)

// applyDestinationRule applies the destination rule if it exists for the Service. It returns the subset clusters if any created as it
// applies the destination rule.
func applyDestinationRule(cluster *apiv2.Cluster, clusterMode ClusterMode, service *model.Service, port *model.Port, proxy *model.Proxy,
	proxyNetworkView map[string]bool, push *model.PushContext) []*apiv2.Cluster {
	destRule := push.DestinationRule(proxy, service)
	destinationRule := castDestinationRuleOrDefault(destRule)

	opts := buildClusterOpts{
		push:        push,
		cluster:     cluster,
		policy:      destinationRule.TrafficPolicy,
		port:        port,
		clusterMode: clusterMode,
		direction:   model.TrafficDirectionOutbound,
		proxy:       proxy,
	}

	if clusterMode == DefaultClusterMode {
		opts.serviceAccounts = push.ServiceAccounts[service.Hostname][port.Port]
		opts.istioMtlsSni = model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, "", service.Hostname, port.Port)
		opts.simpleTLSSni = string(service.Hostname)
		opts.meshExternal = service.MeshExternal
		opts.serviceMTLSMode = push.BestEffortInferServiceMTLSMode(service, port)
	}

	// Apply traffic policy for the main default cluster.
	applyTrafficPolicy(opts, proxy)
	var clusterMetadata *core.Metadata
	if destRule != nil {
		clusterMetadata = util.BuildConfigInfoMetadata(destRule.ConfigMeta)
		cluster.Metadata = clusterMetadata
	}
	subsetClusters := make([]*apiv2.Cluster, 0)
	for _, subset := range destinationRule.Subsets {
		var subsetClusterName string
		var defaultSni string
		if clusterMode == DefaultClusterMode {
			subsetClusterName = model.BuildSubsetKey(model.TrafficDirectionOutbound, subset.Name, service.Hostname, port.Port)
			defaultSni = model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, subset.Name, service.Hostname, port.Port)

		} else {
			subsetClusterName = model.BuildDNSSrvSubsetKey(model.TrafficDirectionOutbound, subset.Name, service.Hostname, port.Port)
		}
		// clusters with discovery type STATIC, STRICT_DNS rely on cluster.hosts field
		// ServiceEntry's need to filter hosts based on subset.labels in order to perform weighted routing
		var lbEndpoints []*endpoint.LocalityLbEndpoints
		if cluster.GetType() != apiv2.Cluster_EDS && len(subset.Labels) != 0 {
			lbEndpoints = buildLocalityLbEndpoints(push, proxyNetworkView, service, port.Port, []labels.Instance{subset.Labels})
		}

		subsetCluster := buildDefaultCluster(push, subsetClusterName, cluster.GetType(), lbEndpoints,
			model.TrafficDirectionOutbound, proxy, nil, service.MeshExternal)

		if subsetCluster == nil {
			continue
		}
		if len(push.Mesh.OutboundClusterStatName) != 0 {
			subsetCluster.AltStatName = util.BuildStatPrefix(push.Mesh.OutboundClusterStatName, string(service.Hostname), subset.Name, port, service.Attributes)
		}
		setUpstreamProtocol(proxy, subsetCluster, port, model.TrafficDirectionOutbound)

		// Apply traffic policy for subset cluster with the destination rule traffice policy.
		opts.cluster = subsetCluster
		opts.policy = destinationRule.TrafficPolicy
		opts.istioMtlsSni = defaultSni
		applyTrafficPolicy(opts, proxy)

		// If subset has a traffic policy, apply it so that it overrides the destination rule traffic policy.
		if subset.TrafficPolicy != nil {
			opts.policy = subset.TrafficPolicy
			applyTrafficPolicy(opts, proxy)
		}

		maybeApplyEdsConfig(subsetCluster)

		subsetCluster.Metadata = util.AddSubsetToMetadata(clusterMetadata, subset.Name)
		subsetClusters = append(subsetClusters, subsetCluster)
	}
	return subsetClusters
}

// castDestinationRuleOrDefault returns the destination rule enclosed by the config, if not null.
// Otherwise, return default (empty) DR.
func castDestinationRuleOrDefault(config *model.Config) *networking.DestinationRule {
	if config != nil {
		return config.Spec.(*networking.DestinationRule)
	}

	return &defaultDestinationRule
}

// maybeApplyEdsConfig applies EdsClusterConfig on the passed in cluster if it is an EDS type of cluster.
func maybeApplyEdsConfig(cluster *apiv2.Cluster) {
	switch v := cluster.ClusterDiscoveryType.(type) {
	case *apiv2.Cluster_Type:
		if v.Type != apiv2.Cluster_EDS {
			return
		}
	}
	cluster.EdsClusterConfig = &apiv2.Cluster_EdsClusterConfig{
		ServiceName: cluster.Name,
		EdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_Ads{
				Ads: &core.AggregatedConfigSource{},
			},
			InitialFetchTimeout: features.InitialFetchTimeout,
		},
	}
}
