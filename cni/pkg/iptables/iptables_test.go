//go:build linux
// +build linux

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
package iptables

import (
	"net/netip"
	"path/filepath"
	"strings"
	"testing"

	"istio.io/istio/cni/pkg/scopes"
	testutil "istio.io/istio/pilot/test/util"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

func TestIptablesPodOverrides(t *testing.T) {
	cases := GetCommonInPodTestCases()

	for _, tt := range cases {
		for _, ipv6 := range []bool{false, true} {
			t.Run(tt.name+"_"+ipstr(ipv6), func(t *testing.T) {
				cfg := constructTestConfig()
				cfg.EnableIPv6 = ipv6
				tt.config(cfg)
				ext := &dep.DependenciesStub{}
				iptConfigurator, _, _ := NewIptablesConfigurator(cfg, cfg, ext, ext, EmptyNlDeps())
				err := iptConfigurator.CreateInpodRules(scopes.CNIAgent, tt.podOverrides, "")
				if err != nil {
					t.Fatal(err)
				}

				compareToGolden(t, ipv6, tt.name, ext.ExecutedAll)
			})
		}
	}
}

func TestIptablesHostRules(t *testing.T) {
	cases := GetCommonHostTestCases()

	for _, tt := range cases {
		for _, ipv6 := range []bool{false, true} {
			t.Run(tt.name+"_"+ipstr(ipv6), func(t *testing.T) {
				cfg := constructTestConfig()
				cfg.EnableIPv6 = ipv6
				cfg.HostProbeSNATAddress = netip.MustParseAddr("169.254.7.127")
				cfg.HostProbeV6SNATAddress = netip.MustParseAddr("fd16:9254:7127:1337:ffff:ffff:ffff:ffff")
				tt.config(cfg)
				ext := &dep.DependenciesStub{}
				iptConfigurator, _, _ := NewIptablesConfigurator(cfg, cfg, ext, ext, EmptyNlDeps())
				err := iptConfigurator.CreateHostRulesForHealthChecks()
				if err != nil {
					t.Fatal(err)
				}

				compareToGolden(t, ipv6, tt.name, ext.ExecutedAll)
			})
		}
	}
}

func TestInvokedTwiceIsIdempotent(t *testing.T) {
	tests := GetCommonInPodTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := constructTestConfig()
			tt.config(cfg)
			ext := &dep.DependenciesStub{}
			iptConfigurator, _, _ := NewIptablesConfigurator(cfg, cfg, ext, ext, EmptyNlDeps())
			err := iptConfigurator.CreateInpodRules(scopes.CNIAgent, tt.podOverrides, "")
			if err != nil {
				t.Fatal(err)
			}
			compareToGolden(t, false, tt.name, ext.ExecutedAll)

			*ext = dep.DependenciesStub{}
			// run another time to make sure we are idempotent
			err = iptConfigurator.CreateInpodRules(scopes.CNIAgent, tt.podOverrides, "")
			if err != nil {
				t.Fatal(err)
			}
			compareToGolden(t, false, tt.name, ext.ExecutedAll)
		})
	}
}

func ipstr(ipv6 bool) string {
	if ipv6 {
		return "ipv6"
	}
	return "ipv4"
}

func compareToGolden(t *testing.T, ipv6 bool, name string, actual []string) {
	t.Helper()
	gotBytes := []byte(strings.Join(actual, "\n"))
	goldenFile := filepath.Join("testdata", name+".golden")
	if ipv6 {
		goldenFile = filepath.Join("testdata", name+"_ipv6.golden")
	}
	testutil.CompareContent(t, gotBytes, goldenFile)
}

func constructTestConfig() *IptablesConfig {
	probeSNATipv4 := netip.MustParseAddr("169.254.7.127")
	probeSNATipv6 := netip.MustParseAddr("e9ac:1e77:90ca:399f:4d6d:ece2:2f9b:3164")
	return &IptablesConfig{
		HostProbeSNATAddress:   probeSNATipv4,
		HostProbeV6SNATAddress: probeSNATipv6,
	}
}
