// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package core

import (
	"testing"

	"istio.io/api/label"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/destination"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/kind"
)

type fakeDestinationConsumerIndex map[destination.ConsumerID][]destination.ResolvedDestination

func (f fakeDestinationConsumerIndex) ForConsumer(consumer destination.ConsumerID) []destination.ResolvedDestination {
	return f[consumer]
}

func TestDestinationBindingsForGatewayIsolation(t *testing.T) {
	source := model.ConfigKey{Kind: kind.XBackend, Namespace: "backends", Name: "api"}
	id := destination.DefinitionID{Source: source, UID: "uid-1", Port: "https"}
	consumer := destination.ConsumerID{Kind: "Gateway", Namespace: "apps", Name: "waypoint-a"}
	resolved := destination.ResolvedDestination{Binding: destination.DestinationBinding{
		Key: destination.BindingKey{Definition: id, Consumer: consumer}, Definition: id, Consumer: consumer,
	}}
	index := fakeDestinationConsumerIndex{consumer: {resolved}}
	waypoint := func(name string) *model.Proxy {
		return &model.Proxy{ConfigNamespace: "apps", Labels: labels.Instance{
			label.IoK8sNetworkingGatewayGatewayName.Name: name,
		}}
	}
	if got := destinationBindingsForGateway(index, waypoint("waypoint-a")); len(got) != 1 {
		t.Fatalf("authorized waypoint binding not available: %+v", got)
	}
	if got := destinationBindingsForGateway(index, waypoint("waypoint-b")); len(got) != 0 {
		t.Fatalf("binding leaked to unrelated waypoint: %+v", got)
	}
	if got := destinationBindingsForGateway(index, &model.Proxy{ConfigNamespace: "apps"}); len(got) != 0 {
		t.Fatalf("binding leaked to unlabeled ztunnel-like proxy: %+v", got)
	}
}

func TestProjectWaypointOutboundDestinations(t *testing.T) {
	id := destination.DefinitionID{
		Source: model.ConfigKey{Kind: kind.XBackend, Namespace: "backends", Name: "api"},
		UID:    "uid-1", Port: "https",
	}
	runtimeName := host.Name("api-uid.backends.xbackend.internal")
	endpoint := &model.IstioEndpoint{Addresses: []string{"api.example.com"}, EndpointPort: 443}
	resolved := destination.ResolvedDestination{
		Binding: destination.DestinationBinding{
			Key: destination.BindingKey{Definition: id, Consumer: destination.ConsumerID{
				Kind: "Gateway", Namespace: "gateways", Name: "waypoint",
			}},
			RuntimeName: runtimeName,
			Definition:  id,
			Port:        destination.DestinationPort{Name: "https", Number: 443, Protocol: protocol.HTTPS},
			Endpoints:   destination.EndpointSource{Kind: destination.DNS, Hostname: "api.example.com", Port: 443},
			Namespace:   "backends",
		},
		Definition: destination.DestinationDefinition{ID: id, Namespace: "backends"},
		Endpoints:  []*model.IstioEndpoint{endpoint},
	}

	got := projectWaypointOutboundDestinations([]destination.ResolvedDestination{resolved})
	if len(got) != 1 {
		t.Fatalf("got %d projections, want 1", len(got))
	}
	if got[0].Service.Hostname != runtimeName {
		t.Fatalf("hostname = %q, want %q", got[0].Service.Hostname, runtimeName)
	}
	if got[0].Service.Attributes.ServiceRegistry != provider.GatewayBackend {
		t.Fatalf("registry = %q, want %q", got[0].Service.Attributes.ServiceRegistry, provider.GatewayBackend)
	}
	if len(got[0].Endpoints) != 1 || got[0].Endpoints[0] != endpoint {
		t.Fatalf("endpoint projection changed: %+v", got[0].Endpoints)
	}
}
