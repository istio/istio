// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package serviceentry

import (
	"strings"

	"istio.io/istio/pilot/pkg/model"
	destination "istio.io/istio/pilot/pkg/model/destination"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/krt"
)

// BuildDestinationCollections projects ServiceEntry source objects into KRT
// collections suitable for joining into the global DestinationIndex.
func BuildDestinationCollections(
	serviceEntries krt.Collection[config.Config],
	opts krt.OptionsBuilder,
) (
	krt.Collection[destination.FrontendDefinition],
	krt.Collection[destination.DestinationDefinition],
	krt.Collection[destination.DestinationBinding],
) {
	frontends := krt.NewManyCollection(serviceEntries, func(_ krt.HandlerContext, cfg config.Config) []destination.FrontendDefinition {
		return ConvertToDestinationInputs(cfg).Frontends
	}, opts.WithName("outputs/DestinationFrontends")...)
	definitions := krt.NewManyCollection(serviceEntries, func(_ krt.HandlerContext, cfg config.Config) []destination.DestinationDefinition {
		return ConvertToDestinationInputs(cfg).Destinations
	}, opts.WithName("outputs/DestinationDefinitions")...)
	bindings := krt.NewCollection(definitions, func(_ krt.HandlerContext, definition destination.DestinationDefinition) *destination.DestinationBinding {
		if len(definition.Ports) != 1 {
			return nil
		}
		consumer := destination.ConsumerID{Kind: "Mesh", Namespace: definition.Namespace}
		return &destination.DestinationBinding{
			Key:         destination.BindingKey{Definition: definition.ID, Consumer: consumer},
			RuntimeName: destinationHostname(definition), Definition: definition.ID, Consumer: consumer,
			Port: definition.Ports[0], Endpoints: definition.Endpoints, Connection: definition.Connection,
			Visibility: destination.ConsumerSet{Kind: "Mesh"}, Dependencies: definition.Dependencies,
			Namespace: definition.Namespace, CreationTime: definition.CreationTime,
		}
	}, opts.WithName("outputs/DestinationBindings")...)
	return frontends, definitions, bindings
}

// DestinationResolvers creates ServiceEntry resolvers backed by the existing
// instance conversion. Fetching through the index context preserves KRT
// invalidation for inline endpoints, WorkloadEntries, and selected workloads.
func DestinationResolvers(
	services krt.Collection[ServiceWithInstances],
	_ krt.OptionsBuilder,
) map[destination.ResolverKey]destination.Resolver {
	byNamespaceHost := krt.NewIndex(services, "destination-serviceentry-ns-host", func(service ServiceWithInstances) []string {
		return []string{service.Service.Attributes.Namespace + "/" + service.Service.Hostname.String()}
	})
	resolver := func(ctx krt.HandlerContext, definition destination.DestinationDefinition, binding destination.DestinationBinding) (
		[]*model.IstioEndpoint, []model.ConfigKey,
	) {
		if definition.ID.Source.Kind != kind.ServiceEntry {
			return fallbackAddressEndpoint(definition, binding), nil
		}
		hostname := destinationHostname(definition).String()
		candidates := byNamespaceHost.Fetch(ctx, definition.Namespace+"/"+hostname)
		endpoints := make([]*model.IstioEndpoint, 0)
		for _, candidate := range candidates {
			if candidate.Service.Attributes.K8sAttributes.ObjectName != definition.ID.Source.Name {
				continue
			}
			for _, instance := range candidate.Instances {
				if instance.ServicePort == nil || (instance.ServicePort.Name != binding.Port.Name && instance.ServicePort.Port != binding.Port.Number) {
					continue
				}
				endpoints = append(endpoints, instance.Endpoint.DeepCopy())
			}
		}
		return endpoints, []model.ConfigKey{definition.ID.Source}
	}
	return map[destination.ResolverKey]destination.Resolver{
		{Kind: destination.StaticEndpoints, SourceKind: kind.ServiceEntry}:                                          resolver,
		{Kind: destination.DNS, SourceKind: kind.ServiceEntry}:                                                      resolver,
		{Kind: destination.DynamicDNS, SourceKind: kind.ServiceEntry}:                                               resolver,
		{Kind: destination.ExtensionResolved, SourceKind: kind.ServiceEntry, Extension: "serviceentry/passthrough"}: resolver,
	}
}

func destinationHostname(definition destination.DestinationDefinition) host.Name {
	if definition.Endpoints.Hostname != "" {
		return definition.Endpoints.Hostname
	}
	hostname, _, _ := strings.Cut(definition.ID.Port, "/")
	return host.Name(hostname)
}

func fallbackAddressEndpoint(definition destination.DestinationDefinition, binding destination.DestinationBinding) []*model.IstioEndpoint {
	if binding.Endpoints.Hostname == "" || binding.Port.Number < 1 || binding.Port.Number > 65535 {
		return nil
	}
	port := binding.Endpoints.Port
	if port == 0 {
		port = uint32(binding.Port.Number)
	}
	return []*model.IstioEndpoint{{
		Addresses: []string{binding.Endpoints.Hostname.String()}, ServicePortName: binding.Port.Name,
		EndpointPort: port, Namespace: definition.Namespace,
	}}
}
