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

package core

import (
	"strings"
	"testing"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	sfs "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/set_filter_state/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
)

// A cross-network inner tunnel forwarded by the ambient east-west gateway carries the service
// hostname as its CONNECT authority. With the bridge enabled, an HBONE-capable sidecar's
// connect_terminate must store the matching workload address as the original destination, so
// main_internal selects the inbound chain for that port; nothing else changes. With the bridge
// disabled the default connect_authority filter is used.
func TestSidecarHBONEServiceAddressedConnect(t *testing.T) {
	test.SetForTest(t, &features.EnableSidecarHBONEListening, true)
	test.SetForTest(t, &features.EnableAmbientMultiNetwork, true)

	svc := buildServiceWithPort("echo.default.svc.cluster.local", 80, protocol.HTTP, tnow)
	inst := buildServiceInstance(svc, "1.1.1.1")
	inst.Endpoint.EndpointPort = 8080
	const mapping = "'echo.default.svc.cluster.local:80': '1.1.1.1:8080'"

	for _, enabled := range []bool{false, true} {
		name := "bridge disabled"
		if enabled {
			name = "bridge enabled"
		}
		t.Run(name, func(t *testing.T) {
			test.SetForTest(t, &features.EnableSidecarAmbientBridge, enabled)
			cg := NewConfigGenTest(t, TestOptions{
				Services:  []*model.Service{svc},
				Instances: []*model.ServiceInstance{inst},
			})
			p := getProxy()
			p.Metadata.EnableHBONE = true
			p.IstioVersion = &model.IstioVersion{Major: 1, Minor: 30}
			proxy := cg.SetupProxy(p)

			var terminate, internal string
			for _, l := range cg.Listeners(proxy) {
				switch l.Name {
				case ConnectTerminate:
					terminate = connectAuthorityOriginalDst(t, l)
				case MainInternalName:
					internal = l.String()
				}
			}
			if terminate == "" || internal == "" {
				t.Fatalf("missing %s or %s listener", ConnectTerminate, MainInternalName)
			}
			if got := strings.Contains(terminate, mapping); got != enabled {
				t.Errorf("connect_terminate maps the service authority to the workload address = %v, want %v", got, enabled)
			}
			if got := strings.Contains(terminate, "%CEL(request.host in"); got != enabled {
				t.Errorf("connect_terminate uses the CEL authority mapping = %v, want %v", got, enabled)
			}
			// main_internal is untouched: the restored destination selects the usual chain for 8080.
			if !strings.Contains(internal, "destination_port:{value:8080}") {
				t.Errorf("main_internal should keep its chain for port 8080")
			}
		})
	}
}

// connectAuthorityOriginalDst returns the format string the connect_authority filter uses for the
// original destination on the connect_terminate listener.
func connectAuthorityOriginalDst(t *testing.T, l *listener.Listener) string {
	t.Helper()
	for _, fc := range l.FilterChains {
		for _, f := range fc.Filters {
			h := &hcm.HttpConnectionManager{}
			if f.GetTypedConfig().UnmarshalTo(h) != nil {
				continue
			}
			for _, hf := range h.HttpFilters {
				if hf.Name != xdsfilters.ConnectAuthorityFilter.Name {
					continue
				}
				cfg := &sfs.Config{}
				if err := hf.GetTypedConfig().UnmarshalTo(cfg); err != nil {
					t.Fatal(err)
				}
				for _, v := range cfg.OnRequestHeaders {
					if v.GetObjectKey() == xdsfilters.OriginalDstFilterStateKey {
						return v.GetFormatString().GetTextFormatSource().GetInlineString()
					}
				}
			}
		}
	}
	t.Fatalf("no connect_authority filter on %s", l.Name)
	return ""
}
