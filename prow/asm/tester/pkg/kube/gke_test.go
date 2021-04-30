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

package kube

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGKEClusterSpecsFromContexts(t *testing.T) {
	ctx1 := "gke_project1_us-central1_cluster1"
	cluster1 := GKEClusterSpec{
		Name:      "cluster1",
		ProjectID: "project1",
		Location:  "us-central1",
	}
	ctx2 := "gke_project2_us-central1_cluster2"
	cluster2 := GKEClusterSpec{
		Name:      "cluster2",
		ProjectID: "project2",
		Location:  "us-central1",
	}

	tcs := []struct {
		name            string
		kubectlContexts string
		want            []*GKEClusterSpec
	}{
		{
			"empty context string returns an empty array",
			"",
			[]*GKEClusterSpec{},
		},
		{
			"non GKE context string returns an empty array",
			"gke-on-prem_context1,gke-on-prem_context2",
			[]*GKEClusterSpec{},
		},
		{
			"one GKE cluster context string returns an array of size 1",
			ctx1,
			[]*GKEClusterSpec{&cluster1},
		},
		{
			"two GKE clusters context string returns an array of size 1",
			ctx1 + "," + ctx2,
			[]*GKEClusterSpec{&cluster1, &cluster2},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(st *testing.T) {
			got := GKEClusterSpecsFromContexts(tc.kubectlContexts)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("got(+) is different from wanted(-)\n%v", diff)
			}
		})
	}
}
