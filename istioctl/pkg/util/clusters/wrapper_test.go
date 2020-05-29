// Copyright 2020 Istio Authors
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

package clusters

import (
	"reflect"
	"testing"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
)

func TestMarshalJSON(t *testing.T) {
	tests := []struct {
		desc     string
		in       *Wrapper
		expected string
		err      error
	}{
		{
			desc:     "no-status",
			in:       &Wrapper{},
			expected: `{}`,
			err:      nil,
		},
		{
			desc: "valid-cluster-status",
			in: &Wrapper{
				Clusters: &adminapi.Clusters{
					ClusterStatuses: []*adminapi.ClusterStatus{{
							Name:        "foo",
							AddedViaApi: false,
						},
					},
				},
			},
			expected: `{"Clusters":{"clusterStatuses":[{"name":"foo"}]}}`,
			err:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, err := tt.in.MarshalJSON(); string(got) != tt.expected || ((err != nil && tt.err == nil) || (err == nil && tt.err != nil)) {
				t.Errorf("%s: expected & err %v %v got %v %v", tt.desc, tt.expected, tt.err, got, err)
			}
		})
	}
}

func TestUnmarshalJSON(t *testing.T) {
	tests := []struct {
		desc     string
		wrapper  *Wrapper
		inBytes  []byte
		expected *Wrapper
		err      error
	}{
		{
			desc:    "set-wrappers",
			wrapper: &Wrapper{},
			inBytes: []byte(`{"clusterStatuses":[{"name":"foo"}]}`),
			expected: &Wrapper{
				Clusters: &adminapi.Clusters{
					ClusterStatuses: []*adminapi.ClusterStatus{{
							Name:        "foo",
							AddedViaApi: false,
						},
					},
				},
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			err := tt.wrapper.UnmarshalJSON(tt.inBytes)
			if !reflect.DeepEqual(*tt.expected, *tt.wrapper) || ((tt.err != nil && err == nil) || (tt.err == nil && err != nil)) {
				t.Errorf("%s: expected %v %v got %v %v", tt.desc, tt.expected, tt.err, tt.wrapper, err)
			}
		})
	}
}
