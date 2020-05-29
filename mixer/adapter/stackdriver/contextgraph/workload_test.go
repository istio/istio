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

package contextgraph

import (
	"strings"
	"testing"
	"time"

	messagediff "gopkg.in/d4l3k/messagediff.v1"

	"istio.io/istio/mixer/pkg/adapter/test"
)

func TestWorkloadInstanceReify(t *testing.T) {
	wi := workloadInstance{
		meshUID:           "mesh/1",
		istioProject:      "org:project",
		clusterProject:    "org2:project2",
		clusterLocation:   "pangea",
		clusterName:       "global-mesh",
		uid:               "kubernetes://istio-system/istio-telemetry-65db5b46fc-r7qhq",
		owner:             "kubernetes://apis/apps/v1/namespaces/istio-system/deployments/istio-policy",
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
				"global-mesh/owners/kubernetes%3A%2F%2Fapis%2Fapps%2Fv1%2Fnamespaces%2Fistio-system%2Fdeployments%2Fistio-policy",
			"pangea",
			[4]string{
				"mesh%2F1",
				"org2:project2",
				"global-mesh",
				"kubernetes%3A%2F%2Fapis%2Fapps%2Fv1%2Fnamespaces%2Fistio-system%2Fdeployments%2Fistio-policy",
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
				"locations/pangea/clusters/global-mesh/owners/kubernetes%3A%2F%2Fapis%2Fapps%2Fv1%2Fnamespaces%2Fistio-system%2Fdeployments%2Fistio-policy",
			typeName: "google.cloud.contextgraph.Membership",
		}, {
			sourceFullName: "//istio.io/projects/org:project/meshes/mesh%2F1/clusterProjects/org2:project2/" +
				"locations/pangea/clusters/global-mesh/owners/kubernetes%3A%2F%2Fapis%2Fapps%2Fv1%2Fnamespaces%2Fistio-system%2Fdeployments%2Fistio-policy",
			destinationFullName: "//istio.io/projects/org:project/meshes/mesh%2F1/clusterProjects/org2:project2/" +
				"locations/pangea/clusters/global-mesh/workloadInstances/kubernetes%3A%2F%2Fistio-system%2Fistio-telemetry-65db5b46fc-r7qhq",
			typeName: "google.cloud.contextgraph.Membership",
		}, {
			sourceFullName: "//istio.io/projects/org:project/meshes/mesh%2F1/clusterProjects/org2:project2/" +
				"locations/pangea/clusters/global-mesh/owners/kubernetes%3A%2F%2Fapis%2Fapps%2Fv1%2Fnamespaces%2Fistio-system%2Fdeployments%2Fistio-policy",
			destinationFullName: "//container.googleapis.com/projects/org2:project2/locations/pangea/clusters/global-mesh/" +
				"k8s/namespaces/istio-system/apps/deployments/istio-policy",
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
		owner:             "kubernetes://apis/apps/v1/namespaces/istio-system/deployments/istio-policy",
		workloadName:      "istio-policy",
		workloadNamespace: "istio-system",
	}

	for _, test := range []struct {
		location, wantFullName string
	}{
		{"pangea",
			"//container.googleapis.com/projects/org2:project2/locations/pangea/" +
				"clusters/global-mesh/k8s/namespaces/istio-system/apps/deployments/istio-policy"},
		{"us-central1-a",
			"//container.googleapis.com/projects/org2:project2/zones/us-central1-a/" +
				"clusters/global-mesh/k8s/namespaces/istio-system/apps/deployments/istio-policy"},
		{"us-central1",
			"//container.googleapis.com/projects/org2:project2/locations/us-central1/" +
				"clusters/global-mesh/k8s/namespaces/istio-system/apps/deployments/istio-policy"},
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

func TestTrafficAssertionReify(t *testing.T) {

	cases := []struct {
		name         string
		assertion    trafficAssertion
		wantEdges    []edge
		wantEntities []entity
	}{
		{"http-service",
			trafficAssertion{
				source:             istioPolicyWorkloadInstance,
				destination:        istioTelemetryWorkloadInstance,
				contextProtocol:    "http",
				destinationService: svc,
				timestamp:          time.Now(),
			},
			[]edge{
				// policy memberships
				{policyWorkloadEntity.fullName, policyOwnerEntity.fullName, membershipTypeName},
				{policyOwnerEntity.fullName, policyWorkloadInstanceEntity.fullName, membershipTypeName},

				// policy -> k8s membership
				{policyOwnerEntity.fullName,
					"//container.googleapis.com/projects/org2:project2/locations//clusters/global-mesh/" +
						"k8s/namespaces/istio-system/apps/deployments/istio-policy",
					membershipTypeName},
				{policyWorkloadInstanceEntity.fullName,
					"//container.googleapis.com/projects/org2:project2/locations//clusters/global-mesh/" +
						"k8s/namespaces/istio-system/pods/istio-policy-65db5b46fc-r7qhq",
					membershipTypeName},

				// policy -> service comms
				{policyWorkloadInstanceEntity.fullName, svcEntity.fullName, httpComm},
				{policyOwnerEntity.fullName, svcEntity.fullName, httpComm},
				{policyWorkloadEntity.fullName, svcEntity.fullName, httpComm},

				// telemetry memberships
				{telemetryWorkloadEntity.fullName, telemetryOwnerEntity.fullName, membershipTypeName},
				{telemetryOwnerEntity.fullName, telemetryWorkloadInstanceEntity.fullName, membershipTypeName},

				// telemetry -> k8s membership
				{telemetryOwnerEntity.fullName,
					"//container.googleapis.com/projects/org2:project2/locations//clusters/global-mesh/" +
						"k8s/namespaces/istio-system/apps/deployments/istio-telemetry",
					membershipTypeName},
				{telemetryWorkloadInstanceEntity.fullName,
					"//container.googleapis.com/projects/org2:project2/locations//clusters/global-mesh/" +
						"k8s/namespaces/istio-system/pods/istio-telemetry-65db5b46fc-r7qhq",
					membershipTypeName},

				// policy sources -> telemetry workload instance
				{policyWorkloadInstanceEntity.fullName, telemetryWorkloadInstanceEntity.fullName, httpComm},
				{policyOwnerEntity.fullName, telemetryWorkloadInstanceEntity.fullName, httpComm},
				{policyWorkloadEntity.fullName, telemetryWorkloadInstanceEntity.fullName, httpComm},

				// service -> workload instance comms
				{svcEntity.fullName, telemetryWorkloadInstanceEntity.fullName, httpComm},

				// policy sources -> telemetry owner
				{policyWorkloadInstanceEntity.fullName, telemetryOwnerEntity.fullName, httpComm},
				{policyOwnerEntity.fullName, telemetryOwnerEntity.fullName, httpComm},
				{policyWorkloadEntity.fullName, telemetryOwnerEntity.fullName, httpComm},

				// service -> owner comms
				{svcEntity.fullName, telemetryOwnerEntity.fullName, httpComm},

				// policy sources -> telemetry workload
				{policyWorkloadInstanceEntity.fullName, telemetryWorkloadEntity.fullName, httpComm},
				{policyOwnerEntity.fullName, telemetryWorkloadEntity.fullName, httpComm},
				{policyWorkloadEntity.fullName, telemetryWorkloadEntity.fullName, httpComm},

				// service -> workload comms
				{svcEntity.fullName, telemetryWorkloadEntity.fullName, httpComm},

				// svc -> k8s svc membership
				{svcEntity.fullName,
					"//container.googleapis.com/projects/org2:project2/locations//clusters/global-mesh/k8s/namespaces/svc-ns/services/my-svc",
					membershipTypeName},
			},
			[]entity{
				policyWorkloadInstanceEntity,
				policyOwnerEntity,
				policyWorkloadEntity,
				telemetryWorkloadInstanceEntity,
				telemetryOwnerEntity,
				telemetryWorkloadEntity,
				svcEntity,
			},
		},
	}

	for _, v := range cases {
		t.Run(v.name, func(tt *testing.T) {
			gotEntities, gotEdges := v.assertion.Reify(test.NewEnv(tt))
			if diff, equal := messagediff.PrettyDiff(gotEntities, v.wantEntities); !equal {
				tt.Errorf("Reify() produced unexpected edges; diff: \n%s", diff)
				tt.Logf("Got: \n%#v\n", gotEntities)
				tt.Logf("Want: \n%#v\n", v.wantEntities)
			}
			if diff, equal := messagediff.PrettyDiff(gotEdges, v.wantEdges); !equal {
				tt.Errorf("Reify() produced unexpected edges; diff: \n%s", diff)
				tt.Logf("Got:\n%#v\n", gotEdges)
				tt.Logf("Want: \n#%v\n", v.wantEdges)
			}
		})
	}

}

var svc = service{
	istioProject: "org:project",
	namespace:    "svc-ns",
	name:         "my-svc",
	meshUID:      "mesh/1",
}

var istioPolicyWorkloadInstance = workloadInstance{
	meshUID:           "mesh/1",
	istioProject:      "org:project",
	clusterProject:    "org2:project2",
	clusterName:       "global-mesh",
	uid:               "kubernetes://istio-policy-65db5b46fc-r7qhq.istio-system",
	owner:             "kubernetes://apis/apps/v1/namespaces/istio-system/deployments/istio-policy",
	workloadName:      "istio-policy",
	workloadNamespace: "istio-system",
}

var istioTelemetryWorkloadInstance = workloadInstance{
	meshUID:           "mesh/1",
	istioProject:      "org:project",
	clusterProject:    "org2:project2",
	clusterName:       "global-mesh",
	uid:               "kubernetes://istio-telemetry-65db5b46fc-r7qhq.istio-system",
	owner:             "kubernetes://apis/apps/v1/namespaces/istio-system/deployments/istio-telemetry",
	workloadName:      "istio-telemetry",
	workloadNamespace: "istio-system",
}

var policyWorkloadInstanceEntity = entity{
	containerFullName: "//cloudresourcemanager.googleapis.com/projects/org:project",
	typeName:          "io.istio.WorkloadInstance",
	fullName: "//istio.io/projects/org:project/meshes/mesh%2F1/clusterProjects/org2:project2/locations//clusters/global-mesh/" +
		"workloadInstances/kubernetes%3A%2F%2Fistio-policy-65db5b46fc-r7qhq.istio-system",
	shortNames: [4]string{"mesh%2F1", "org2:project2", "global-mesh", "kubernetes%3A%2F%2Fistio-policy-65db5b46fc-r7qhq.istio-system"},
}

var policyOwnerEntity = entity{
	containerFullName: "//cloudresourcemanager.googleapis.com/projects/org:project",
	typeName:          "io.istio.Owner",
	fullName: "//istio.io/projects/org:project/meshes/mesh%2F1/clusterProjects/org2:project2/locations//clusters/global-mesh/" +
		"owners/kubernetes%3A%2F%2Fapis%2Fapps%2Fv1%2Fnamespaces%2Fistio-system%2Fdeployments%2Fistio-policy",
	shortNames: [4]string{"mesh%2F1", "org2:project2", "global-mesh",
		"kubernetes%3A%2F%2Fapis%2Fapps%2Fv1%2Fnamespaces%2Fistio-system%2Fdeployments%2Fistio-policy"},
}

var policyWorkloadEntity = entity{
	containerFullName: "//cloudresourcemanager.googleapis.com/projects/org:project",
	typeName:          "io.istio.Workload",
	fullName:          "//istio.io/projects/org:project/meshes/mesh%2F1/workloads/istio-system/istio-policy",
	location:          "global",
	shortNames:        [4]string{"mesh%2F1", "istio-system", "istio-policy", ""},
}

var telemetryWorkloadInstanceEntity = entity{
	containerFullName: "//cloudresourcemanager.googleapis.com/projects/org:project",
	typeName:          "io.istio.WorkloadInstance",
	fullName: "//istio.io/projects/org:project/meshes/mesh%2F1/clusterProjects/org2:project2/locations//clusters/global-mesh/" +
		"workloadInstances/kubernetes%3A%2F%2Fistio-telemetry-65db5b46fc-r7qhq.istio-system",
	shortNames: [4]string{"mesh%2F1", "org2:project2", "global-mesh", "kubernetes%3A%2F%2Fistio-telemetry-65db5b46fc-r7qhq.istio-system"},
}

var telemetryOwnerEntity = entity{
	containerFullName: "//cloudresourcemanager.googleapis.com/projects/org:project",
	typeName:          "io.istio.Owner",
	fullName: "//istio.io/projects/org:project/meshes/mesh%2F1/clusterProjects/org2:project2/locations//clusters/global-mesh/" +
		"owners/kubernetes%3A%2F%2Fapis%2Fapps%2Fv1%2Fnamespaces%2Fistio-system%2Fdeployments%2Fistio-telemetry",
	shortNames: [4]string{"mesh%2F1", "org2:project2", "global-mesh",
		"kubernetes%3A%2F%2Fapis%2Fapps%2Fv1%2Fnamespaces%2Fistio-system%2Fdeployments%2Fistio-telemetry"},
}

var telemetryWorkloadEntity = entity{
	containerFullName: "//cloudresourcemanager.googleapis.com/projects/org:project",
	typeName:          "io.istio.Workload",
	fullName:          "//istio.io/projects/org:project/meshes/mesh%2F1/workloads/istio-system/istio-telemetry",
	location:          "global",
	shortNames:        [4]string{"mesh%2F1", "istio-system", "istio-telemetry", ""},
}

var svcEntity = entity{
	containerFullName: "//cloudresourcemanager.googleapis.com/projects/org:project",
	typeName:          "io.istio.Service",
	fullName:          "//istio.io/projects/org:project/meshes/mesh%2F1/services/svc-ns/my-svc",
	location:          "global",
	shortNames:        [4]string{"mesh%2F1", "svc-ns", "my-svc", ""},
}
