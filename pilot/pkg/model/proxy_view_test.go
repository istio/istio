// Copyright Istio Authors. All Rights Reserved.
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

package model_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/network"
)

func TestProxyView(t *testing.T) {
	cases := []struct {
		name        string
		networkView []string
		network     string
		visible     bool
	}{
		{
			name:    "no views",
			network: "network1",
			visible: true,
		},
		{
			name:        "network visible",
			networkView: []string{"network1"},
			network:     "network1",
			visible:     true,
		},
		{
			name:        "network not visible",
			networkView: []string{"network1"},
			network:     "network2",
			visible:     false,
		},
		{
			name:        "no network label",
			networkView: []string{"network1"},
			network:     "",
			visible:     true,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			g := NewWithT(t)

			view := (&model.Proxy{
				Metadata: &model.NodeMetadata{
					RequestedNetworkView: c.networkView,
				},
			}).GetView()

			actual := view.IsVisible(&model.IstioEndpoint{
				Network: network.ID(c.network),
			})

			g.Expect(actual).To(Equal(c.visible))
		})
	}
}
