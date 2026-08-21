// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package gateway

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	inferencev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"

	"istio.io/istio/pilot/pkg/model/destination"
	"istio.io/istio/pkg/util/sets"
)

func TestInferencePoolDestinationBindings(t *testing.T) {
	pool := &inferencev1.InferencePool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "models", UID: "uid-1"},
		Spec: inferencev1.InferencePoolSpec{
			TargetPorts:       []inferencev1.Port{{Number: 8080}},
			EndpointPickerRef: inferencev1.EndpointPickerRef{Name: "picker", Port: &inferencev1.Port{Number: 9002}},
		},
	}
	compiled := createInferencePoolObject(pool, sets.New(
		types.NamespacedName{Namespace: "gateways", Name: "b"},
		types.NamespacedName{Namespace: "gateways", Name: "a"},
	), "cluster.local")
	bindings := compiled.DestinationBindings()
	definitions := compiled.destinationDefinitions()
	if len(bindings) != 2 {
		t.Fatalf("got %d bindings, want 2", len(bindings))
	}
	if len(definitions) != 1 || definitions[0].ID != bindings[0].Definition {
		t.Fatalf("definition and binding identity differ: %+v, %+v", definitions, bindings[0].Definition)
	}
	if got, want := bindings[0].Consumer.Name, "a"; got != want {
		t.Fatalf("first consumer = %q, want %q", got, want)
	}
	if got, want := bindings[0].Endpoints.Kind, destination.ExtensionResolved; got != want {
		t.Fatalf("endpoint source = %q, want %q", got, want)
	}
	if got, want := bindings[0].RuntimeName.String(), "pool-ip-27cac550.models.svc.cluster.local"; got != want {
		t.Fatalf("runtime name = %q, want %q", got, want)
	}
	if got, want := bindings[0].Port.Number, 8080; got != want {
		t.Fatalf("target port = %d, want %d", got, want)
	}
	if bindings[0].Endpoints.Extension != "picker" || bindings[0].Endpoints.Port != 9002 {
		t.Fatalf("endpoint picker metadata not preserved: %+v", bindings[0].Endpoints)
	}
	if definitions[0].Metadata.Semantics != destination.InferencePoolSemantics ||
		definitions[0].Metadata.FailureMode != destination.ExtensionFailClose {
		t.Fatalf("typed inference metadata not preserved: %+v", definitions[0].Metadata)
	}
}

func TestInferencePoolCompilationDoesNotCreateShadowService(t *testing.T) {
	pool := &inferencev1.InferencePool{
		ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "models", UID: "uid-1"},
		Spec: inferencev1.InferencePoolSpec{
			TargetPorts: []inferencev1.Port{{Number: 8080}, {Number: 8081}},
			AppProtocol: inferencev1.AppProtocol("h2c"),
			EndpointPickerRef: inferencev1.EndpointPickerRef{
				Name: "picker", Port: &inferencev1.Port{Number: 9002},
				FailureMode: inferencev1.EndpointPickerFailOpen,
			},
		},
	}
	compiled := createInferencePoolObject(pool,
		sets.New(types.NamespacedName{Namespace: "gateways", Name: "gateway"}), "cluster.local")
	if compiled == nil {
		t.Fatal("expected compiled InferencePool")
	}
	if got := compiled.ResourceName(); got != "models/pool" {
		t.Fatalf("resource name = %q, want models/pool", got)
	}
	definitions := compiled.destinationDefinitions()
	if len(definitions) != 2 || definitions[0].Ports[0].Number != 8080 || definitions[1].Ports[0].Number != 8081 {
		t.Fatalf("target ports not compiled directly: %+v", definitions)
	}
	if definitions[0].Metadata.FailureMode != destination.ExtensionFailOpen {
		t.Fatalf("failure mode = %q, want FailOpen", definitions[0].Metadata.FailureMode)
	}
}
