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

package internal

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/pkg/mcp/internal/test"
)

func TestUpdateResourceVersionTracking(t *testing.T) {
	var (
		r0 = test.Type0A[0].Resource
		r1 = test.Type0B[0].Resource
		r2 = test.Type0C[0].Resource

		r0Updated = test.Type0A[1].Resource
		r1Updated = test.Type0B[1].Resource
		r2Updated = test.Type0C[1].Resource
	)

	cases := []struct {
		name      string
		current   map[string]string
		want      map[string]string
		resources *mcp.Resources
	}{
		{
			name:    "add initial state",
			current: map[string]string{},
			want: map[string]string{
				r0.Metadata.Name: r0.Metadata.Version,
				r1.Metadata.Name: r1.Metadata.Version,
				r2.Metadata.Name: r2.Metadata.Version,
			},
			resources: &mcp.Resources{Resources: []mcp.Resource{*r0, *r1, *r2}},
		},
		{
			name: "remove everything",
			current: map[string]string{
				r0.Metadata.Name: r0.Metadata.Version,
				r1.Metadata.Name: r1.Metadata.Version,
				r2.Metadata.Name: r2.Metadata.Version,
			},
			want:      map[string]string{},
			resources: &mcp.Resources{Resources: []mcp.Resource{}},
		},
		{
			name: "replace everything",
			current: map[string]string{
				r0.Metadata.Name: r0.Metadata.Version,
				r1.Metadata.Name: r1.Metadata.Version,
				r2.Metadata.Name: r2.Metadata.Version,
			},
			want: map[string]string{
				r0Updated.Metadata.Name: r0Updated.Metadata.Version,
				r1Updated.Metadata.Name: r1Updated.Metadata.Version,
				r2Updated.Metadata.Name: r2Updated.Metadata.Version,
			},
			resources: &mcp.Resources{Resources: []mcp.Resource{*r0Updated, *r1Updated, *r2Updated}},
		},
		{
			name:    "add incrementally",
			current: map[string]string{},
			want: map[string]string{
				r0.Metadata.Name: r0.Metadata.Version,
				r1.Metadata.Name: r1.Metadata.Version,
				r2.Metadata.Name: r2.Metadata.Version,
			},
			resources: &mcp.Resources{
				Incremental: true,
				Resources:   []mcp.Resource{*r0, *r1, *r2},
			},
		},
		{
			name: "update incrementally",
			current: map[string]string{
				r0.Metadata.Name: r0.Metadata.Version,
				r1.Metadata.Name: r1.Metadata.Version,
				r2.Metadata.Name: r2.Metadata.Version,
			},
			want: map[string]string{
				r0Updated.Metadata.Name: r0Updated.Metadata.Version,
				r1.Metadata.Name:        r1.Metadata.Version,
				r2.Metadata.Name:        r2.Metadata.Version,
			},
			resources: &mcp.Resources{
				Incremental: true,
				Resources:   []mcp.Resource{*r0Updated},
			},
		},
		{
			name: "delete incrementally",
			current: map[string]string{
				r0.Metadata.Name: r0.Metadata.Version,
				r1.Metadata.Name: r1.Metadata.Version,
				r2.Metadata.Name: r2.Metadata.Version,
			},
			want: map[string]string{
				r1.Metadata.Name: r1.Metadata.Version,
				r2.Metadata.Name: r2.Metadata.Version,
			},
			resources: &mcp.Resources{
				Incremental:      true,
				Resources:        []mcp.Resource{},
				RemovedResources: []string{r0Updated.Metadata.Name},
			},
		},
		{
			name: "add, update, and delete incrementally",
			current: map[string]string{
				r0.Metadata.Name: r0.Metadata.Version,
				r1.Metadata.Name: r1.Metadata.Version,
			},
			want: map[string]string{
				r1Updated.Metadata.Name: r1Updated.Metadata.Version,
				r2.Metadata.Name:        r2.Metadata.Version,
			},
			resources: &mcp.Resources{
				Incremental:      true,
				Resources:        []mcp.Resource{*r1Updated, *r2},
				RemovedResources: []string{r0Updated.Metadata.Name},
			},
		},
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("[%v] %v", i, c.name), func(tt *testing.T) {
			UpdateResourceVersionTracking(c.current, c.resources)
			got := c.current // map is modified in place
			if diff := cmp.Diff(got, c.want); diff != "" {
				tt.Fatalf("wrong versions: \n got %v \nwant %v\ndiff %v", got, c.want, diff)
			}
		})
	}
}
