// Copyright 2019 Istio Authors
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

package v2

import (
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schemas"
)

func TestProxyNeedsPush(t *testing.T) {
	sidecar := &model.Proxy{Type: model.SidecarProxy, IPAddresses: []string{"127.0.0.1"}}
	gateway := &model.Proxy{Type: model.Router}
	cases := []struct {
		name       string
		proxy      *model.Proxy
		namespaces []string
		proxies    []string
		configs    []string
		want       bool
	}{
		{"no namespace or configs", sidecar, nil, nil, nil, true},
		{"gateway config for sidecar", sidecar, nil, nil, []string{schemas.Gateway.Type}, false},
		{"gateway config for gateway", gateway, nil, nil, []string{schemas.Gateway.Type}, true},
		{"proxy in targetProxies", sidecar, nil, []string{"127.0.0.1"}, nil, true},
		{"proxy not in targetProxies", sidecar, nil, []string{"127.0.0.2"}, nil, false},
		{"proxy in targetProxies and gateway config", sidecar, nil, []string{"127.0.0.1"}, []string{schemas.Gateway.Type}, true},

		// TODO: add test for namespaces
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ns := map[string]struct{}{}
			for _, n := range tt.namespaces {
				ns[n] = struct{}{}
			}
			proxies := map[string]struct{}{}
			for _, id := range tt.proxies {
				proxies[id] = struct{}{}
			}
			cfgs := map[string]struct{}{}
			for _, c := range tt.configs {
				cfgs[c] = struct{}{}
			}
			pushEv := &XdsEvent{targetNamespaces: ns, targetProxies: proxies, configTypesUpdated: cfgs}
			got := ProxyNeedsPush(tt.proxy, pushEv)
			if got != tt.want {
				t.Fatalf("Got needs push = %v, expected %v", got, tt.want)
			}
		})
	}
}
