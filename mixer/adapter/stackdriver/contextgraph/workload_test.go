// Copyright 2017 Istio Authors
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

package contextgraph

import (
	"strings"
	"testing"
)

func TestWorkloadInstanceReify(t *testing.T) {
	wi := workloadInstance{
		meshUID:           "mesh/1",
		istioProject:      "org:project",
		clusterProject:    "org2:project2",
		clusterLocation:   "pangea",
		clusterName:       "global-mesh",
		uid:               "kubernetes://istio-system/istio-telemetry-65db5b46fc-r7qhq",
		owner:             "kubernetes://apis/extensions/v1beta1/namespaces/istio-system/deployments/istio-policy",
		workloadName:      "istio-policy",
		workloadNamespace: "istio-system",
	}

	entities, edges := wi.Reify(mockLogger{})
	wantEntities := []entity{
		{
			"//cloudresourcemanager.googleapis.com/projects/org:project",
			"io.istio.WorkloadInstance",
			"//istio.io/projects/org:project/meshes/mesh%2F1/clusterProjects/org2:project2/locations/pangea/clusters/" +
				"global-mesh/workloadInstances/kubernetes%3A%2F%2Fistio-system%2Fistio-telemetry-65db5b46fc-r7qhq",
			"pangea",
			[4]string{"mesh%2F1", "org2:project2", "global-mesh", "kubernetes%3A%2F%2Fistio-system%2Fistio-telemetry-65db5b46fc-r7qhq"},
		},
		{
			"//cloudresourcemanager.googleapis.com/projects/org:project",
			"io.istio.Owner",
			"//istio.io/projects/org:project/meshes/mesh%2F1/clusterProjects/org2:project2/locations/pangea/clusters/" +
				"global-mesh/owners/kubernetes%3A%2F%2Fapis%2Fextensions%2Fv1beta1%2Fnamespaces%2Fistio-system%2Fdeployments%2Fistio-policy",
			"pangea",
			[4]string{
				"mesh%2F1",
				"org2:project2",
				"global-mesh",
				"kubernetes%3A%2F%2Fapis%2Fextensions%2Fv1beta1%2Fnamespaces%2Fistio-system%2Fdeployments%2Fistio-policy",
			},
		},
		{
			"//cloudresourcemanager.googleapis.com/projects/org:project",
			"io.istio.Workload",
			"//istio.io/projects/org:project/meshes/mesh%2F1/workloads/istio-system/istio-policy",
			"global",
			[4]string{"mesh%2F1", "istio-system", "istio-policy", ""},
		},
	}

	if got, want := len(entities), len(wantEntities); got != want {
		t.Errorf("got %d entities, want %d", got, want)
	}
	for i, e := range entities {
		if i < len(wantEntities) && e != wantEntities[i] {
			t.Errorf("entity %d = %#v, want %#v", i, e, wantEntities[i])
		}
	}
	wantEdges := []edge{
		{
			sourceFullName: "//istio.io/projects/org:project/meshes/mesh%2F1/workloads/istio-system/istio-policy",
			destinationFullName: "//istio.io/projects/org:project/meshes/mesh%2F1/clusterProjects/org2:project2/" +
				"locations/pangea/clusters/global-mesh/owners/kubernetes%3A%2F%2Fapis%2Fextensions%2Fv1beta1%2Fnamespaces%2Fistio-system%2Fdeployments%2Fistio-policy",
			typeName: "google.cloud.contextgraph.Membership",
		}, {
			sourceFullName: "//istio.io/projects/org:project/meshes/mesh%2F1/clusterProjects/org2:project2/" +
				"locations/pangea/clusters/global-mesh/owners/kubernetes%3A%2F%2Fapis%2Fextensions%2Fv1beta1%2Fnamespaces%2Fistio-system%2Fdeployments%2Fistio-policy",
			destinationFullName: "//istio.io/projects/org:project/meshes/mesh%2F1/clusterProjects/org2:project2/" +
				"locations/pangea/clusters/global-mesh/workloadInstances/kubernetes%3A%2F%2Fistio-system%2Fistio-telemetry-65db5b46fc-r7qhq",
			typeName: "google.cloud.contextgraph.Membership",
		}, {
			sourceFullName: "//istio.io/projects/org:project/meshes/mesh%2F1/clusterProjects/org2:project2/" +
				"locations/pangea/clusters/global-mesh/owners/kubernetes%3A%2F%2Fapis%2Fextensions%2Fv1beta1%2Fnamespaces%2Fistio-system%2Fdeployments%2Fistio-policy",
			destinationFullName: "//container.googleapis.com/projects/org2:project2/locations/pangea/clusters/global-mesh/" +
				"k8s/namespaces/istio-system/extensions/deployments/istio-policy",
			typeName: "google.cloud.contextgraph.Membership",
		},
	}
	if got, want := len(edges), len(wantEdges); got != want {
		t.Errorf("got %d edges, want %d", got, want)
	}
	for i, e := range edges {
		if i < len(wantEdges) && e != wantEdges[i] {
			t.Errorf("edge %d = %#v, want %#v", i, e, wantEdges[i])
		}
	}
}

func TestWorkloadInstanceClusterLocation(t *testing.T) {
	wi := workloadInstance{
		meshUID:           "mesh/1",
		istioProject:      "org:project",
		clusterProject:    "org2:project2",
		clusterName:       "global-mesh",
		uid:               "kubernetes://istio-system/istio-telemetry-65db5b46fc-r7qhq",
		owner:             "kubernetes://apis/extensions/v1beta1/namespaces/istio-system/deployments/istio-policy",
		workloadName:      "istio-policy",
		workloadNamespace: "istio-system",
	}

	for _, test := range []struct {
		location, wantFullName string
	}{
		{"pangea",
			"//container.googleapis.com/projects/org2:project2/locations/pangea/" +
				"clusters/global-mesh/k8s/namespaces/istio-system/extensions/deployments/istio-policy"},
		{"us-central1-a",
			"//container.googleapis.com/projects/org2:project2/zones/us-central1-a/" +
				"clusters/global-mesh/k8s/namespaces/istio-system/extensions/deployments/istio-policy"},
		{"us-central1",
			"//container.googleapis.com/projects/org2:project2/locations/us-central1/" +
				"clusters/global-mesh/k8s/namespaces/istio-system/extensions/deployments/istio-policy"},
	} {
		wi.clusterLocation = test.location
		_, edges := wi.Reify(mockLogger{})
		var fullName string
		for _, e := range edges {
			if strings.HasPrefix(e.destinationFullName, "//container.googleapis.com/") {
				if fullName != "" {
					t.Errorf("Found two container destinations: %q and %q", fullName, e.destinationFullName)
				}
				fullName = e.destinationFullName
			}
		}
		if fullName != test.wantFullName {
			t.Errorf("for location %q, got %q, want %q", test.location, fullName, test.wantFullName)
		}
	}
}
