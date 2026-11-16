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

package util

import (
	"testing"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

	"istio.io/istio/pkg/test/util/assert"
)

func nodeWithExtensions(names ...string) *core.Node {
	node := &core.Node{}
	for _, name := range names {
		node.Extensions = append(node.Extensions, &core.Extension{Name: name})
	}
	return node
}

func TestNodeSupportsHBONEConnectWait(t *testing.T) {
	cases := []struct {
		name string
		node *core.Node
		want bool
	}{
		{name: "nil node", node: nil},
		{name: "no extensions", node: nodeWithExtensions()},
		{name: "unrelated extensions", node: nodeWithExtensions("envoy.transport_sockets.tls", "envoy.upstreams.tcp.generic")},
		{name: "transport only", node: nodeWithExtensions(HBONEOriginationAdapterTransport)},
		{name: "pool only", node: nodeWithExtensions(HBONERejectionPool)},
		{
			name: "both",
			node: nodeWithExtensions("envoy.transport_sockets.tls", HBONEOriginationAdapterTransport, HBONERejectionPool),
			want: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, NodeSupportsHBONEConnectWait(tt.node), tt.want)
		})
	}
}
