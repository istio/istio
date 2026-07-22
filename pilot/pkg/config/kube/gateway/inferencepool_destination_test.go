// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package gateway

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	inferencev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"

	"istio.io/istio/pilot/pkg/model/destination"
	"istio.io/istio/pkg/config/protocol"
)

func TestInferencePoolEndpointResolver(t *testing.T) {
	pool := &inferencev1.InferencePool{
		ObjectMeta: metav1.ObjectMeta{Name: "models", Namespace: "apps"},
		Spec: inferencev1.InferencePoolSpec{Selector: inferencev1.LabelSelector{
			MatchLabels: map[inferencev1.LabelKey]inferencev1.LabelValue{"app": "model"},
		}},
	}
	ready := corev1.ConditionTrue
	pods := []*corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "selected", Namespace: "apps", Labels: map[string]string{"app": "model"}},
			Spec:       corev1.PodSpec{ServiceAccountName: "model-sa", NodeName: "node-a"},
			Status:     corev1.PodStatus{PodIP: "10.0.0.1", Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: ready}}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "apps", Labels: map[string]string{"app": "other"}},
			Status:     corev1.PodStatus{PodIP: "10.0.0.2", Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: ready}}},
		},
	}
	resolved := resolveInferencePoolPods(pool, pods, destination.DestinationPort{Name: "http-0", Number: 8080, Protocol: protocol.HTTP}, "cluster-a", "cluster.local")
	if len(resolved) != 1 {
		t.Fatalf("got %d endpoints, want 1: %+v", len(resolved), resolved)
	}
	endpoint := resolved[0]
	if endpoint.Addresses[0] != "10.0.0.1" || endpoint.EndpointPort != 8080 || endpoint.ServiceAccount != "spiffe://cluster.local/ns/apps/sa/model-sa" {
		t.Fatalf("unexpected endpoint: %+v", endpoint)
	}
}
