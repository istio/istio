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

package xds

import (
	"strings"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	"github.com/golang/protobuf/ptypes/wrappers"

	networkingapi "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/gvk"
)

type EndpointBuilder struct {
	// These fields define the primary key for an endpoint, and can be used as a cache key
	clusterName     string
	network         string
	clusterID       string
	locality        *core.Locality
	destinationRule *model.Config
	service         *model.Service

	// These fields are provided for convenience only
	subsetName string
	hostname   host.Name
	port       int
	push       *model.PushContext
}

func NewEndpointBuilder(clusterName string, proxy *model.Proxy, push *model.PushContext) EndpointBuilder {
	_, subsetName, hostname, port := model.ParseSubsetKey(clusterName)
	svc := push.ServiceForHostname(proxy, hostname)
	return EndpointBuilder{
		clusterName:     clusterName,
		network:         proxy.Metadata.Network,
		clusterID:       proxy.Metadata.ClusterID,
		locality:        proxy.Locality,
		service:         svc,
		destinationRule: push.DestinationRule(proxy, svc),

		push:       push,
		subsetName: subsetName,
		hostname:   hostname,
		port:       port,
	}
}

func (b EndpointBuilder) DestinationRule() *networkingapi.DestinationRule {
	if b.destinationRule == nil {
		return nil
	}
	return b.destinationRule.Spec.(*networkingapi.DestinationRule)
}

// CacheKey Functions
func (b EndpointBuilder) Key() string {
	params := []string{b.clusterName, b.network, b.clusterID, util.LocalityToString(b.locality)}
	if b.destinationRule != nil {
		params = append(params, b.destinationRule.Name+"/"+b.destinationRule.Namespace)
	}
	if b.service != nil {
		params = append(params, string(b.service.Hostname)+"/"+b.service.Attributes.Namespace)
	}
	return strings.Join(params, "~")
}

func (b EndpointBuilder) Cacheable() bool {
	// If service is not defined, we cannot do any caching as we will not have a way to
	// invalidate the results.
	// Service being nil means the EDS will be empty anyways, so not much lost here.
	return b.service != nil
}

func (b EndpointBuilder) DependentConfigs() []model.ConfigKey {
	configs := []model.ConfigKey{}
	if b.destinationRule != nil {
		configs = append(configs, model.ConfigKey{Kind: gvk.DestinationRule, Name: b.destinationRule.Name, Namespace: b.destinationRule.Namespace})
	}
	if b.service != nil {
		configs = append(configs, model.ConfigKey{Kind: gvk.ServiceEntry, Name: string(b.service.Hostname), Namespace: b.service.Attributes.Namespace})
	}
	return configs
}

// build LocalityLbEndpoints for a cluster from existing EndpointShards.
func (b *EndpointBuilder) buildLocalityLbEndpointsFromShards(
	shards *EndpointShards,
	svcPort *model.Port,
) []*endpoint.LocalityLbEndpoints {
	localityEpMap := make(map[string]*endpoint.LocalityLbEndpoints)

	// get the subset labels
	epLabels := getSubSetLabels(b.DestinationRule(), b.subsetName)

	// Determine whether or not the target service is considered local to the cluster
	// and should, therefore, not be accessed from outside the cluster.
	isClusterLocal := b.push.IsClusterLocal(b.service)

	shards.mutex.Lock()
	// The shards are updated independently, now need to filter and merge
	// for this cluster
	for clusterID, endpoints := range shards.Shards {
		// If the downstream service is configured as cluster-local, only include endpoints that
		// reside in the same cluster.
		if isClusterLocal && (clusterID != b.clusterID) {
			continue
		}

		for _, ep := range endpoints {
			if svcPort.Name != ep.ServicePortName {
				continue
			}
			// Port labels
			if !epLabels.HasSubsetOf(ep.Labels) {
				continue
			}

			locLbEps, found := localityEpMap[ep.Locality.Label]
			if !found {
				locLbEps = &endpoint.LocalityLbEndpoints{
					Locality:    util.ConvertLocality(ep.Locality.Label),
					LbEndpoints: make([]*endpoint.LbEndpoint, 0, len(endpoints)),
				}
				localityEpMap[ep.Locality.Label] = locLbEps
			}
			if ep.EnvoyEndpoint == nil {
				ep.EnvoyEndpoint = buildEnvoyLbEndpoint(ep)
			}
			locLbEps.LbEndpoints = append(locLbEps.LbEndpoints, ep.EnvoyEndpoint)
		}
	}
	shards.mutex.Unlock()

	locEps := make([]*endpoint.LocalityLbEndpoints, 0, len(localityEpMap))
	for _, locLbEps := range localityEpMap {
		var weight uint32
		for _, ep := range locLbEps.LbEndpoints {
			weight += ep.LoadBalancingWeight.GetValue()
		}
		locLbEps.LoadBalancingWeight = &wrappers.UInt32Value{
			Value: weight,
		}
		locEps = append(locEps, locLbEps)
	}

	if len(locEps) == 0 {
		b.push.AddMetric(model.ProxyStatusClusterNoInstances, b.clusterName, nil, "")
	}

	return locEps
}

// buildEnvoyLbEndpoint packs the endpoint based on istio info.
func buildEnvoyLbEndpoint(e *model.IstioEndpoint) *endpoint.LbEndpoint {
	addr := util.BuildAddress(e.Address, e.EndpointPort)

	epWeight := e.LbWeight
	if epWeight == 0 {
		epWeight = 1
	}
	ep := &endpoint.LbEndpoint{
		LoadBalancingWeight: &wrappers.UInt32Value{
			Value: epWeight,
		},
		HostIdentifier: &endpoint.LbEndpoint_Endpoint{
			Endpoint: &endpoint.Endpoint{
				Address: addr,
			},
		},
	}

	// Istio telemetry depends on the metadata value being set for endpoints in the mesh.
	// Istio endpoint level tls transport socket configuration depends on this logic
	// Do not removepilot/pkg/xds/fake.go
	ep.Metadata = util.BuildLbEndpointMetadata(e.Network, e.TLSMode)

	return ep
}
