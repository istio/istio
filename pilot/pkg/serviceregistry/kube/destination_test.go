// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package kube

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	destinationmodel "istio.io/istio/pilot/pkg/model/destination"
	"istio.io/istio/pkg/cluster"
)

func TestConvertServiceToDestinationIRMatchesExistingConversion(t *testing.T) {
	clusterID := cluster.ID("cluster-1")
	cases := []struct {
		name         string
		svc          corev1.Service
		wantEndpoint destinationmodel.EndpointSourceKind
	}{
		{name: "cluster ip", svc: testDestinationService("10.0.0.1"), wantEndpoint: destinationmodel.ServiceMembership},
		{name: "headless", svc: testDestinationService(corev1.ClusterIPNone), wantEndpoint: destinationmodel.ServiceMembership},
		{name: "external name", svc: func() corev1.Service {
			s := testDestinationService("")
			s.Spec.Type = corev1.ServiceTypeExternalName
			s.Spec.ExternalName = "api.example.com"
			return s
		}(), wantEndpoint: destinationmodel.DNS},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			legacy := ConvertService(tt.svc, nil, "cluster.local", clusterID, "cluster.local")
			got := ConvertServiceToDestinationIR(tt.svc, nil, "cluster.local", clusterID, "cluster.local")
			frontend := got.Frontend
			if frontend.Hostname != legacy.Hostname || frontend.DefaultAddress != legacy.DefaultAddress ||
				frontend.Resolution != legacy.Resolution || frontend.MeshExternal != legacy.MeshExternal ||
				frontend.ExternalName != legacy.Attributes.ExternalName || !reflect.DeepEqual(frontend.ClusterVIPs, legacy.ClusterVIPs) {
				t.Fatalf("frontend diverged from existing conversion:\nfrontend=%+v\nlegacy=%+v", frontend, legacy)
			}
			if len(got.Definitions) != len(legacy.Ports) || len(frontend.DefaultDestinations) != len(legacy.Ports) {
				t.Fatalf("got %d definitions and %d links for %d ports", len(got.Definitions), len(frontend.DefaultDestinations), len(legacy.Ports))
			}
			for i, definition := range got.Definitions {
				if definition.ID != frontend.DefaultDestinations[i] || definition.Endpoints.Kind != tt.wantEndpoint ||
					definition.Ports[0].Number != legacy.Ports[i].Port || definition.Ports[0].Protocol != legacy.Ports[i].Protocol {
					t.Fatalf("destination %d diverged: %+v", i, definition)
				}
			}
			if tt.wantEndpoint == destinationmodel.DNS && got.Definitions[0].Endpoints.Hostname.String() != tt.svc.Spec.ExternalName {
				t.Fatalf("ExternalName target lost: %+v", got.Definitions[0].Endpoints)
			}
		})
	}
}

func testDestinationService(clusterIP string) corev1.Service {
	grpc := "grpc"
	return corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "payments", Namespace: "apps", UID: types.UID("uid-1"), ResourceVersion: "7"},
		Spec: corev1.ServiceSpec{
			ClusterIP: clusterIP, ClusterIPs: []string{clusterIP}, Selector: map[string]string{"app": "payments"},
			Ports: []corev1.ServicePort{{Name: "http", Port: 80}, {Name: "grpc", Port: 9090, AppProtocol: &grpc}},
		},
	}
}

func TestConvertServiceToDestinationIRPreservesFrontendOnlyAddressing(t *testing.T) {
	svc := testDestinationService("10.0.0.1")
	svc.Spec.ExternalIPs = []string{"192.0.2.1"}
	svc.Spec.PublishNotReadyAddresses = true
	local := corev1.ServiceInternalTrafficPolicyLocal
	svc.Spec.InternalTrafficPolicy = &local
	ir := ConvertServiceToDestinationIR(svc, nil, "cluster.local", cluster.ID("cluster-1"), "cluster.local")
	if !ir.Frontend.NodeLocal || !ir.Frontend.PublishNotReadyAddresses ||
		!reflect.DeepEqual(ir.Frontend.Selector, svc.Spec.Selector) {
		t.Fatalf("frontend behavior lost: %+v", ir.Frontend)
	}
	if got := ir.Frontend.ClusterExternalAddresses.GetAddressesFor("cluster-1"); !reflect.DeepEqual(got, svc.Spec.ExternalIPs) {
		t.Fatalf("external addresses lost: %v", got)
	}
	if ir.Definitions[0].Endpoints.Kind != destinationmodel.ServiceMembership || ir.Frontend.Resolution != model.ClientSideLB {
		t.Fatalf("unexpected destination/frontend resolution: %+v", ir)
	}
}
