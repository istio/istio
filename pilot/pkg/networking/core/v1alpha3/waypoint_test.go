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

package v1alpha3

import (
	"reflect"
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/workloadapi"
)

func TestFindWorkloadServices(t *testing.T) {
	namespace := "ns"
	svcA := &model.Service{
		Attributes: model.ServiceAttributes{
			Namespace:      namespace,
			Name:           "a",
			LabelSelectors: map[string]string{"app": "a"},
		},
		Hostname: "a.com",
	}
	svcFoo := &model.Service{
		Attributes: model.ServiceAttributes{
			Namespace: namespace,
			Name:      "foo",
		},
		Hostname: "foo.com",
	}

	a := &model.WorkloadInfo{
		Workload: &workloadapi.Workload{
			Namespace: namespace,
			Name:      "a",
		},
		Labels: map[string]string{"app": "a"},
	}
	b := &model.WorkloadInfo{
		Workload: &workloadapi.Workload{
			Namespace: namespace,
			Name:      "b",
		},
		Labels: map[string]string{"app": "b"},
	}
	foo := &model.WorkloadInfo{
		Workload: &workloadapi.Workload{
			Namespace: namespace,
			Name:      "foo",
		},
	}

	tests := []struct {
		name      string
		workloads []*model.WorkloadInfo
		want      map[host.Name]*model.Service
	}{
		{
			name:      "no workloads",
			workloads: []*model.WorkloadInfo{},
			want:      map[host.Name]*model.Service{},
		},
		{
			name:      "not matched",
			workloads: []*model.WorkloadInfo{b, foo},
			want:      map[host.Name]*model.Service{},
		},
		{
			name:      "matched",
			workloads: []*model.WorkloadInfo{a, b, foo},
			want:      map[host.Name]*model.Service{svcA.Hostname: svcA},
		},
	}
	for _, tt := range tests {
		cg := NewConfigGenTest(t, TestOptions{
			Services: []*model.Service{svcA, svcFoo},
		})
		t.Run(tt.name, func(t *testing.T) {
			if got := findWorkloadServices(tt.workloads, cg.PushContext()); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("findWorkloadServices: got %v, want %v", got, tt.want)
			}
		})
	}
}
