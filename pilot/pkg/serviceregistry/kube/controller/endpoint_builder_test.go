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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"
	labelutil "istio.io/istio/pilot/pkg/serviceregistry/util/label"
	cluster2 "istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	pkgmodel "istio.io/istio/pkg/model"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
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
			name: "network priority",
			ctl: testController{
				network: "ns-network",
			},
			podLabels: labels.Instance{
				label.TopologyNetwork.Name: "pod-network",
			},
			expected: labels.Instance{
				label.TopologyNetwork.Name: "pod-network",
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
		t.Run(c.name, func(t *testing.T) {
			pod := v1.Pod{}
			pod.Name = "testpod"
			pod.Namespace = "testns"
			pod.Spec.ServiceAccountName = "testsan"
			pod.Labels = c.podLabels
			pod.Spec.NodeName = "fake"
			// All should get this
			c.expected[labelutil.LabelHostname] = "fake"

			loc := pkgmodel.ConvertLocality(c.ctl.locality)
			fc := kube.NewFakeClient(&v1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "fake", Labels: map[string]string{
					NodeRegionLabelGA:          loc.Region,
					NodeZoneLabel:              loc.Zone,
					label.TopologySubzone.Name: loc.SubZone,
				}},
				Spec:   v1.NodeSpec{},
				Status: v1.NodeStatus{},
			})
			nodes := kclient.New[*v1.Node](fc)
			fc.RunAndWait(test.NewStop(t))
			cc := &Controller{
				nodes:       nodes,
				meshWatcher: meshwatcher.NewTestWatcher(mesh.DefaultMeshConfig()),
				networkManager: &networkManager{
					clusterID: c.ctl.cluster,
					network:   c.ctl.network,
				},
				opts: Options{ClusterID: c.ctl.cluster},
			}
			eb := cc.NewEndpointBuilder(&pod)

			assert.Equal(t, eb.labels, c.expected)
		})
	}
}

type testController struct {
	locality string
	cluster  cluster2.ID
	network  network.ID
}
