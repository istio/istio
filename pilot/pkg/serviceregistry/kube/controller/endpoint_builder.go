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
	kubeUtil "istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

// A stateful IstioEndpoint builder with metadata used to build IstioEndpoint
type EndpointBuilder struct {
	controller controllerInterface

	labels         labels.Instance
	metaNetwork    string
	serviceAccount string
	locality       model.Locality
	tlsMode        string
	workloadName   string
	namespace      string

	// Values used to build dns name tables per pod.
	// The the hostname of the Pod, by default equals to pod name.
	hostname string
	// If specified, the fully qualified Pod hostname will be "<hostname>.<subdomain>.<pod namespace>.svc.<cluster domain>".
	subDomain string
}

func NewEndpointBuilder(c controllerInterface, pod *v1.Pod) *EndpointBuilder {
	locality, sa, wn, namespace, hostname, subdomain := "", "", "", "", "", ""
	var podLabels labels.Instance
	if pod != nil {
		locality = c.getPodLocality(pod)
		sa = kube.SecureNamingSAN(pod)
		podLabels = pod.Labels
		namespace = pod.Namespace
		subdomain = pod.Spec.Subdomain
		if subdomain != "" {
			hostname = pod.Spec.Hostname
			if hostname == "" {
				hostname = pod.Name
			}
		}
	}
	dm, _ := kubeUtil.GetDeployMetaFromPod(pod)
	if dm != nil {
		wn = dm.Name
	}

	return &EndpointBuilder{
		controller:     c,
		labels:         augmentLabels(podLabels, c.Cluster(), locality),
		serviceAccount: sa,
		locality: model.Locality{
			Label:     locality,
			ClusterID: c.Cluster(),
		},
		tlsMode:      kube.PodTLSMode(pod),
		workloadName: wn,
		namespace:    namespace,
		hostname:     hostname,
		subDomain:    subdomain,
	}
}

func NewEndpointBuilderFromMetadata(c controllerInterface, proxy *model.Proxy) *EndpointBuilder {
	locality := util.LocalityToString(proxy.Locality)
	return &EndpointBuilder{
		controller:     c,
		metaNetwork:    proxy.Metadata.Network,
		labels:         augmentLabels(proxy.Metadata.Labels, c.Cluster(), locality),
		serviceAccount: proxy.Metadata.ServiceAccount,
		locality: model.Locality{
			Label:     locality,
			ClusterID: c.Cluster(),
		},
		tlsMode: model.GetTLSModeFromEndpointLabels(proxy.Metadata.Labels),
	}
}

// augmentLabels adds additional labels to the those provided.
func augmentLabels(in labels.Instance, clusterID, locality string) labels.Instance {
	// Copy the original labels to a new map.
	out := make(labels.Instance)
	for k, v := range in {
		out[k] = v
	}

	// Don't need to add label.TopologyNetwork.Name, since that's already added by injection.
	region, zone, subzone := model.SplitLocalityLabel(locality)
	if len(region) > 0 {
		out[NodeRegionLabelGA] = region
	}
	if len(zone) > 0 {
		out[NodeZoneLabelGA] = zone
	}
	if len(subzone) > 0 {
		out[label.TopologySubzone.Name] = subzone
	}
	if len(clusterID) > 0 {
		out[label.TopologyCluster.Name] = clusterID
	}
	return out
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
		ServiceAccount:  b.serviceAccount,
		Locality:        b.locality,
		TLSMode:         b.tlsMode,
		Address:         endpointAddress,
		EndpointPort:    uint32(endpointPort),
		ServicePortName: svcPortName,
		Network:         b.endpointNetwork(endpointAddress),
		WorkloadName:    b.workloadName,
		Namespace:       b.namespace,
		HostName:        b.hostname,
		SubDomain:       b.subDomain,
	}
}

// return the mesh network for the endpoint IP. Empty string if not found.
func (b *EndpointBuilder) endpointNetwork(endpointIP string) string {
	// Try to determine the network by checking whether the endpoint IP belongs
	// to any of the configure networks' CIDR ranges
	if b.controller.cidrRanger() != nil {
		entries, err := b.controller.cidrRanger().ContainingNetworks(net.ParseIP(endpointIP))
		if err != nil {
			log.Error(err)
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
	if nw := b.labels[label.TopologyNetwork.Name]; nw != "" {
		return nw
	}

	// If we're building the endpoint based on proxy meta, prefer the injected ISTIO_META_NETWORK value.
	if b.metaNetwork != "" {
		return b.metaNetwork
	}

	// Fallback to legacy fromRegistry setting, all endpoints from this cluster are on that network.
	return b.controller.defaultNetwork()
}

// TODO(lambdai): Make it true everywhere.
func (b *EndpointBuilder) supportsH2Tunnel() bool {
	return false
}
