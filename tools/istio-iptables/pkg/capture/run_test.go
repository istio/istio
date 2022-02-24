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
	"net"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	testutil "istio.io/istio/pilot/test/util"
	"istio.io/istio/tools/istio-iptables/pkg/config"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

func constructTestConfig() *config.Config {
	return &config.Config{
		ProxyPort:               "15001",
		InboundCapturePort:      "15006",
		InboundTunnelPort:       "15008",
		ProxyUID:                constants.DefaultProxyUID,
		ProxyGID:                constants.DefaultProxyUID,
		InboundTProxyMark:       "1337",
		InboundTProxyRouteTable: "133",
		OwnerGroupsInclude:      constants.OwnerGroupsInclude.DefaultValue,
		RestoreFormat:           true,
	}
}

func TestIptables(t *testing.T) {
	cases := []struct {
		name   string
		config func(cfg *config.Config)
	}{
		{
			"ipv6-empty-inbound-ports",
			func(cfg *config.Config) {
				cfg.InboundPortsInclude = ""
				cfg.EnableInboundIPv6 = true
			},
		},
		{
			"ip-range",
			func(cfg *config.Config) {
				cfg.OutboundIPRangesExclude = "1.1.0.0/16"
				cfg.OutboundIPRangesInclude = "9.9.0.0/16"
				cfg.DryRun = true
				cfg.RedirectDNS = true
				cfg.EnableInboundIPv6 = false
				cfg.ProxyGID = "1,2"
				cfg.ProxyUID = "3,4"
				cfg.DNSServersV4 = []string{"127.0.0.53"}
			},
		},
		{
			"tproxy",
			func(cfg *config.Config) {
				cfg.InboundInterceptionMode = constants.TPROXY
				cfg.InboundPortsInclude = "*"
				cfg.OutboundIPRangesExclude = "1.1.0.0/16"
				cfg.OutboundIPRangesInclude = "9.9.0.0/16"
				cfg.DryRun = true
				cfg.RedirectDNS = true
				cfg.DNSServersV4 = []string{"127.0.0.53"}
				cfg.ProxyGID = "1337"
				cfg.ProxyUID = "1337"
				cfg.ExcludeInterfaces = "not-istio-nic"
				cfg.EnableInboundIPv6 = true
			},
		},
		{
			"ipv6-inbound-ports",
			func(cfg *config.Config) {
				cfg.InboundPortsInclude = "4000,5000"
				cfg.EnableInboundIPv6 = true
			},
		},
		{
			"ipv6-virt-interfaces",
			func(cfg *config.Config) {
				cfg.InboundPortsInclude = "4000,5000"
				cfg.KubeVirtInterfaces = "eth0,eth1"
				cfg.EnableInboundIPv6 = true
			},
		},
		{
			"ipv6-ipnets",
			func(cfg *config.Config) {
				cfg.InboundPortsInclude = "4000,5000"
				cfg.InboundPortsExclude = "6000,7000,"
				cfg.KubeVirtInterfaces = "eth0,eth1"
				cfg.OutboundIPRangesExclude = "2001:db8::/32"
				cfg.OutboundIPRangesInclude = "2001:db8::/32"
				cfg.EnableInboundIPv6 = true
			},
		},
		{
			"ipv6-uid-gid",
			func(cfg *config.Config) {
				cfg.InboundPortsInclude = "4000,5000"
				cfg.InboundPortsExclude = "6000,7000"
				cfg.KubeVirtInterfaces = "eth0,eth1"
				cfg.ProxyGID = "1,2"
				cfg.ProxyUID = "3,4"
				cfg.EnableInboundIPv6 = true
				cfg.OutboundIPRangesExclude = "2001:db8::/32"
				cfg.OutboundIPRangesInclude = "2001:db8::/32"
			},
		},
		{
			"ipv6-outbound-ports",
			func(cfg *config.Config) {
				cfg.InboundPortsInclude = ""
				cfg.OutboundPortsInclude = "32000,31000"
				cfg.EnableInboundIPv6 = true
			},
		},
		{
			"empty",
			func(cfg *config.Config) {},
		},
		{
			"kube-virt-interfaces",
			func(cfg *config.Config) {
				cfg.KubeVirtInterfaces = "eth1,eth2"
				cfg.OutboundIPRangesInclude = "*"
			},
		},
		{
			"ipnets",
			func(cfg *config.Config) {
				cfg.OutboundIPRangesInclude = "10.0.0.0/8"
			},
		},
		{
			"ipnets-with-kube-virt-interfaces",
			func(cfg *config.Config) {
				cfg.KubeVirtInterfaces = "eth1,eth2"
				cfg.OutboundIPRangesInclude = "10.0.0.0/8"
			},
		},
		{
			"inbound-ports-include",
			func(cfg *config.Config) {
				cfg.InboundPortsInclude = "32000,31000"
			},
		},
		{
			"inbound-ports-wildcard",
			func(cfg *config.Config) {
				cfg.InboundPortsInclude = "*"
			},
		},
		{
			"inbound-ports-tproxy",
			func(cfg *config.Config) {
				cfg.InboundPortsInclude = "32000,31000"
				cfg.InboundInterceptionMode = constants.TPROXY
			},
		},
		{
			"inbound-ports-wildcard-tproxy",
			func(cfg *config.Config) {
				cfg.InboundPortsInclude = "*"
				cfg.InboundInterceptionMode = constants.TPROXY
			},
		},
		{
			"dns-uid-gid",
			func(cfg *config.Config) {
				cfg.RedirectDNS = true
				cfg.DNSServersV4 = []string{"127.0.0.53"}
				cfg.DNSServersV6 = []string{"::127.0.0.53"}
				cfg.ProxyGID = "1,2"
				cfg.ProxyUID = "3,4"
				cfg.EnableInboundIPv6 = true
			},
		},
		{
			"ipv6-dns-uid-gid",
			func(cfg *config.Config) {
				cfg.EnableInboundIPv6 = true
				cfg.RedirectDNS = true
				cfg.ProxyGID = "1,2"
				cfg.ProxyUID = "3,4"
			},
		},
		{
			"outbound-owner-groups",
			func(cfg *config.Config) {
				cfg.OwnerGroupsInclude = "java,202"
			},
		},
		{
			"outbound-owner-groups-exclude",
			func(cfg *config.Config) {
				cfg.OwnerGroupsExclude = "888,ftp"
			},
		},
		{
			"ipv6-dns-outbound-owner-groups",
			func(cfg *config.Config) {
				cfg.EnableInboundIPv6 = true
				cfg.RedirectDNS = true
				cfg.OwnerGroupsInclude = "java,202"
			},
		},
		{
			"ipv6-dns-outbound-owner-groups-exclude",
			func(cfg *config.Config) {
				cfg.EnableInboundIPv6 = true
				cfg.RedirectDNS = true
				cfg.OwnerGroupsExclude = "888,ftp"
			},
		},
		{
			"outbound-ports-include",
			func(cfg *config.Config) {
				cfg.OutboundPortsInclude = "32000,31000"
			},
		},
		{
			"loopback-outbound-iprange",
			func(cfg *config.Config) {
				cfg.OutboundIPRangesInclude = "127.1.2.3/32"
				cfg.DryRun = true
				cfg.RedirectDNS = true
				cfg.DNSServersV4 = []string{"127.0.0.53"}
				cfg.EnableInboundIPv6 = false
				cfg.ProxyGID = "1,2"
				cfg.ProxyUID = "3,4"
			},
		},
		{
			"basic-exclude-nic",
			func(cfg *config.Config) {
				cfg.ExcludeInterfaces = "not-istio-nic"
			},
		},
		{
			"logging",
			func(cfg *config.Config) {
				cfg.TraceLogging = true
			},
		},
		{
			"drop-invalid",
			func(cfg *config.Config) {
				cfg.DropInvalid = true
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cfg := constructTestConfig()
			tt.config(cfg)
			iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
			iptConfigurator.Run()
			v4Rules := iptConfigurator.iptables.BuildV4()
			v6Rules := iptConfigurator.iptables.BuildV6()
			allRules := append(v4Rules, v6Rules...)
			actual := FormatIptablesCommands(allRules)
			compareToGolden(t, tt.name, actual)
		})
	}
}

func TestSeparateV4V6(t *testing.T) {
	mkIPList := func(ips ...string) []*net.IPNet {
		ret := []*net.IPNet{}
		for _, ip := range ips {
			_, network, err := net.ParseCIDR(ip)
			if err != nil {
				panic(err.Error())
			}
			ret = append(ret, network)
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
			v4:   NetworkRange{IPNets: mkIPList("10.0.0.0/8", "172.16.0.0/16")},
		},
		{
			name: "v6 only",
			cidr: "fd04:3e42:4a4e:3381::/64,ffff:ffff:ac10:ac10::/64",
			v6:   NetworkRange{IPNets: mkIPList("fd04:3e42:4a4e:3381::/64", "ffff:ffff:ac10:ac10::/64")},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cfg := constructTestConfig()
			iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
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
