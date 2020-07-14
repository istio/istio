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
	"reflect"
	"testing"
)

func TestGetLocalityFromTopology(t *testing.T) {
	cases := []struct {
		name     string
		topology map[string]string
		locality string
	}{
		{
			"all standard kubernetes labels",
			map[string]string{
				NodeRegionLabelGA: "region",
				NodeZoneLabelGA:   "zone",
			},
			"region/zone",
		},
		{
			"all standard kubernetes labels and Istio custom labels",
			map[string]string{
				NodeRegionLabelGA: "region",
				NodeZoneLabelGA:   "zone",
				IstioSubzoneLabel: "subzone",
			},
			"region/zone/subzone",
		},
		{
			"missing zone",
			map[string]string{
				NodeRegionLabelGA: "region",
			},
			"region",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := getLocalityFromTopology(tt.topology)
			if !reflect.DeepEqual(tt.locality, got) {
				t.Fatalf("Expected %v, got %v", tt.topology, got)
			}
		})
	}
}
