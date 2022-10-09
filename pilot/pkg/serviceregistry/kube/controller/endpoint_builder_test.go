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
	"testing"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	cluster2 "istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/network"
)

func TestNewEndpointBuilderTopologyLabels(t *testing.T) {
	cases := []struct {
		name      string
		ctl       testController
		podLabels labels.Instance
		expected  labels.Instance
	}{
		{
			name:      "empty",
			ctl:       testController{},
			podLabels: nil,
			expected:  labels.Instance{},
		},
		{
			name: "region only",
			ctl: testController{
				locality: "myregion",
			},
			podLabels: labels.Instance{
				"k1":                       "v1",
				label.TopologyNetwork.Name: "mynetwork",
			},
			expected: labels.Instance{
				"k1":                       "v1",
				NodeRegionLabelGA:          "myregion",
				label.TopologyNetwork.Name: "mynetwork",
			},
		},
		{
			name: "region and zone",
			ctl: testController{
				locality: "myregion/myzone",
			},
			podLabels: labels.Instance{
				"k1":                       "v1",
				label.TopologyNetwork.Name: "mynetwork",
			},
			expected: labels.Instance{
				"k1":                       "v1",
				NodeRegionLabelGA:          "myregion",
				NodeZoneLabelGA:            "myzone",
				label.TopologyNetwork.Name: "mynetwork",
			},
		},
		{
			name: "network only ",
			ctl: testController{
				network: "mynetwork",
			},
			podLabels: labels.Instance{
				"k1": "v1",
			},
			expected: labels.Instance{
				"k1":                       "v1",
				label.TopologyNetwork.Name: "mynetwork",
			},
		},
		{
			name: "all values",
			ctl: testController{
				locality: "myregion/myzone/mysubzone",
				cluster:  "mycluster",
				network:  "mynetwork",
			},
			podLabels: labels.Instance{
				"k1":                       "v1",
				label.TopologyNetwork.Name: "mynetwork",
			},
			expected: labels.Instance{
				"k1":                       "v1",
				NodeRegionLabelGA:          "myregion",
				NodeZoneLabelGA:            "myzone",
				label.TopologySubzone.Name: "mysubzone",
				label.TopologyCluster.Name: "mycluster",
				label.TopologyNetwork.Name: "mynetwork",
			},
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			pod := v1.Pod{}
			pod.Name = "testpod"
			pod.Namespace = "testns"
			pod.Spec.ServiceAccountName = "testsan"
			pod.Labels = c.podLabels

			eb := NewEndpointBuilder(c.ctl, &pod)

			g := NewGomegaWithT(t)
			g.Expect(eb.labels).Should(Equal(c.expected))
		})
	}
}

func TestNewEndpointBuilderFromMetadataTopologyLabels(t *testing.T) {
	cases := []struct {
		name     string
		ctl      testController
		proxy    *model.Proxy
		expected labels.Instance
	}{
		{
			name: "empty",
			ctl:  testController{},
			proxy: &model.Proxy{
				Metadata: &model.NodeMetadata{},
			},
			expected: labels.Instance{},
		},
		{
			name: "region only",
			ctl:  testController{},
			proxy: &model.Proxy{
				Labels: labels.Instance{
					"k1":                       "v1",
					label.TopologyNetwork.Name: "mynetwork",
				},
				Metadata: &model.NodeMetadata{
					Labels: labels.Instance{
						"k1":                       "v1",
						label.TopologyNetwork.Name: "mynetwork",
					},
				},
				Locality: &core.Locality{
					Region: "myregion",
				},
			},
			expected: labels.Instance{
				"k1":                       "v1",
				NodeRegionLabelGA:          "myregion",
				label.TopologyNetwork.Name: "mynetwork",
			},
		},
		{
			name: "region and zone",
			ctl:  testController{},
			proxy: &model.Proxy{
				Labels: labels.Instance{
					"k1":                       "v1",
					label.TopologyNetwork.Name: "mynetwork",
				},
				Metadata: &model.NodeMetadata{
					Labels: labels.Instance{
						"k1":                       "v1",
						label.TopologyNetwork.Name: "mynetwork",
					},
				},
				Locality: &core.Locality{
					Region: "myregion",
					Zone:   "myzone",
				},
			},
			expected: labels.Instance{
				"k1":                       "v1",
				NodeRegionLabelGA:          "myregion",
				NodeZoneLabelGA:            "myzone",
				label.TopologyNetwork.Name: "mynetwork",
			},
		},
		{
			name: "all values set",
			ctl: testController{
				cluster: "mycluster",
			},
			proxy: &model.Proxy{
				Labels: labels.Instance{
					"k1":                       "v1",
					label.TopologyNetwork.Name: "mynetwork",
				},
				Metadata: &model.NodeMetadata{
					Labels: labels.Instance{
						"k1":                       "v1",
						label.TopologyNetwork.Name: "mynetwork",
					},
				},
				Locality: &core.Locality{
					Region:  "myregion",
					Zone:    "myzone",
					SubZone: "mysubzone",
				},
			},
			expected: labels.Instance{
				"k1":                       "v1",
				NodeRegionLabelGA:          "myregion",
				NodeZoneLabelGA:            "myzone",
				label.TopologySubzone.Name: "mysubzone",
				label.TopologyCluster.Name: "mycluster",
				label.TopologyNetwork.Name: "mynetwork",
			},
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			eb := NewEndpointBuilderFromMetadata(c.ctl, c.proxy)

			g := NewGomegaWithT(t)
			g.Expect(eb.labels).Should(Equal(c.expected))
		})
	}
}

var _ controllerInterface = testController{}

type testController struct {
	locality string
	cluster  cluster2.ID
	network  network.ID
}

func (c testController) getPodLocality(*v1.Pod) string {
	return c.locality
}

func (c testController) Network(ip string, instance labels.Instance) network.ID {
	return c.network
}

func (c testController) Cluster() cluster2.ID {
	return c.cluster
}
