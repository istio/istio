// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package destination

import (
	"fmt"
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test/util/retry"
)

func TestEDSPublisherCollapsesBindingsAndDeletesShard(t *testing.T) {
	runtimeName := host.Name("pool.models.svc.cluster.local")
	resolved := krt.NewStaticCollection[ResolvedDestination](nil, nil)
	endpointIndex := model.NewEndpointIndex(model.DisabledCache{})
	publisher := NewEDSPublisher(resolved, cluster.ID("cluster-1"), model.NewEndpointIndexUpdater(endpointIndex),
		func(destination ResolvedDestination) bool {
			return destination.Definition.Metadata.Semantics == InferencePoolSemantics
		})
	makeResolved := func(consumer, port string, number uint32) ResolvedDestination {
		return ResolvedDestination{
			Binding: DestinationBinding{
				Key:         BindingKey{Consumer: ConsumerID{Name: consumer}, Policy: port},
				RuntimeName: runtimeName, Namespace: "models",
			},
			Definition: DestinationDefinition{Metadata: DestinationMetadata{Semantics: InferencePoolSemantics}},
			Endpoints: []*model.IstioEndpoint{{
				Addresses: []string{"10.0.0.1"}, ServicePortName: port, EndpointPort: number,
			}},
		}
	}
	first := makeResolved("gateway-a", "http-0", 8080)
	duplicate := makeResolved("gateway-b", "http-0", 8080)
	secondPort := makeResolved("gateway-a", "http-1", 8081)
	resolved.UpdateObject(first)
	resolved.UpdateObject(duplicate)
	resolved.UpdateObject(secondPort)

	retry.UntilSuccessOrFail(t, func() error {
		shards, found := endpointIndex.ShardsForService(runtimeName.String(), "models")
		if !found {
			return fmt.Errorf("destination shard not published")
		}
		shards.RLock()
		defer shards.RUnlock()
		got := shards.Shards[model.ShardKey{Cluster: "cluster-1", Provider: provider.Destination}]
		if len(got) != 2 {
			return fmt.Errorf("got %d endpoints, want two deduplicated ports", len(got))
		}
		return nil
	})
	if !publisher.HasSynced() {
		t.Fatal("publisher did not report synced")
	}

	resolved.DeleteObject(first.ResourceName())
	resolved.DeleteObject(duplicate.ResourceName())
	resolved.DeleteObject(secondPort.ResourceName())
	retry.UntilSuccessOrFail(t, func() error {
		if _, found := endpointIndex.ShardsForService(runtimeName.String(), "models"); found {
			return fmt.Errorf("destination shard was not deleted")
		}
		return nil
	})
}
