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
	v1 "k8s.io/api/core/v1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	labelutil "istio.io/istio/pilot/pkg/serviceregistry/util/label"
	"istio.io/istio/pkg/config/labels"
	kubeUtil "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/network"
)

// EndpointBuilder is a stateful IstioEndpoint builder with metadata used to build IstioEndpoint
type EndpointBuilder struct {
	controller controllerInterface

	labels         labels.Instance
	metaNetwork    network.ID
	serviceAccount string
	locality       model.Locality
	tlsMode        string
	workloadName   string
	namespace      string

	// Values used to build dns name tables per pod.
	// The hostname of the Pod, by default equals to pod name.
	hostname string
	// If specified, the fully qualified Pod hostname will be "<hostname>.<subdomain>.<pod namespace>.svc.<cluster domain>".
	subDomain string
}

func NewEndpointBuilder(c controllerInterface, pod *v1.Pod) *EndpointBuilder {
	locality, sa, namespace, hostname, subdomain, ip := "", "", "", "", "", ""
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
		ip = pod.Status.PodIP
	}
	dm, _ := kubeUtil.GetDeployMetaFromPod(pod)
	out := &EndpointBuilder{
		controller:     c,
		serviceAccount: sa,
		locality: model.Locality{
			Label:     locality,
			ClusterID: c.Cluster(),
		},
		tlsMode:      kube.PodTLSMode(pod),
		workloadName: dm.Name,
		namespace:    namespace,
		hostname:     hostname,
		subDomain:    subdomain,
	}
	networkID := out.endpointNetwork(ip)
	out.labels = labelutil.AugmentLabels(podLabels, c.Cluster(), locality, networkID)
	return out
}

func NewEndpointBuilderFromMetadata(c controllerInterface, proxy *model.Proxy) *EndpointBuilder {
	locality := util.LocalityToString(proxy.Locality)
	out := &EndpointBuilder{
		controller:     c,
		metaNetwork:    proxy.Metadata.Network,
		serviceAccount: proxy.Metadata.ServiceAccount,
		locality: model.Locality{
			Label:     locality,
			ClusterID: c.Cluster(),
		},
		tlsMode: model.GetTLSModeFromEndpointLabels(proxy.Labels),
	}
	var networkID network.ID
	if len(proxy.IPAddresses) > 0 {
		networkID = out.endpointNetwork(proxy.IPAddresses[0])
	}
	out.labels = labelutil.AugmentLabels(proxy.Labels, c.Cluster(), locality, networkID)
	return out
}

func (b *EndpointBuilder) buildIstioEndpoint(
	endpointAddress string,
	endpointPort int32,
	svcPortName string,
	discoverabilityPolicy model.EndpointDiscoverabilityPolicy,
) *model.IstioEndpoint {
	if b == nil {
		return nil
	}

	// in case pod is not found when init EndpointBuilder.
	networkID := network.ID(b.labels[label.TopologyNetwork.Name])
	if networkID == "" {
		networkID = b.endpointNetwork(endpointAddress)
		b.labels[label.TopologyNetwork.Name] = string(networkID)
	}

	return &model.IstioEndpoint{
		Labels:                b.labels,
		ServiceAccount:        b.serviceAccount,
		Locality:              b.locality,
		TLSMode:               b.tlsMode,
		Address:               endpointAddress,
		EndpointPort:          uint32(endpointPort),
		ServicePortName:       svcPortName,
		Network:               networkID,
		WorkloadName:          b.workloadName,
		Namespace:             b.namespace,
		HostName:              b.hostname,
		SubDomain:             b.subDomain,
		DiscoverabilityPolicy: discoverabilityPolicy,
	}
}

// return the mesh network for the endpoint IP. Empty string if not found.
func (b *EndpointBuilder) endpointNetwork(endpointIP string) network.ID {
	// If we're building the endpoint based on proxy meta, prefer the injected ISTIO_META_NETWORK value.
	if b.metaNetwork != "" {
		return b.metaNetwork
	}

	return b.controller.Network(endpointIP, b.labels)
}
