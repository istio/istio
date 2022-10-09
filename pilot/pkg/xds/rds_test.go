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
package xds_test

import (
	"fmt"
	"testing"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
)

func TestRDS(t *testing.T) {
	tests := []struct {
		name   string
		node   string
		routes []string
	}{
		{
			"sidecar_new",
			sidecarID(app3Ip, "app3"),
			[]string{"80", "8080"},
		},
		{
			"gateway_new",
			gatewayID(gatewayIP),
			[]string{"http.80", "https.443.https.my-gateway.testns"},
		},
		{
			// Even if we get a bad route, we should still send Envoy an empty response, rather than
			// ignore it. If we ignore the route, the listeners can get stuck waiting forever.
			"sidecar_badroute",
			sidecarID(app3Ip, "app3"),
			[]string{"ht&p"},
		},
	}

	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ads := s.ConnectADS().WithType(v3.RouteType).WithID(tt.node)
			ads.RequestResponseAck(t, &discovery.DiscoveryRequest{ResourceNames: tt.routes})
		})
	}
}

const (
	app3Ip    = "10.2.0.1"
	gatewayIP = "10.3.0.1"
)

// Common code for the xds testing.
// The tests in this package use an in-process pilot using mock service registry and
// envoy.

func sidecarID(ip, deployment string) string { // nolint: unparam
	return fmt.Sprintf("sidecar~%s~%s-644fc65469-96dza.testns~testns.svc.cluster.local", ip, deployment)
}

func gatewayID(ip string) string { //nolint: unparam
	return fmt.Sprintf("router~%s~istio-gateway-644fc65469-96dzt.istio-system~istio-system.svc.cluster.local", ip)
}
