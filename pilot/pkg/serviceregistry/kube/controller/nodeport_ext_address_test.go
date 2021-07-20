// Copyright Istio Authors
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
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
)

func TestExtractNodeNameFromEndpoints(t *testing.T) {
	epOnNode1 := &model.IstioEndpoint{
		NodeName:        "node1",
		EndpointPort:    8080,
		WorkloadName:    "app-1",
		Namespace:       "app-ns",
		ServicePortName: "http-app",
	}
	epOnNode2 := &model.IstioEndpoint{
		NodeName:        "node2",
		EndpointPort:    8080,
		WorkloadName:    "app-1",
		Namespace:       "app-ns",
		ServicePortName: "http-app",
	}
	epWithoutNodeName := &model.IstioEndpoint{
		EndpointPort:    8090,
		WorkloadName:    "app-1",
		Namespace:       "app-ns",
		ServicePortName: "http-app",
	}
	for _, tt := range []struct {
		name          string
		endpoints     []*model.IstioEndpoint
		expectedNodes map[string]struct{}
	}{
		{
			name:          "workload with single endpoint on node1",
			endpoints:     []*model.IstioEndpoint{epOnNode1},
			expectedNodes: map[string]struct{}{"node1": {}},
		},
		{
			name:          "workload with multiple endpoints on different nodes",
			endpoints:     []*model.IstioEndpoint{epOnNode1, epOnNode2},
			expectedNodes: map[string]struct{}{"node1": {}, "node2": {}},
		},
		{
			name:          "workload without node-name filled",
			endpoints:     []*model.IstioEndpoint{epWithoutNodeName},
			expectedNodes: map[string]struct{}{},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			actualNodes := getWorkloadNodeLocations(tt.endpoints)
			if diff := cmp.Diff(actualNodes, tt.expectedNodes); diff != "" {
				t.Fatalf("diff:\n%s\nexpected:\n%s\nactual:\n%s\n", diff, tt.expectedNodes, actualNodes)
			}
		})
	}
}

func TestUpdateClusterExternalAddressesForNodePortServices(t *testing.T) {
	clusterNodePortSvc := &model.Service{
		Hostname: host.Name("cluster-svc.ingress.svc.cluster.local"),
		Attributes: model.ServiceAttributes{
			ExternalTrafficPolicy: v1.ServiceExternalTrafficPolicyTypeCluster,
		},
	}
	localNodePortSvc := &model.Service{
		Hostname: host.Name("local-svc.ingress.svc.cluster.local"),
		Attributes: model.ServiceAttributes{
			ExternalTrafficPolicy: v1.ServiceExternalTrafficPolicyTypeLocal,
		},
	}
	clusterID := cluster.ID("test-cluster")
	nodes := map[string]kubernetesNode{
		"node1": {address: "10.10.10.10", labels: labels.Instance{NodeZoneLabelGA: "east"}},
		"node2": {address: "10.10.10.20", labels: labels.Instance{NodeZoneLabelGA: "west"}},
		"node3": {address: "10.10.10.30", labels: labels.Instance{NodeZoneLabelGA: "east"}},
		"node4": {address: "10.10.10.40", labels: labels.Instance{NodeZoneLabelGA: "west"}},
	}
	for _, tt := range []struct {
		name                 string
		service              *model.Service
		workloadHostingNodes []string
		nodeLabelSelector    labels.Instance
		expectedAddresses    []string
	}{
		{
			name:                 "external traffic policy is Cluster in 4 node cluster",
			service:              clusterNodePortSvc,
			workloadHostingNodes: []string{"node1", "node2"},
			expectedAddresses:    []string{"10.10.10.10", "10.10.10.20", "10.10.10.30", "10.10.10.40"},
			nodeLabelSelector:    labels.Instance{},
		},
		{
			name:                 "external traffic policy is Local in 4 node cluster",
			service:              localNodePortSvc,
			workloadHostingNodes: []string{"node1", "node2"},
			expectedAddresses:    []string{"10.10.10.10", "10.10.10.20"},
			nodeLabelSelector:    labels.Instance{},
		},
		{
			name:                 "external traffic policy is Cluster with node-label selector specified",
			service:              clusterNodePortSvc,
			workloadHostingNodes: []string{"node1", "node3"},
			expectedAddresses:    []string{"10.10.10.10", "10.10.10.30"},
			nodeLabelSelector:    labels.Instance{NodeZoneLabelGA: "east"},
		},
		{
			name:                 "external traffic policy is Local with node-label selector specified",
			service:              localNodePortSvc,
			workloadHostingNodes: []string{"node3"},
			expectedAddresses:    []string{"10.10.10.30"},
			nodeLabelSelector:    labels.Instance{NodeZoneLabelGA: "east"},
		},
		{
			name:                 "external traffic policy is Local with nil node-selector",
			service:              localNodePortSvc,
			workloadHostingNodes: []string{"node3", "node2"},
			expectedAddresses:    []string{"10.10.10.20", "10.10.10.30"},
			nodeLabelSelector:    nil,
		},
		{
			name:                 "node-label selector does not match",
			service:              clusterNodePortSvc,
			workloadHostingNodes: []string{"node1", "node3"},
			expectedAddresses:    nil,
			nodeLabelSelector:    labels.Instance{NodeZoneLabelGA: "south"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := &Controller{
				opts:                                     Options{ClusterID: clusterID},
				nodeInfoMap:                              nodes,
				nodePortGwLocalServiceToWorkloadNodesMap: map[host.Name]map[string]struct{}{},
			}
			// Copy because the function mutates
			svcCopied := tt.service.DeepCopy()
			c.nodePortGwLocalServiceToWorkloadNodesMap[svcCopied.Hostname] = map[string]struct{}{}
			for _, h := range tt.workloadHostingNodes {
				c.nodePortGwLocalServiceToWorkloadNodesMap[svcCopied.Hostname][h] = struct{}{}
			}

			c.updateClusterExternalAddressesForNodePortServices(tt.nodeLabelSelector, svcCopied)
			actualAddresses := svcCopied.Attributes.ClusterExternalAddresses[clusterID]
			sort.Strings(tt.expectedAddresses)
			sort.Strings(actualAddresses)
			if diff := cmp.Diff(actualAddresses, tt.expectedAddresses); diff != "" {
				t.Fatalf("unexpected external addresses set: diff = %s", diff)
			}
		})
	}
}
