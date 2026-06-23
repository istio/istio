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

package agentgateway

import (
	"sort"
	"testing"

	"github.com/agentgateway/agentgateway/api"
	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pkg/config/constants"
)

// agwBindListener builds a minimal GatewayListener with only the fields
// buildBindsFromGateway reads: the parent gateway identity and the parent
// listener info (port, protocol, gateway class, and any conflict).
func agwBindListener(
	gwName string,
	port gatewayv1.PortNumber,
	proto gatewayv1.ProtocolType,
	className string,
	conflict ListenerConflict,
) *GatewayListener {
	pg := types.NamespacedName{Namespace: "default", Name: gwName}
	return &GatewayListener{
		ParentGateway: pg,
		ParentInfo: AgwParentInfo{
			Port:                   port,
			Protocol:               proto,
			ParentGatewayClassName: className,
		},
		Conflict: conflict,
	}
}

// bindResult is a flattened, comparable view of a generated bind resource.
type bindResult struct {
	key      string
	gateway  string
	port     uint32
	protocol api.Bind_Protocol
	tunnel   api.Bind_TunnelProtocol
}

func sortBinds(binds []bindResult) []bindResult {
	sort.Slice(binds, func(i, j int) bool { return binds[i].key < binds[j].key })
	return binds
}

// TestBuildBindsFromGateway verifies bind generation, with an emphasis on the
// fact that different gateways may share a port as long as each gateway gets its
// own bind keyed by "port/namespace/name". This means two gateways can bind the
// same port with different protocols or different tunnel protocols.
func TestBuildBindsFromGateway(t *testing.T) {
	c := &Controller{}
	tests := []struct {
		name string
		// each entry is the set of listeners belonging to a single parent
		// gateway; buildBindsFromGateway is called once per group, mirroring how
		// the controller groups listeners by parent gateway.
		gateways [][]*GatewayListener
		want     []bindResult
	}{
		{
			name:     "no listeners produces no binds",
			gateways: [][]*GatewayListener{{}},
			want:     nil,
		},
		{
			name: "different gateways share a port with different protocols",
			gateways: [][]*GatewayListener{
				{agwBindListener("gateway-http", 8080, gatewayv1.HTTPProtocolType, constants.AgentgatewayClassName, "")},
				{agwBindListener("gateway-https", 8080, gatewayv1.HTTPSProtocolType, constants.AgentgatewayClassName, "")},
			},
			want: []bindResult{
				{key: "8080/default/gateway-http", gateway: "default/gateway-http", port: 8080, protocol: api.Bind_HTTP, tunnel: api.Bind_DIRECT},
				{key: "8080/default/gateway-https", gateway: "default/gateway-https", port: 8080, protocol: api.Bind_TLS, tunnel: api.Bind_DIRECT},
			},
		},
		{
			name: "different gateways share a port with different tunnel protocols",
			gateways: [][]*GatewayListener{
				{agwBindListener("waypoint", 15008, hboneProtocol, constants.AgentgatewayWaypointClassName, "")},
				{agwBindListener("gateway", 15008, hboneProtocol, constants.AgentgatewayClassName, "")},
			},
			want: []bindResult{
				{key: "15008/default/waypoint", gateway: "default/waypoint", port: 15008, protocol: api.Bind_HTTP, tunnel: api.Bind_HBONE_WAYPOINT},
				{key: "15008/default/gateway", gateway: "default/gateway", port: 15008, protocol: api.Bind_HTTP, tunnel: api.Bind_HBONE_GATEWAY},
			},
		},
		{
			name: "single gateway multiple non-conflicting listeners on the same port collapse to one bind",
			gateways: [][]*GatewayListener{
				{
					agwBindListener("gw", 8080, gatewayv1.HTTPProtocolType, constants.AgentgatewayClassName, ""),
					agwBindListener("gw", 8080, gatewayv1.HTTPProtocolType, constants.AgentgatewayClassName, ""),
					agwBindListener("gw", 443, gatewayv1.HTTPSProtocolType, constants.AgentgatewayClassName, ""),
				},
			},
			want: []bindResult{
				{key: "8080/default/gw", gateway: "default/gw", port: 8080, protocol: api.Bind_HTTP, tunnel: api.Bind_DIRECT},
				{key: "443/default/gw", gateway: "default/gw", port: 443, protocol: api.Bind_TLS, tunnel: api.Bind_DIRECT},
			},
		},
		{
			name: "conflicting listener does not determine the bind protocol",
			gateways: [][]*GatewayListener{
				{
					agwBindListener("gw", 8080, gatewayv1.HTTPProtocolType, constants.AgentgatewayClassName, ""),
					agwBindListener("gw", 8080, gatewayv1.HTTPSProtocolType, constants.AgentgatewayClassName, ListenerConflictProtocol),
				},
			},
			want: []bindResult{
				{key: "8080/default/gw", gateway: "default/gw", port: 8080, protocol: api.Bind_HTTP, tunnel: api.Bind_DIRECT},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []bindResult
			for _, listeners := range tt.gateways {
				for _, res := range c.buildBindsFromGateway(listeners) {
					b := res.Resource.GetBind()
					got = append(got, bindResult{
						key:      b.GetKey(),
						gateway:  res.Gateway.String(),
						port:     b.GetPort(),
						protocol: b.GetProtocol(),
						tunnel:   b.GetTunnelProtocol(),
					})
				}
			}
			if diff := cmp.Diff(sortBinds(got), sortBinds(tt.want), cmp.AllowUnexported(bindResult{})); diff != "" {
				t.Fatalf("unexpected binds (-got +want):\n%s", diff)
			}
		})
	}
}
