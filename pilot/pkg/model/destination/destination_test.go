// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package destination

import (
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/kind"
)

func TestEndpointSourceComparable(t *testing.T) {
	a := EndpointSource{Kind: DNS, Hostname: "api.example.com", Port: 443}
	b := EndpointSource{Kind: DNS, Hostname: "api.example.com", Port: 443}
	if a != b {
		t.Fatalf("equal descriptors did not compare equal: %v != %v", a, b)
	}
}

func TestBindingIdentityAndEquality(t *testing.T) {
	id := DefinitionID{Source: model.ConfigKey{Kind: kind.XBackend, Namespace: "payments", Name: "api"}, UID: "uid-1", Port: "https"}
	consumer := ConsumerID{Kind: "Gateway", Namespace: "gateways", Name: "edge"}
	b := DestinationBinding{
		Key: BindingKey{Definition: id, Consumer: consumer}, RuntimeName: host.Name("api-123.payments.xbackend.internal"),
		Definition: id, Consumer: consumer, Port: DestinationPort{Name: "https", Number: 443, Protocol: protocol.HTTPS},
		Endpoints:    EndpointSource{Kind: DNS, Hostname: "api.example.com", Port: 443},
		Dependencies: NormalizeDependencies(id.Source),
	}
	if got, want := b.ResourceName(), id.String()+"/"+consumer.String(); got != want {
		t.Fatalf("ResourceName() = %q, want %q", got, want)
	}
	copy := b
	if !b.Equals(copy) {
		t.Fatal("identical bindings must be equal")
	}
	copy.Endpoints.Hostname = "other.example.com"
	if b.Equals(copy) {
		t.Fatal("endpoint identity change must affect equality")
	}
}

func TestNormalizeDependencies(t *testing.T) {
	a := model.ConfigKey{Kind: kind.Secret, Namespace: "ns", Name: "ca"}
	b := model.ConfigKey{Kind: kind.XBackend, Namespace: "ns", Name: "backend"}
	got := NormalizeDependencies(a, b, a)
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("unexpected normalized dependencies: %v", got)
	}
}
