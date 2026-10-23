// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ambient

import (
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/workloadapi"
)

func TestMergeServiceInfosDoesNotModifyInputs(t *testing.T) {
	input := &model.ServiceInfo{
		Service: &workloadapi.Service{
			Name:            "svc",
			Namespace:       "ns",
			Hostname:        "svc.ns.svc.cluster.local",
			Addresses:       []*workloadapi.NetworkAddress{{Network: "network-1", Address: []byte{1, 2, 3, 4}}},
			SubjectAltNames: []string{"original"},
		},
		Scope: model.Global,
	}
	other := &model.ServiceInfo{
		Service: &workloadapi.Service{
			Name:            "svc",
			Namespace:       "ns",
			Hostname:        "svc.ns.svc.cluster.local",
			Addresses:       []*workloadapi.NetworkAddress{{Network: "network-2", Address: []byte{5, 6, 7, 8}}},
			SubjectAltNames: []string{"other"},
		},
		Scope: model.Global,
	}

	merged := mergeServiceInfosWithCluster("local")([]krt.ObjectWithCluster[model.ServiceInfo]{
		{ClusterID: cluster.ID("local"), Object: input},
		{ClusterID: cluster.ID("remote"), Object: other},
	})

	assert.Equal(t, input.Service.Addresses, []*workloadapi.NetworkAddress{{Network: "network-1", Address: []byte{1, 2, 3, 4}}})
	assert.Equal(t, input.Service.SubjectAltNames, []string{"original"})
	assert.Equal(t, input.MarshaledAddress, nil)
	if merged.Object == input || merged.Object.Service == input.Service {
		t.Fatal("merge must create a new ServiceInfo and Service")
	}
}

func TestWorkloadPointerIdentity(t *testing.T) {
	input := &model.WorkloadInfo{Workload: &workloadapi.Workload{Uid: "workload"}}
	if got := precomputeWorkload(input); got != input {
		t.Fatal("precomputeWorkload must retain its newly allocated input")
	}

	wrapped := wrapPointerObjectWithCluster[model.WorkloadInfo]("local")(input)
	if wrapped.Object != input {
		t.Fatal("workload wrapper must retain the producer pointer")
	}

	merged := mergeWorkloadInfosWithCluster("local")([]krt.ObjectWithCluster[model.WorkloadInfo]{wrapped})
	if merged.Object != input {
		t.Fatal("workload merge must retain the selected producer pointer")
	}
}
