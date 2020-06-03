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

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/labels"
)

// A stateful IstioEndpoint builder with metadata used to build IstioEndpoint
type EndpointBuilder struct {
	controller *Controller

	labels         labels.Instance
	uid            string
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
		Network:         b.controller.endpointNetwork(endpointAddress),
	}
}
