// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package destination

import (
	"fmt"
	"slices"
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test/util/retry"
)

func TestIndexActivationConsumerFilteringAndDependencies(t *testing.T) {
	source := model.ConfigKey{Kind: kind.XBackend, Namespace: "apps", Name: "payments"}
	id := DefinitionID{Source: source, UID: "uid-1", Port: "https"}
	definitions := krt.NewStaticCollection(nil, []DestinationDefinition{{
		ID: id, Namespace: "apps", Ports: []DestinationPort{{Name: "https", Number: 443, Protocol: protocol.HTTPS}},
		Endpoints: EndpointSource{Kind: DNS, Hostname: "payments.example.com", Port: 443},
	}})
	bindings := krt.NewStaticCollection[DestinationBinding](nil, nil)
	index := NewIndex(definitions, bindings, krt.NewOptionsBuilder(nil, "test", nil), IndexOptions{})
	gatewayA := ConsumerID{Kind: "Gateway", Namespace: "apps", Name: "a"}
	gatewayB := ConsumerID{Kind: "Gateway", Namespace: "apps", Name: "b"}
	if got := index.ForConsumer(gatewayA); len(got) != 0 {
		t.Fatalf("unreferenced definition became active: %v", got)
	}
	binding := DestinationBinding{
		Key: BindingKey{Definition: id, Consumer: gatewayA}, Definition: id, Consumer: gatewayA,
		RuntimeName: "payments-opaque.xbackend.internal", Namespace: "apps",
		Port:      DestinationPort{Name: "https", Number: 443, Protocol: protocol.HTTPS},
		Endpoints: EndpointSource{Kind: DNS, Hostname: "payments.example.com", Port: 443},
		Dependencies: []model.ConfigKey{
			{Kind: kind.HTTPRoute, Namespace: "apps", Name: "route"},
			{Kind: kind.Secret, Namespace: "apps", Name: "ca"},
		},
	}
	bindings.UpdateObject(binding)
	retry.UntilSuccessOrFail(t, func() error {
		if len(index.ForConsumer(gatewayA)) != 1 {
			return fmt.Errorf("binding is not visible")
		}
		return nil
	})
	if got := index.ForConsumer(gatewayB); len(got) != 0 {
		t.Fatalf("binding leaked: %v", got)
	}
	resolved := index.ForConsumer(gatewayA)[0]
	if got := resolved.Endpoints[0]; got.Addresses[0] != "payments.example.com" || got.EndpointPort != 443 {
		t.Fatalf("unexpected DNS endpoint: %+v", got)
	}
	want := []string{"HTTPRoute/apps/route", "Secret/apps/ca", "XBackend/apps/payments"}
	for n, key := range resolved.Dependencies {
		if key.String() != want[n] {
			t.Fatalf("dependency %d = %q, want %q", n, key, want[n])
		}
	}
	bindings.DeleteObject(binding.ResourceName())
	retry.UntilSuccessOrFail(t, func() error {
		if len(index.ForConsumer(gatewayA)) != 0 {
			return fmt.Errorf("last edge removal did not deactivate")
		}
		return nil
	})
}

func TestIndexFailsClosedAndSupportsResolverPlugins(t *testing.T) {
	source := model.ConfigKey{Kind: kind.Service, Namespace: "apps", Name: "catalog"}
	id := DefinitionID{Source: source, UID: "1", Port: "http"}
	definitions := krt.NewStaticCollection(nil, []DestinationDefinition{{
		ID: id, Namespace: "apps", Ports: []DestinationPort{{Name: "http", Number: 80}},
		Endpoints: EndpointSource{Kind: ServiceMembership, Source: source},
	}})
	consumer := ConsumerID{Kind: "Proxy", Namespace: "apps", Name: "pod"}
	bindings := krt.NewStaticCollection(nil, []DestinationBinding{{
		Key: BindingKey{Definition: id, Consumer: consumer}, Definition: id, Consumer: consumer,
		RuntimeName: "catalog.apps.svc.cluster.local", Port: DestinationPort{Name: "http", Number: 80},
		Endpoints: EndpointSource{Kind: ServiceMembership, Source: source},
	}})
	opts := krt.NewOptionsBuilder(nil, "test", nil)
	withoutResolver := NewIndex(definitions, bindings, opts, IndexOptions{})
	if got := withoutResolver.Resolved.List(); len(got) != 0 {
		t.Fatalf("unsupported source did not fail closed: %v", got)
	}
	resolverDependency := model.ConfigKey{Kind: kind.Service, Namespace: "apps", Name: "catalog-endpoints"}
	withResolver := NewIndex(definitions, bindings, opts, IndexOptions{Resolvers: map[EndpointSourceKind]Resolver{
		ServiceMembership: func(_ krt.HandlerContext, definition DestinationDefinition, _ DestinationBinding) ([]*model.IstioEndpoint, []model.ConfigKey) {
			return []*model.IstioEndpoint{{Addresses: []string{"10.0.0.1"}, ServicePortName: definition.Ports[0].Name, EndpointPort: 8080}}, []model.ConfigKey{resolverDependency}
		},
	}})
	retry.UntilSuccessOrFail(t, func() error {
		if len(withResolver.ForConsumer(consumer)) != 1 {
			return fmt.Errorf("plugin result not indexed")
		}
		return nil
	})
	if got := withResolver.ForConsumer(consumer)[0].Dependencies; !slices.Contains(got, resolverDependency) {
		t.Fatalf("resolver dependency not propagated: %v", got)
	}
}

func TestProjectClassic(t *testing.T) {
	source := model.ConfigKey{Kind: kind.XBackend, Namespace: "apps", Name: "payments"}
	id := DefinitionID{Source: source}
	resolved := ResolvedDestination{
		Binding: DestinationBinding{
			Key: BindingKey{Definition: id}, RuntimeName: "opaque.internal", Namespace: "apps",
			Port:      DestinationPort{Name: "https", Number: 443, Protocol: protocol.HTTPS},
			Endpoints: EndpointSource{Kind: DNS, Hostname: "payments.example.com", Port: 443},
		},
		Definition: DestinationDefinition{ID: id, Namespace: "apps"},
		Endpoints:  []*model.IstioEndpoint{{Addresses: []string{"payments.example.com"}, EndpointPort: 443}},
	}
	projection := ProjectClassic(resolved, provider.GatewayBackend)
	if projection.Service.Hostname != "opaque.internal" || projection.Service.Resolution != model.DNSLB ||
		!projection.Service.MeshExternal || projection.Service.Attributes.Name != "opaque.internal" {
		t.Fatalf("unexpected classic projection: %+v", projection.Service)
	}
	if len(projection.Endpoints) != 1 || projection.Endpoints[0].Addresses[0] != "payments.example.com" {
		t.Fatalf("unexpected endpoints: %+v", projection.Endpoints)
	}
}
