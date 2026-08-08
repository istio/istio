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

package ambient

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/api/label"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/workloadapi"
)

func TestGatewayLocality(t *testing.T) {
	cases := []struct {
		name   string
		labels map[string]string
		want   *workloadapi.Locality
	}{
		{
			name:   "no locality label",
			labels: map[string]string{"topology.istio.io/network": "network-1"},
			want:   nil,
		},
		{
			name: "topology.istio.io/locality label, all three levels",
			labels: map[string]string{
				label.TopologyLocality.Name: "region1.zone1.subzone1",
			},
			want: &workloadapi.Locality{Region: "region1", Zone: "zone1", Subzone: "subzone1"},
		},
		{
			name: "topology.istio.io/locality label, region only",
			labels: map[string]string{
				label.TopologyLocality.Name: "region1",
			},
			want: &workloadapi.Locality{Region: "region1"},
		},
		{
			name: "legacy istio-locality label",
			labels: map[string]string{
				"istio-locality": "region1.zone1",
			},
			want: &workloadapi.Locality{Region: "region1", Zone: "zone1"},
		},
		{
			name: "topology.istio.io/locality takes precedence over legacy label",
			labels: map[string]string{
				label.TopologyLocality.Name: "region1.zone1",
				"istio-locality":            "region2.zone2",
			},
			want: &workloadapi.Locality{Region: "region1", Zone: "zone1"},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			gw := &gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "eastwestgateway",
					Namespace: testNS,
					Labels:    tt.labels,
				},
			}
			assert.Equal(t, gatewayLocality(gw), tt.want)
		})
	}
}
