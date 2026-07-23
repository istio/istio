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
	"istio.io/istio/pkg/config/host"
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
	withResolver := NewIndex(definitions, bindings, opts, IndexOptions{Resolvers: map[ResolverKey]Resolver{
		{Kind: ServiceMembership}: func(_ krt.HandlerContext, definition DestinationDefinition, _ DestinationBinding) ([]*model.IstioEndpoint, []model.ConfigKey) {
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
	projection := ProjectClassic(resolved, provider.Destination)
	if projection.Service.Hostname != "opaque.internal" || projection.Service.Resolution != model.DNSLB ||
		!projection.Service.MeshExternal || projection.Service.Attributes.Name != "opaque.internal" {
		t.Fatalf("unexpected classic projection: %+v", projection.Service)
	}
	if len(projection.Endpoints) != 1 || projection.Endpoints[0].Addresses[0] != "payments.example.com" {
		t.Fatalf("unexpected endpoints: %+v", projection.Endpoints)
	}
}

func TestForRuntimeIsConsumerScoped(t *testing.T) {
	runtimeName := host.Name("shared.internal")
	firstConsumer := ConsumerID{Kind: "Gateway", Namespace: "a", Name: "gw"}
	secondConsumer := ConsumerID{Kind: "Gateway", Namespace: "b", Name: "gw"}
	resolved := krt.NewStaticCollection(nil, []ResolvedDestination{
		{Binding: DestinationBinding{
			Key: BindingKey{Consumer: firstConsumer, Policy: "first"}, Consumer: firstConsumer,
			RuntimeName: runtimeName, Port: DestinationPort{Number: 8080},
		}},
		{Binding: DestinationBinding{
			Key: BindingKey{Consumer: secondConsumer, Policy: "second"}, Consumer: secondConsumer,
			RuntimeName: runtimeName, Port: DestinationPort{Number: 8080},
		}},
	})
	index := &Index{Resolved: resolved}
	got, found := index.ForRuntime(secondConsumer, runtimeName, 8080)
	if !found || got.Binding.Consumer != secondConsumer {
		t.Fatalf("runtime lookup crossed consumer boundary: %+v, found=%v", got, found)
	}
	if _, found := index.ForRuntime(ConsumerID{Kind: "Gateway", Name: "other"}, runtimeName, 8080); found {
		t.Fatal("runtime lookup returned an unrelated consumer binding")
	}
}

func TestForRuntimeAggregatesInferencePoolPorts(t *testing.T) {
	runtimeName := host.Name("models.internal")
	consumer := ConsumerID{Kind: "Gateway", Namespace: "models", Name: "gw"}
	makeResolved := func(name string, port int, address string) ResolvedDestination {
		id := DefinitionID{
			Source: model.ConfigKey{Kind: kind.InferencePool, Namespace: "models", Name: "pool"},
			Port:   name,
		}
		return ResolvedDestination{
			Binding: DestinationBinding{
				Key: BindingKey{Definition: id, Consumer: consumer}, Definition: id, Consumer: consumer,
				RuntimeName: runtimeName, Port: DestinationPort{Name: name, Number: port},
			},
			Definition: DestinationDefinition{
				ID: id, Metadata: DestinationMetadata{Semantics: InferencePoolSemantics},
			},
			Endpoints: []*model.IstioEndpoint{{
				Addresses: []string{address}, EndpointPort: uint32(port), ServicePortName: name,
			}},
		}
	}
	index := &Index{Resolved: krt.NewStaticCollection(nil, []ResolvedDestination{
		makeResolved("http-0", 8080, "10.0.0.1"),
		makeResolved("http-1", 8081, "10.0.0.2"),
	})}
	got, found := index.ForRuntime(consumer, runtimeName, 8080)
	if !found || got.Binding.Port.Number != 8080 || len(got.Endpoints) != 2 {
		t.Fatalf("runtime lookup did not aggregate inference ports: %+v, found=%v", got, found)
	}
}

func TestResolverOwnershipSeparatesSharedEndpointKinds(t *testing.T) {
	serviceEntrySource := model.ConfigKey{Kind: kind.ServiceEntry, Namespace: "apps", Name: "passthrough"}
	inferenceSource := model.ConfigKey{Kind: kind.InferencePool, Namespace: "apps", Name: "pool"}
	serviceEntryID := DefinitionID{Source: serviceEntrySource, Port: "host/tcp"}
	inferenceID := DefinitionID{Source: inferenceSource, Port: "http"}
	consumer := ConsumerID{Kind: "Gateway", Namespace: "apps", Name: "gw"}
	definitions := krt.NewStaticCollection(nil, []DestinationDefinition{
		{ID: serviceEntryID, Namespace: "apps", Ports: []DestinationPort{{Name: "tcp", Number: 8080}},
			Endpoints: EndpointSource{Kind: ExtensionResolved, Source: serviceEntrySource, Extension: "serviceentry/passthrough"}},
		{ID: inferenceID, Namespace: "apps", Ports: []DestinationPort{{Name: "http", Number: 80}},
			Endpoints: EndpointSource{Kind: ExtensionResolved, Source: inferenceSource, Extension: "inferencepool/v1"}},
	})
	bindings := krt.NewStaticCollection(nil, []DestinationBinding{
		{Key: BindingKey{Definition: serviceEntryID, Consumer: consumer}, Definition: serviceEntryID, Consumer: consumer,
			Port: DestinationPort{Name: "tcp", Number: 8080}, Endpoints: EndpointSource{Kind: ExtensionResolved, Extension: "serviceentry/passthrough"}},
		{Key: BindingKey{Definition: inferenceID, Consumer: consumer}, Definition: inferenceID, Consumer: consumer,
			Port: DestinationPort{Name: "http", Number: 80}, Endpoints: EndpointSource{Kind: ExtensionResolved, Extension: "inferencepool/v1"}},
	})
	resolver := func(address string) Resolver {
		return func(_ krt.HandlerContext, _ DestinationDefinition, _ DestinationBinding) ([]*model.IstioEndpoint, []model.ConfigKey) {
			return []*model.IstioEndpoint{{Addresses: []string{address}}}, nil
		}
	}
	index := NewIndex(definitions, bindings, krt.NewOptionsBuilder(nil, "test", nil), IndexOptions{Resolvers: map[ResolverKey]Resolver{
		{Kind: ExtensionResolved, SourceKind: kind.ServiceEntry, Extension: "serviceentry/passthrough"}: resolver("serviceentry"),
		{Kind: ExtensionResolved, SourceKind: kind.InferencePool}:                                       resolver("inferencepool"),
	}})
	retry.UntilSuccessOrFail(t, func() error {
		if len(index.ForConsumer(consumer)) != 2 {
			return fmt.Errorf("joined destinations not resolved")
		}
		return nil
	})
	got := map[kind.Kind]string{}
	for _, resolved := range index.ForConsumer(consumer) {
		got[resolved.Definition.ID.Source.Kind] = resolved.Endpoints[0].Addresses[0]
	}
	if got[kind.ServiceEntry] != "serviceentry" || got[kind.InferencePool] != "inferencepool" {
		t.Fatalf("resolver ownership collided: %v", got)
	}
}

func TestProjectClassicInferenceSemantics(t *testing.T) {
	source := model.ConfigKey{Kind: kind.InferencePool, Namespace: "apps", Name: "models"}
	id := DefinitionID{Source: source, Port: "http-0"}
	resolved := ResolvedDestination{
		Binding: DestinationBinding{
			Key: BindingKey{Definition: id}, Definition: id, RuntimeName: "models.internal", Namespace: "apps",
			Port: DestinationPort{Name: "http-0", Number: 8080, Protocol: protocol.HTTP},
		},
		Definition: DestinationDefinition{
			ID: id, Namespace: "apps", Metadata: DestinationMetadata{Semantics: InferencePoolSemantics},
		},
	}
	projection := ProjectClassic(resolved, provider.Destination)
	if !projection.Service.UseInferenceSemantics() {
		t.Fatalf("inference semantics were not preserved: %+v", projection.Service.Attributes.Labels)
	}
}
