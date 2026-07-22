// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package gateway

import (
	"maps"
	"strings"

	corev1 "k8s.io/api/core/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	inferencev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/destination"
	registrykube "istio.io/istio/pilot/pkg/serviceregistry/kube"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/ptr"
)

func inferencePoolEndpointResolver(
	pools krt.Collection[*inferencev1.InferencePool],
	pods krt.Collection[*corev1.Pod],
	clusterID cluster.ID,
	trustDomain string,
) destination.Resolver {
	podsByNamespace := krt.NewIndex(pods, "inference-pool-pods", func(pod *corev1.Pod) []string {
		return []string{pod.Namespace}
	})
	return func(ctx krt.HandlerContext, definition destination.DestinationDefinition, _ destination.DestinationBinding) ([]*model.IstioEndpoint, []model.ConfigKey) {
		pool := ptr.Flatten(krt.FetchOne(ctx, pools, krt.FilterKey(definition.ID.Source.Namespace+"/"+definition.ID.Source.Name)))
		if pool == nil || len(definition.Ports) != 1 {
			return nil, nil
		}
		return resolveInferencePoolPods(pool, podsByNamespace.Fetch(ctx, pool.Namespace), definition.Ports[0], clusterID, trustDomain), nil
	}
}

func resolveInferencePoolPods(
	pool *inferencev1.InferencePool,
	pods []*corev1.Pod,
	port destination.DestinationPort,
	clusterID cluster.ID,
	trustDomain string,
) []*model.IstioEndpoint {
	selector := make(map[string]string, len(pool.Spec.Selector.MatchLabels))
	for key, value := range pool.Spec.Selector.MatchLabels {
		selector[string(key)] = string(value)
	}
	matches := klabels.SelectorFromSet(selector)
	result := make([]*model.IstioEndpoint, 0)
	for _, pod := range pods {
		if !matches.Matches(klabels.Set(pod.Labels)) || !kubecontroller.IsPodReady(pod) {
			continue
		}
		addresses := pod.Status.PodIPs
		if len(addresses) == 0 && pod.Status.PodIP != "" {
			addresses = []corev1.PodIP{{IP: pod.Status.PodIP}}
		}
		for _, address := range addresses {
			if address.IP == "" {
				continue
			}
			podLabels := labels.Instance(maps.Clone(pod.Labels))
			locality := strings.TrimRight(strings.Join([]string{
				podLabels[corev1.LabelTopologyRegion], podLabels[corev1.LabelTopologyZone], podLabels[label.TopologySubzone.Name],
			}, "/"), "/")
			result = append(result, &model.IstioEndpoint{
				Addresses: []string{address.IP}, EndpointPort: uint32(port.Number), //nolint:gosec
				ServicePortName: port.Name, Labels: podLabels,
				ServiceAccount: registrykube.SecureNamingSAN(pod, trustDomain), TLSMode: registrykube.PodTLSMode(pod),
				Locality: model.Locality{Label: locality, ClusterID: clusterID},
				Network:  network.ID(podLabels[label.TopologyNetwork.Name]), WorkloadName: pod.Name,
				Namespace: pod.Namespace, NodeName: pod.Spec.NodeName, HealthStatus: model.Healthy,
			})
		}
	}
	return result
}
