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

package capture

import (
	"net/netip"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	testutil "istio.io/istio/pilot/test/util"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

func TestIptables(t *testing.T) {
	for _, tt := range GetCommonTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ConstructTestConfig()
			tt.config(cfg)

			ext := &dep.DependenciesStub{}
			iptConfigurator, _ := NewIptablesConfigurator(cfg, ext)
			iptConfigurator.Run()
			compareToGolden(t, tt.name, ext.ExecutedAll)
		})
	}
}

func TestSeparateV4V6(t *testing.T) {
	mkIPList := func(ips ...string) []netip.Prefix {
		ret := []netip.Prefix{}
		for _, ip := range ips {
			ipp, err := netip.ParsePrefix(ip)
			if err != nil {
				panic(err.Error())
			}
			ret = append(ret, ipp)
		}
		return ret
	}
	cases := []struct {
		name string
		cidr string
		v4   NetworkRange
		v6   NetworkRange
	}{
		{
			name: "wildcard",
			cidr: "*",
			v4:   NetworkRange{IsWildcard: true},
			v6:   NetworkRange{IsWildcard: true},
		},
		{
			name: "v4 only",
			cidr: "10.0.0.0/8,172.16.0.0/16",
			v4:   NetworkRange{CIDRs: mkIPList("10.0.0.0/8", "172.16.0.0/16")},
		},
		{
			name: "v6 only",
			cidr: "fd04:3e42:4a4e:3381::/64,ffff:ffff:ac10:ac10::/64",
			v6:   NetworkRange{CIDRs: mkIPList("fd04:3e42:4a4e:3381::/64", "ffff:ffff:ac10:ac10::/64")},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ConstructTestConfig()
			iptConfigurator, _ := NewIptablesConfigurator(cfg, &dep.DependenciesStub{})
			v4Range, v6Range, err := iptConfigurator.separateV4V6(tt.cidr)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(v4Range, tt.v4) {
				t.Fatalf("expected %v, got %v", tt.v4, v4Range)
			}
			if !reflect.DeepEqual(v6Range, tt.v6) {
				t.Fatalf("expected %v, got %v", tt.v6, v6Range)
			}
		})
	}
}

func compareToGolden(t *testing.T, name string, actual []string) {
	t.Helper()
	gotBytes := []byte(strings.Join(actual, "\n"))
	goldenFile := filepath.Join("testdata", name+".golden")
	testutil.CompareContent(t, gotBytes, goldenFile)
}
