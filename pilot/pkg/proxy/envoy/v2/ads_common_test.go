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
	"strconv"
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
		configs    []string
		want       bool
	}{
		{"no namespace or configs", sidecar, nil, nil, true},
		{"gateway config for sidecar", sidecar, nil, []string{schemas.Gateway.Type}, false},
		{"gateway config for gateway", gateway, nil, []string{schemas.Gateway.Type}, true},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ns := map[string]struct{}{}
			for _, n := range tt.namespaces {
				ns[n] = struct{}{}
			}
			cfgs := map[string]struct{}{}
			for _, c := range tt.configs {
				cfgs[c] = struct{}{}
			}
			pushEv := &XdsEvent{namespacesUpdated: ns, configTypesUpdated: cfgs}
			got := ProxyNeedsPush(tt.proxy, pushEv)
			if got != tt.want {
				t.Fatalf("Got needs push = %v, expected %v", got, tt.want)
			}
		})
	}
}

func BenchmarkListEquals(b *testing.B) {
	size := 100
	var l []string
	for i := 0; i < size; i++ {
		l = append(l, strconv.Itoa(i))
	}
	var equal []string
	for i := 0; i < size; i++ {
		equal = append(equal, strconv.Itoa(i))
	}
	var notEqual []string
	for i := 0; i < size; i++ {
		notEqual = append(notEqual, strconv.Itoa(i))
	}
	notEqual[size-1] = "z"

	for n := 0; n < b.N; n++ {
		listEqualUnordered(l, equal)
		listEqualUnordered(l, notEqual)
	}
}
