// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package endpoints

import (
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/destination"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
)

// BuildDestinationLoadAssignment runs destination-owned endpoint slices through
// the standard EndpointBuilder pipeline. ProjectClassic is request-local
// compatibility metadata; it is never registered as a global/shadow Service.
// This retains dual-stack addresses, network/discoverability filters, mTLS
// metadata, locality weighting/failover, and DestinationRule subset behavior.
func BuildDestinationLoadAssignment(
	proxy *model.Proxy,
	push *model.PushContext,
	clusterName string,
	direction model.TrafficDirection,
	subset string,
	resolved destination.ResolvedDestination,
	dr *model.ConsolidatedDestRule,
) *endpoint.ClusterLoadAssignment {
	projection := destination.ProjectClassic(resolved, provider.Destination)
	index := model.NewEndpointIndex(model.DisabledCache{})
	index.UpdateServiceEndpoints(
		model.ShardKey{Cluster: proxy.Metadata.ClusterID, Provider: provider.Destination},
		resolved.Binding.RuntimeName.String(), resolved.Binding.Namespace, resolved.Endpoints, false,
	)
	builder := NewCDSEndpointBuilder(
		proxy, push, clusterName, direction, subset, resolved.Binding.RuntimeName,
		resolved.Binding.Port.Number, projection.Service, dr,
	)
	return builder.BuildClusterLoadAssignment(index)
}
