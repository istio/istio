// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package serviceentry

import (
	"testing"
	"time"

	networking "istio.io/api/networking/v1alpha3"
	destination "istio.io/istio/pilot/pkg/model/destination"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/visibility"
)

func TestServiceEntryDestinationAdapterMatchesClassicFrontend(t *testing.T) {
	cases := []struct {
		name       string
		resolution networking.ServiceEntry_Resolution
		wantSource destination.EndpointSourceKind
		addresses  []string
	}{
		{name: "static", resolution: networking.ServiceEntry_STATIC, wantSource: destination.StaticEndpoints, addresses: []string{"240.0.0.1"}},
		{name: "dns", resolution: networking.ServiceEntry_DNS, wantSource: destination.DNS},
		{name: "dns round robin", resolution: networking.ServiceEntry_DNS_ROUND_ROBIN, wantSource: destination.DNS},
		{name: "dynamic dns", resolution: networking.ServiceEntry_DYNAMIC_DNS, wantSource: destination.DynamicDNS},
		{name: "none", resolution: networking.ServiceEntry_NONE, wantSource: destination.ExtensionResolved, addresses: []string{"10.0.0.0/24"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cfg := serviceEntryAdapterConfig(tt.resolution, tt.addresses)
			got := ConvertToDestinationInputs(cfg)
			classic := convertServices(cfg, nil, false)
			if len(got.Frontends) != len(classic) {
				t.Fatalf("frontends = %d, classic services = %d", len(got.Frontends), len(classic))
			}
			if len(got.Destinations) != 2 {
				t.Fatalf("destinations = %d, want host x ports = 2", len(got.Destinations))
			}
			for n, frontend := range got.Frontends {
				service := classic[n]
				if frontend.Hostname != service.Hostname || frontend.DefaultAddress != service.DefaultAddress ||
					frontend.Resolution != service.Resolution || frontend.MeshExternal != service.MeshExternal {
					t.Fatalf("frontend differs from classic service:\nfrontend=%+v\nservice=%+v", frontend, service)
				}
				if len(frontend.Ports) != len(service.Ports) || frontend.Ports[0].Name != service.Ports[0].Name ||
					frontend.Ports[0].Number != service.Ports[0].Port || frontend.Ports[0].Protocol != service.Ports[0].Protocol {
					t.Fatalf("ports differ: %+v vs %+v", frontend.Ports, service.Ports)
				}
				if len(frontend.ExportTo) != 2 || frontend.ExportTo[0] != visibility.Private || frontend.ExportTo[1] != "team-a" {
					t.Fatalf("export visibility not preserved: %v", frontend.ExportTo)
				}
				if frontend.Selector["app"] != "payments" || len(frontend.SubjectAltNames) != 1 {
					t.Fatalf("selector or SANs not preserved: %+v", frontend)
				}
			}
			for _, definition := range got.Destinations {
				if definition.Endpoints.Kind != tt.wantSource || definition.ID.Source.Name != cfg.Name ||
					definition.Metadata.ServiceAccounts[0] != "spiffe://cluster.local/ns/apps/sa/payments" {
					t.Fatalf("unexpected destination: %+v", definition)
				}
			}
		})
	}
}

func TestServiceEntryDestinationAdapterHostAddressExpansion(t *testing.T) {
	cfg := serviceEntryAdapterConfig(networking.ServiceEntry_STATIC, []string{"192.0.2.1", "2001:db8::/128", "invalid"})
	cfg.Spec.(*networking.ServiceEntry).Hosts = []string{"a.example.com", "b.example.com"}
	got := ConvertToDestinationInputs(cfg)
	if len(got.Frontends) != 4 {
		t.Fatalf("frontends = %d, want two valid addresses per host", len(got.Frontends))
	}
	if len(got.Destinations) != 4 {
		t.Fatalf("destinations = %d, want two ports per host", len(got.Destinations))
	}
	if got.Frontends[1].DefaultAddress != "2001:db8::" {
		t.Fatalf("host prefix was not normalized: %+v", got.Frontends)
	}
	if got.Destinations[0].ID == got.Destinations[2].ID {
		t.Fatal("different hosts shared a destination identity")
	}
}

func TestServiceEntryDestinationAdapterUnspecifiedFrontend(t *testing.T) {
	cfg := serviceEntryAdapterConfig(networking.ServiceEntry_DNS, nil)
	got := ConvertToDestinationInputs(cfg)
	if len(got.Frontends) != 1 || got.Frontends[0].DefaultAddress != constants.UnspecifiedIP {
		t.Fatalf("unexpected addressless frontend: %+v", got.Frontends)
	}
	if got.Destinations[0].Endpoints.Hostname != "api.example.com" || got.Destinations[0].Endpoints.Port != 8443 {
		t.Fatalf("DNS target or targetPort not preserved: %+v", got.Destinations[0])
	}
}

func serviceEntryAdapterConfig(resolution networking.ServiceEntry_Resolution, addresses []string) config.Config {
	return config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.ServiceEntry, Name: "external-api", Namespace: "apps", UID: "uid-1",
			ResourceVersion: "rv-1", CreationTimestamp: time.Unix(10, 0),
		},
		Spec: &networking.ServiceEntry{
			Hosts: []string{"api.example.com"}, Addresses: addresses, Resolution: resolution,
			Location: networking.ServiceEntry_MESH_EXTERNAL,
			Ports: []*networking.ServicePort{
				{Name: "https", Number: 443, TargetPort: 8443, Protocol: protocol.HTTPS.String()},
				{Name: "grpc", Number: 9090, Protocol: protocol.GRPC.String()},
			},
			ExportTo:         []string{".", "team-a"},
			WorkloadSelector: &networking.WorkloadSelector{Labels: map[string]string{"app": "payments"}},
			SubjectAltNames:  []string{"spiffe://cluster.local/ns/apps/sa/payments"},
		},
	}
}
