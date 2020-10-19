// Copyright Istio Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"net"

	v1 "k8s.io/api/core/v1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/labels"
	"istio.io/pkg/log"
)

// A stateful IstioEndpoint builder with metadata used to build IstioEndpoint
type EndpointBuilder struct {
	controller *Controller

	labels         labels.Instance
	uid            string
	metaNetwork    string
	serviceAccount string
	locality       model.Locality
	tlsMode        string
}

func NewEndpointBuilder(c *Controller, pod *v1.Pod) *EndpointBuilder {
	locality, sa, uid := "", "", ""
	var podLabels labels.Instance
	if pod != nil {
		locality = c.getPodLocality(pod)
		sa = kube.SecureNamingSAN(pod)
		uid = createUID(pod.Name, pod.Namespace)
		podLabels = pod.Labels
	}

	return &EndpointBuilder{
		controller:     c,
		labels:         podLabels,
		uid:            uid,
		serviceAccount: sa,
		locality: model.Locality{
			Label:     locality,
			ClusterID: c.clusterID,
		},
		tlsMode: kube.PodTLSMode(pod),
	}
}

func NewEndpointBuilderFromMetadata(c *Controller, proxy *model.Proxy) *EndpointBuilder {
	return &EndpointBuilder{
		controller:     c,
		metaNetwork:    proxy.Metadata.Network,
		labels:         proxy.Metadata.Labels,
		serviceAccount: proxy.Metadata.ServiceAccount,
		locality: model.Locality{
			Label:     util.LocalityToString(proxy.Locality),
			ClusterID: c.clusterID,
		},
		tlsMode: model.GetTLSModeFromEndpointLabels(proxy.Metadata.Labels),
	}
}

func (b *EndpointBuilder) buildIstioEndpoint(
	endpointAddress string,
	endpointPort int32,
	svcPortName string) *model.IstioEndpoint {
	if b == nil {
		return nil
	}

	return &model.IstioEndpoint{
		Labels:          b.labels,
		UID:             b.uid,
		ServiceAccount:  b.serviceAccount,
		Locality:        b.locality,
		TLSMode:         b.tlsMode,
		Address:         endpointAddress,
		EndpointPort:    uint32(endpointPort),
		ServicePortName: svcPortName,
		Network:         b.endpointNetwork(endpointAddress),
	}
}

// return the mesh network for the endpoint IP. Empty string if not found.
func (b *EndpointBuilder) endpointNetwork(endpointIP string) string {
	// Try to determine the network by checking whether the endpoint IP belongs
	// to any of the configure networks' CIDR ranges
	if b.controller.ranger != nil {
		entries, err := b.controller.ranger.ContainingNetworks(net.ParseIP(endpointIP))
		if err != nil {
			log.Errora(err)
			return ""
		}
		if len(entries) > 1 {
			log.Warnf("Found multiple networks CIDRs matching the endpoint IP: %s. Using the first match.", endpointIP)
		}
		if len(entries) > 0 {
			return (entries[0].(namedRangerEntry)).name
		}
	}

	// If not using cidr-lookup, or non of the given ranges contain the address, use the pod-label
	if nw := b.labels[label.IstioNetwork]; nw != "" {
		return nw
	}

	// If we're building the endpoint based on proxy meta, prefer the injected ISTIO_META_NETWORK value.
	if b.metaNetwork != "" {
		return b.metaNetwork
	}

	// Fallback to legacy fromRegistry setting, all endpoints from this cluster are on that network.
	return b.controller.networkForRegistry
}
