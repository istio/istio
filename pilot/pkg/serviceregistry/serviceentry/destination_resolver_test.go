// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package serviceentry

import (
	"fmt"
	"testing"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	destination "istio.io/istio/pilot/pkg/model/destination"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

func TestServiceEntryDestinationResolverParity(t *testing.T) {
	for _, resolution := range []networking.ServiceEntry_Resolution{
		networking.ServiceEntry_STATIC,
		networking.ServiceEntry_DNS,
		networking.ServiceEntry_NONE,
	} {
		t.Run(resolution.String(), func(t *testing.T) {
			cfg := serviceEntryAdapterConfig(resolution, nil)
			opts := krt.NewOptionsBuilder(test.NewStop(t), "test", nil)
			configs := krt.NewStaticCollection(nil, []config.Config{cfg}, opts.WithName("configs")...)
			_, definitions, bindings := BuildDestinationCollections(configs, opts)
			instances := krt.NewStaticCollection(nil, []ServiceWithInstances{
				serviceWithResolvedEndpoints(cfg.Name, cfg.Namespace, "api.example.com"),
			}, opts.WithName("instances")...)
			index := destination.NewIndex(definitions, bindings, opts, destination.IndexOptions{Resolvers: DestinationResolvers(instances, opts)})
			consumer := destination.ConsumerID{Kind: "Mesh", Namespace: cfg.Namespace}
			retry.UntilSuccessOrFail(t, func() error {
				if len(index.ForConsumer(consumer)) != 2 {
					return fmt.Errorf("destinations not resolved")
				}
				return nil
			})
			for _, resolved := range index.ForConsumer(consumer) {
				if len(resolved.Endpoints) != 1 {
					t.Fatalf("%s endpoints = %d, want 1", resolved.Binding.Port.Name, len(resolved.Endpoints))
				}
				endpoint := resolved.Endpoints[0]
				if endpoint.ServicePortName != resolved.Binding.Port.Name || endpoint.EndpointPort == 0 {
					t.Fatalf("endpoint port parity lost: %+v", endpoint)
				}
				if len(resolved.Dependencies) != 1 || resolved.Dependencies[0] != resolved.Definition.ID.Source {
					t.Fatalf("source dependency not preserved: %v", resolved.Dependencies)
				}
			}
		})
	}
}

func TestServiceEntryDestinationResolverSourceIsolationAndLifecycle(t *testing.T) {
	cfg := serviceEntryAdapterConfig(networking.ServiceEntry_STATIC, nil)
	other := cfg
	other.Name = "other-entry"
	opts := krt.NewOptionsBuilder(test.NewStop(t), "test", nil)
	configs := krt.NewStaticCollection(nil, []config.Config{cfg}, opts.WithName("configs")...)
	_, definitions, bindings := BuildDestinationCollections(configs, opts)
	services := krt.NewStaticCollection(nil, []ServiceWithInstances{
		serviceWithResolvedEndpoints(other.Name, other.Namespace, "api.example.com"),
		serviceWithResolvedEndpoints(cfg.Name, cfg.Namespace, "api.example.com"),
	}, opts.WithName("instances")...)
	index := destination.NewIndex(definitions, bindings, opts, destination.IndexOptions{Resolvers: DestinationResolvers(services, opts)})
	consumer := destination.ConsumerID{Kind: "Mesh", Namespace: cfg.Namespace}
	retry.UntilSuccessOrFail(t, func() error {
		resolved := index.ForConsumer(consumer)
		if len(resolved) != 2 {
			return fmt.Errorf("got %d destinations", len(resolved))
		}
		for _, result := range resolved {
			if len(result.Endpoints) != 1 || result.Endpoints[0].Addresses[0] == "203.0.113.99" {
				return fmt.Errorf("source isolation failed: %+v", result.Endpoints)
			}
		}
		return nil
	})
	configs.DeleteObject(cfg.Namespace + "/" + cfg.Name)
	retry.UntilSuccessOrFail(t, func() error {
		if len(index.ForConsumer(consumer)) != 0 {
			return fmt.Errorf("deleted source remains active")
		}
		return nil
	})
}

func serviceWithResolvedEndpoints(objectName, namespace, hostname string) ServiceWithInstances {
	https := serviceWithResolvedEndpoint(objectName, namespace, hostname, "https", 443, "192.0.2.10", 8443)
	grpc := serviceWithResolvedEndpoint(objectName, namespace, hostname, "grpc", 9090, "192.0.2.11", 9090)
	if objectName == "other-entry" {
		https.Instances[0].Endpoint.Addresses[0] = "203.0.113.99"
		grpc.Instances[0].Endpoint.Addresses[0] = "203.0.113.99"
	}
	https.Service.Ports = append(https.Service.Ports, grpc.Service.Ports...)
	grpc.Instances[0].Service = https.Service
	https.Instances = append(https.Instances, grpc.Instances...)
	return https
}

func serviceWithResolvedEndpoint(
	objectName, namespace, hostname, portName string,
	servicePort int,
	address string,
	endpointPort uint32,
) ServiceWithInstances {
	port := &model.Port{Name: portName, Port: servicePort, Protocol: protocol.TCP}
	service := &model.Service{
		Hostname: host.Name(hostname), Ports: model.PortList{port},
		Attributes: model.ServiceAttributes{
			Namespace: namespace, K8sAttributes: model.K8sAttributes{ObjectName: objectName},
		},
	}
	return ServiceWithInstances{
		Service: service,
		Instances: []*model.ServiceInstance{{
			Service: service, ServicePort: port,
			Endpoint: &model.IstioEndpoint{
				Addresses: []string{address}, ServicePortName: portName, EndpointPort: endpointPort, Namespace: namespace,
			},
		}},
	}
}
