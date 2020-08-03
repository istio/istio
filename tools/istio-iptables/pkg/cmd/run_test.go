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

package cmd

import (
	"net"
	"reflect"
	"testing"

	"istio.io/istio/tools/istio-iptables/pkg/config"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

func constructTestConfig() *config.Config {
	return &config.Config{
		DryRun:                  false,
		RestoreFormat:           true,
		ProxyPort:               "15001",
		InboundCapturePort:      "15006",
		InboundTunnelPort:       "15008",
		ProxyUID:                constants.DefaultProxyUID,
		ProxyGID:                constants.DefaultProxyUID,
		InboundInterceptionMode: "",
		InboundTProxyMark:       "1337",
		InboundTProxyRouteTable: "133",
		InboundPortsInclude:     "",
		InboundPortsExclude:     "",
		OutboundPortsInclude:    "",
		OutboundPortsExclude:    "",
		OutboundIPRangesInclude: "",
		OutboundIPRangesExclude: "",
		KubevirtInterfaces:      "",
		EnableInboundIPv6:       false,
	}
}

func TestHandleInboundIpv6RulesWithEmptyInboundPorts(t *testing.T) {
	cfg := constructTestConfig()
	cfg.InboundPortsInclude = ""
	cfg.EnableInboundIPv6 = true

	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	ipv6Range := NetworkRange{
		IsWildcard: false,
		IPNets:     nil,
	}
	iptConfigurator.handleInboundIpv6Rules(ipv6Range, ipv6Range)
	actual := FormatIptablesCommands(iptConfigurator.iptables.BuildV6())
	expected := []string{
		"ip6tables -t nat -N ISTIO_INBOUND",
		"ip6tables -t nat -N ISTIO_REDIRECT",
		"ip6tables -t nat -N ISTIO_IN_REDIRECT",
		"ip6tables -t nat -N ISTIO_OUTPUT",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 15008 -j RETURN",
		"ip6tables -t nat -A ISTIO_REDIRECT -p tcp -j REDIRECT --to-ports 15001",
		"ip6tables -t nat -A ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-ports 15006",
		"ip6tables -t nat -A OUTPUT -p tcp -j ISTIO_OUTPUT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -s ::6/128 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo ! -d ::1/128 -m owner --uid-owner 1337 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -m owner ! --uid-owner 1337 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -m owner --uid-owner 1337 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo ! -d ::1/128 -m owner --gid-owner 1337 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -m owner ! --gid-owner 1337 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -m owner --gid-owner 1337 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -d ::1/128 -j RETURN",
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Output mismatch.\nExpected: %#v\nActual: %#v", expected, actual)
	}
}

func TestRulesWithIpRange(t *testing.T) {
	cfg := constructTestConfig()
	cfg.OutboundIPRangesExclude = "1.1.0.0/16"
	cfg.OutboundIPRangesInclude = "9.9.0.0/16"
	cfg.DryRun = true
	dnsCaptureByEnvoy.DefaultValue = "ALL"
	dnsCaptureByAgent.DefaultValue = ""
	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	iptConfigurator.cfg.EnableInboundIPv6 = false
	iptConfigurator.cfg.ProxyGID = "1,2"
	iptConfigurator.cfg.ProxyUID = "3,4"
	iptConfigurator.run()
	actual := FormatIptablesCommands(iptConfigurator.iptables.BuildV4())
	expected := []string{
		"iptables -t nat -N ISTIO_INBOUND",
		"iptables -t nat -N ISTIO_REDIRECT",
		"iptables -t nat -N ISTIO_IN_REDIRECT",
		"iptables -t nat -N ISTIO_OUTPUT",
		"iptables -t nat -A ISTIO_INBOUND -p tcp --dport 15008 -j RETURN",
		"iptables -t nat -A ISTIO_REDIRECT -p tcp -j REDIRECT --to-ports 15001",
		"iptables -t nat -A ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-ports 15006",
		"iptables -t nat -A OUTPUT -p tcp -j ISTIO_OUTPUT",
		"iptables -t nat -A ISTIO_OUTPUT -o lo -s 127.0.0.6/32 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -o lo ! -d 127.0.0.1/32 -m owner --uid-owner 3 -j ISTIO_IN_REDIRECT",
		"iptables -t nat -A ISTIO_OUTPUT -o lo -m owner ! --uid-owner 3 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -m owner --uid-owner 3 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -o lo ! -d 127.0.0.1/32 -m owner --uid-owner 4 -j ISTIO_IN_REDIRECT",
		"iptables -t nat -A ISTIO_OUTPUT -o lo -m owner ! --uid-owner 4 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -m owner --uid-owner 4 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -o lo ! -d 127.0.0.1/32 -m owner --gid-owner 1 -j ISTIO_IN_REDIRECT",
		"iptables -t nat -A ISTIO_OUTPUT -o lo -m owner ! --gid-owner 1 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -m owner --gid-owner 1 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -o lo ! -d 127.0.0.1/32 -m owner --gid-owner 2 -j ISTIO_IN_REDIRECT",
		"iptables -t nat -A ISTIO_OUTPUT -o lo -m owner ! --gid-owner 2 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -m owner --gid-owner 2 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -d 127.0.0.1/32 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -d 1.1.0.0/16 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -d 9.9.0.0/16 -j ISTIO_REDIRECT",
		"iptables -t nat -A ISTIO_OUTPUT -j RETURN",
		"iptables -t nat -A OUTPUT -p udp --dport 53 -m owner --uid-owner 3 -j RETURN",
		"iptables -t nat -A OUTPUT -p udp --dport 53 -m owner --uid-owner 4 -j RETURN",
		"iptables -t nat -A OUTPUT -p udp --dport 53 -m owner --gid-owner 1 -j RETURN",
		"iptables -t nat -A OUTPUT -p udp --dport 53 -m owner --gid-owner 2 -j RETURN",
		"iptables -t nat -A OUTPUT -p udp --dport 53 -j DNAT --to-destination 127.0.0.1:15013",
		"iptables -t nat -A POSTROUTING -p udp --dport 15013 -j SNAT --to-source 127.0.0.1",
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Output mismatch. Expected: \n%#v ; Actual: \n%#v", expected, actual)
	}
}

func TestHandleInboundIpv6RulesWithInboundPorts(t *testing.T) {
	cfg := constructTestConfig()
	cfg.InboundPortsInclude = "4000,5000"
	cfg.EnableInboundIPv6 = true

	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	ipv6Range := NetworkRange{
		IsWildcard: false,
		IPNets:     nil,
	}
	iptConfigurator.handleInboundIpv6Rules(ipv6Range, ipv6Range)
	actual := FormatIptablesCommands(iptConfigurator.iptables.BuildV6())
	expected := []string{
		"ip6tables -t nat -N ISTIO_INBOUND",
		"ip6tables -t nat -N ISTIO_REDIRECT",
		"ip6tables -t nat -N ISTIO_IN_REDIRECT",
		"ip6tables -t nat -N ISTIO_OUTPUT",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 15008 -j RETURN",
		"ip6tables -t nat -A ISTIO_REDIRECT -p tcp -j REDIRECT --to-ports 15001",
		"ip6tables -t nat -A ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-ports 15006",
		"ip6tables -t nat -A PREROUTING -p tcp -j ISTIO_INBOUND",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 4000 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 5000 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A OUTPUT -p tcp -j ISTIO_OUTPUT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -s ::6/128 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo ! -d ::1/128 -m owner --uid-owner 1337 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -m owner ! --uid-owner 1337 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -m owner --uid-owner 1337 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo ! -d ::1/128 -m owner --gid-owner 1337 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -m owner ! --gid-owner 1337 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -m owner --gid-owner 1337 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -d ::1/128 -j RETURN",
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Output mismatch. Expected: %#v ; Actual: %#v", expected, actual)
	}
}

func TestHandleInboundIpv6RulesWithWildcardRanges(t *testing.T) {
	cfg := constructTestConfig()
	cfg.InboundPortsInclude = "4000,5000"
	cfg.KubevirtInterfaces = "eth0,eth1"
	cfg.EnableInboundIPv6 = true

	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	ipv6Range := NetworkRange{
		IsWildcard: true,
		IPNets:     nil,
	}
	iptConfigurator.handleInboundIpv6Rules(ipv6Range, ipv6Range)
	actual := FormatIptablesCommands(iptConfigurator.iptables.BuildV6())
	expected := []string{
		"ip6tables -t nat -N ISTIO_INBOUND",
		"ip6tables -t nat -N ISTIO_REDIRECT",
		"ip6tables -t nat -N ISTIO_IN_REDIRECT",
		"ip6tables -t nat -N ISTIO_OUTPUT",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 15008 -j RETURN",
		"ip6tables -t nat -A ISTIO_REDIRECT -p tcp -j REDIRECT --to-ports 15001",
		"ip6tables -t nat -A ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-ports 15006",
		"ip6tables -t nat -A PREROUTING -p tcp -j ISTIO_INBOUND",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 4000 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 5000 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A OUTPUT -p tcp -j ISTIO_OUTPUT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -s ::6/128 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo ! -d ::1/128 -m owner --uid-owner 1337 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -m owner ! --uid-owner 1337 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -m owner --uid-owner 1337 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo ! -d ::1/128 -m owner --gid-owner 1337 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -m owner ! --gid-owner 1337 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -m owner --gid-owner 1337 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -d ::1/128 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -j ISTIO_REDIRECT",
		"ip6tables -t nat -I PREROUTING 1 -i eth0 -j RETURN",
		"ip6tables -t nat -I PREROUTING 1 -i eth1 -j RETURN",
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Output mismatch.\nExpected: %#v\nActual: %#v", expected, actual)
	}
}

func TestHandleInboundIpv6RulesWithIpNets(t *testing.T) {
	cfg := constructTestConfig()
	cfg.InboundPortsInclude = "4000,5000"
	cfg.InboundPortsExclude = "6000,7000,"
	cfg.KubevirtInterfaces = "eth0,eth1"
	cfg.EnableInboundIPv6 = true

	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	_, ipnet, _ := net.ParseCIDR("10.0.0.0/8")
	ipv6Range := NetworkRange{
		IsWildcard: false,
		IPNets:     []*net.IPNet{ipnet},
	}
	iptConfigurator.handleInboundIpv6Rules(ipv6Range, ipv6Range)
	actual := FormatIptablesCommands(iptConfigurator.iptables.BuildV6())
	expected := []string{
		"ip6tables -t nat -N ISTIO_INBOUND",
		"ip6tables -t nat -N ISTIO_REDIRECT",
		"ip6tables -t nat -N ISTIO_IN_REDIRECT",
		"ip6tables -t nat -N ISTIO_OUTPUT",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 15008 -j RETURN",
		"ip6tables -t nat -A ISTIO_REDIRECT -p tcp -j REDIRECT --to-ports 15001",
		"ip6tables -t nat -A ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-ports 15006",
		"ip6tables -t nat -A PREROUTING -p tcp -j ISTIO_INBOUND",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 4000 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 5000 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A OUTPUT -p tcp -j ISTIO_OUTPUT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -s ::6/128 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo ! -d ::1/128 -m owner --uid-owner 1337 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -m owner ! --uid-owner 1337 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -m owner --uid-owner 1337 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo ! -d ::1/128 -m owner --gid-owner 1337 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -m owner ! --gid-owner 1337 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -m owner --gid-owner 1337 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -d ::1/128 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -d 10.0.0.0/8 -j RETURN",
		"ip6tables -t nat -I PREROUTING 1 -i eth0 -d 10.0.0.0/8 -j ISTIO_REDIRECT",
		"ip6tables -t nat -I PREROUTING 1 -i eth1 -d 10.0.0.0/8 -j ISTIO_REDIRECT",
		"ip6tables -t nat -A ISTIO_OUTPUT -d 10.0.0.0/8 -j ISTIO_REDIRECT",
		"ip6tables -t nat -A ISTIO_OUTPUT -j RETURN",
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Output mismatch.\nExpected: %#v\nActual: %#v", expected, actual)
	}
}

func TestHandleInboundIpv6RulesWithUidGid(t *testing.T) {
	cfg := constructTestConfig()
	cfg.InboundPortsInclude = "4000,5000"
	cfg.InboundPortsExclude = "6000,7000"
	cfg.KubevirtInterfaces = "eth0,eth1"
	cfg.ProxyGID = "1,2"
	cfg.ProxyUID = "3,4"
	cfg.EnableInboundIPv6 = true

	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	_, ipnet, _ := net.ParseCIDR("10.0.0.0/8")
	ipv6Range := NetworkRange{
		IsWildcard: false,
		IPNets:     []*net.IPNet{ipnet},
	}
	iptConfigurator.handleInboundIpv6Rules(ipv6Range, ipv6Range)
	actual := FormatIptablesCommands(iptConfigurator.iptables.BuildV6())
	expected := []string{
		"ip6tables -t nat -N ISTIO_INBOUND",
		"ip6tables -t nat -N ISTIO_REDIRECT",
		"ip6tables -t nat -N ISTIO_IN_REDIRECT",
		"ip6tables -t nat -N ISTIO_OUTPUT",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 15008 -j RETURN",
		"ip6tables -t nat -A ISTIO_REDIRECT -p tcp -j REDIRECT --to-ports 15001",
		"ip6tables -t nat -A ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-ports 15006",
		"ip6tables -t nat -A PREROUTING -p tcp -j ISTIO_INBOUND",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 4000 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 5000 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A OUTPUT -p tcp -j ISTIO_OUTPUT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -s ::6/128 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo ! -d ::1/128 -m owner --uid-owner 3 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -m owner ! --uid-owner 3 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -m owner --uid-owner 3 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo ! -d ::1/128 -m owner --uid-owner 4 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -m owner ! --uid-owner 4 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -m owner --uid-owner 4 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo ! -d ::1/128 -m owner --gid-owner 1 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -m owner ! --gid-owner 1 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -m owner --gid-owner 1 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo ! -d ::1/128 -m owner --gid-owner 2 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -m owner ! --gid-owner 2 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -m owner --gid-owner 2 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -d ::1/128 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -d 10.0.0.0/8 -j RETURN",
		"ip6tables -t nat -I PREROUTING 1 -i eth0 -d 10.0.0.0/8 -j ISTIO_REDIRECT",
		"ip6tables -t nat -I PREROUTING 1 -i eth1 -d 10.0.0.0/8 -j ISTIO_REDIRECT",
		"ip6tables -t nat -A ISTIO_OUTPUT -d 10.0.0.0/8 -j ISTIO_REDIRECT",
		"ip6tables -t nat -A ISTIO_OUTPUT -j RETURN",
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Output mismatch.\nExpected: %#v\nActual: %#v", expected, actual)
	}
}

func TestHandleInboundIpv4RulesWithWildCard(t *testing.T) {
	cfg := constructTestConfig()

	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	ipv4Range := NetworkRange{
		IsWildcard: true,
		IPNets:     nil,
	}
	iptConfigurator.handleInboundIpv4Rules(ipv4Range)
	actual := FormatIptablesCommands(iptConfigurator.iptables.BuildV4())
	expected := []string{
		"iptables -t nat -N ISTIO_OUTPUT",
		"iptables -t nat -A ISTIO_OUTPUT -j ISTIO_REDIRECT",
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Output mismatch.\nExpected: %#v\nActual: %#v", expected, actual)
	}
}

func TestHandleInboundIpv4RulesWithWildcardWithKubeVirtInterfaces(t *testing.T) {
	cfg := constructTestConfig()
	cfg.KubevirtInterfaces = "eth1,eth2"

	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	ipv4Range := NetworkRange{
		IsWildcard: true,
		IPNets:     nil,
	}
	iptConfigurator.handleInboundIpv4Rules(ipv4Range)
	actual := FormatIptablesCommands(iptConfigurator.iptables.BuildV4())
	expected := []string{
		"iptables -t nat -N ISTIO_OUTPUT",
		"iptables -t nat -A ISTIO_OUTPUT -j ISTIO_REDIRECT",
		"iptables -t nat -I PREROUTING 1 -i eth1 -j ISTIO_REDIRECT",
		"iptables -t nat -I PREROUTING 1 -i eth2 -j ISTIO_REDIRECT",
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Output mismatch.\nExpected: %#v\nActual: %#v", expected, actual)
	}
}

func TestHandleInboundIpv4RulesWithIpNets(t *testing.T) {
	cfg := constructTestConfig()

	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	_, ipnet, _ := net.ParseCIDR("10.0.0.0/8")
	ipv4Range := NetworkRange{
		IsWildcard: false,
		IPNets:     []*net.IPNet{ipnet},
	}
	iptConfigurator.handleInboundIpv4Rules(ipv4Range)
	actual := FormatIptablesCommands(iptConfigurator.iptables.BuildV4())
	expected := []string{
		"iptables -t nat -N ISTIO_OUTPUT",
		"iptables -t nat -A ISTIO_OUTPUT -d 10.0.0.0/8 -j ISTIO_REDIRECT",
		"iptables -t nat -A ISTIO_OUTPUT -j RETURN",
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Output mismatch.\nExpected: %#v\nActual: %#v", expected, actual)
	}
}

func TestHandleInboundIpv4RulesWithIpNetsWithKubeVirtInterfaces(t *testing.T) {
	cfg := constructTestConfig()
	cfg.KubevirtInterfaces = "eth1,eth2"

	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	_, ipnet, _ := net.ParseCIDR("10.0.0.0/8")
	ipv4Range := NetworkRange{
		IsWildcard: false,
		IPNets:     []*net.IPNet{ipnet},
	}
	iptConfigurator.handleInboundIpv4Rules(ipv4Range)
	actual := FormatIptablesCommands(iptConfigurator.iptables.BuildV4())
	expected := []string{
		"iptables -t nat -N ISTIO_OUTPUT",
		"iptables -t nat -I PREROUTING 1 -i eth1 -d 10.0.0.0/8 -j ISTIO_REDIRECT",
		"iptables -t nat -I PREROUTING 1 -i eth2 -d 10.0.0.0/8 -j ISTIO_REDIRECT",
		"iptables -t nat -A ISTIO_OUTPUT -d 10.0.0.0/8 -j ISTIO_REDIRECT",
		"iptables -t nat -A ISTIO_OUTPUT -j RETURN",
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Output mismatch.\nExpected: %#v\nActual: %#v", expected, actual)
	}
}

func TestSeparateV4V6WithWildcardCIDRPrefix(t *testing.T) {
	cfg := constructTestConfig()

	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	v4Range, v6Range, _ := iptConfigurator.separateV4V6("*")
	if !v4Range.IsWildcard || !v6Range.IsWildcard {
		t.Errorf("Expected v4Range and v6Range to be wildcards")
	}
}

func TestSeparateV4V6WithV4OnlyCIDRPrefix(t *testing.T) {
	cfg := constructTestConfig()

	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	v4Range, v6Range, _ := iptConfigurator.separateV4V6("10.0.0.0/8,172.16.0.0/16")
	if v4Range.IsWildcard {
		t.Errorf("Expected v4Range to be not have wildcards")
	}
	expectedIpv6Range := NetworkRange{
		IsWildcard: false,
		IPNets:     make([]*net.IPNet, 0),
	}
	if !reflect.DeepEqual(v6Range, expectedIpv6Range) {
		t.Errorf("Output mismatch\nExpected: %#v\nActual: %#v", expectedIpv6Range, v6Range)
	}
	var ips []string
	for _, val := range v4Range.IPNets {
		ips = append(ips, val.String())
	}
	expectedIpv4s := []string{"10.0.0.0/8", "172.16.0.0/16"}
	if !reflect.DeepEqual(ips, expectedIpv4s) {
		t.Errorf("Output mismatch\nExpected: %#v\nActual: %#v", expectedIpv4s, ips)
	}
}

func TestSeparateV4V6WithV6OnlyCIDRPrefix(t *testing.T) {
	cfg := constructTestConfig()

	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	v4Range, v6Range, _ := iptConfigurator.separateV4V6("fd04:3e42:4a4e:3381::/64,ffff:ffff:ac10:ac10::/64")
	if v6Range.IsWildcard {
		t.Errorf("Expected v6Range to be not have wildcards")
	}
	expectedIpv4Range := NetworkRange{
		IsWildcard: false,
		IPNets:     make([]*net.IPNet, 0),
	}
	if !reflect.DeepEqual(v4Range, expectedIpv4Range) {
		t.Errorf("Output mismatch\nExpected: %#v\nActual: %#v", expectedIpv4Range, v4Range)
	}
	var ips []string
	for _, val := range v6Range.IPNets {
		ips = append(ips, val.String())
	}
	expectedIpv6s := []string{"fd04:3e42:4a4e:3381::/64", "ffff:ffff:ac10:ac10::/64"}
	if !reflect.DeepEqual(ips, expectedIpv6s) {
		t.Errorf("Output mismatch\nExpected: %#v\nActual: %#v", expectedIpv6s, ips)
	}
}

func TestHandleInboundPortsIncludeWithEmptyInboundPorts(t *testing.T) {
	cfg := constructTestConfig()
	cfg.InboundPortsInclude = ""

	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	iptConfigurator.handleInboundPortsInclude()

	ip4Rules := FormatIptablesCommands(iptConfigurator.iptables.BuildV4())
	ip6Rules := FormatIptablesCommands(iptConfigurator.iptables.BuildV6())

	if !reflect.DeepEqual([]string{}, ip4Rules) {
		t.Errorf("Expected ip4Rules to be empty; instead got %#v", ip4Rules)
	}
	if !reflect.DeepEqual([]string{}, ip6Rules) {
		t.Errorf("Expected ip6Rules to be empty; instead got %#v", ip6Rules)
	}
}

func TestHandleInboundPortsIncludeWithInboundPorts(t *testing.T) {
	cfg := constructTestConfig()
	cfg.InboundPortsInclude = "32000,31000"

	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	iptConfigurator.handleInboundPortsInclude()

	ip4Rules := FormatIptablesCommands(iptConfigurator.iptables.BuildV4())
	ip6Rules := FormatIptablesCommands(iptConfigurator.iptables.BuildV6())
	if !reflect.DeepEqual([]string{}, ip6Rules) {
		t.Errorf("Expected ip6Rules to be empty; instead got %#v", ip6Rules)
	}
	expectedIpv4Rules := []string{
		"iptables -t nat -N ISTIO_INBOUND",
		"iptables -t nat -A PREROUTING -p tcp -j ISTIO_INBOUND",
		"iptables -t nat -A ISTIO_INBOUND -p tcp --dport 32000 -j ISTIO_IN_REDIRECT",
		"iptables -t nat -A ISTIO_INBOUND -p tcp --dport 31000 -j ISTIO_IN_REDIRECT",
	}
	if !reflect.DeepEqual(ip4Rules, expectedIpv4Rules) {
		t.Errorf("Output mismatch\nExpected: %#v\nActual: %#v", expectedIpv4Rules, ip4Rules)
	}
}

func TestHandleInboundPortsIncludeWithWildcardInboundPorts(t *testing.T) {
	cfg := constructTestConfig()
	cfg.InboundPortsInclude = "*"

	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	iptConfigurator.handleInboundPortsInclude()

	ip4Rules := FormatIptablesCommands(iptConfigurator.iptables.BuildV4())
	ip6Rules := FormatIptablesCommands(iptConfigurator.iptables.BuildV6())
	if !reflect.DeepEqual([]string{}, ip6Rules) {
		t.Errorf("Expected ip6Rules to be empty; instead got %#v", ip6Rules)
	}
	expectedIpv4Rules := []string{
		"iptables -t nat -N ISTIO_INBOUND",
		"iptables -t nat -A PREROUTING -p tcp -j ISTIO_INBOUND",
		"iptables -t nat -A ISTIO_INBOUND -p tcp --dport 22 -j RETURN",
		"iptables -t nat -A ISTIO_INBOUND -p tcp -j ISTIO_IN_REDIRECT",
	}
	if !reflect.DeepEqual(ip4Rules, expectedIpv4Rules) {
		t.Errorf("Output mismatch\nExpected: %#v\nActual: %#v", expectedIpv4Rules, ip4Rules)
	}
}

func TestHandleInboundPortsIncludeWithInboundPortsAndTproxy(t *testing.T) {
	cfg := constructTestConfig()
	cfg.InboundPortsInclude = "32000,31000"
	cfg.InboundInterceptionMode = "TPROXY"

	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	iptConfigurator.handleInboundPortsInclude()

	ip4Rules := FormatIptablesCommands(iptConfigurator.iptables.BuildV4())
	ip6Rules := FormatIptablesCommands(iptConfigurator.iptables.BuildV6())
	if !reflect.DeepEqual([]string{}, ip6Rules) {
		t.Errorf("Expected ip6Rules to be empty; instead got %#v", ip6Rules)
	}
	expectedIpv4Rules := []string{
		"iptables -t mangle -N ISTIO_DIVERT",
		"iptables -t mangle -N ISTIO_TPROXY",
		"iptables -t mangle -N ISTIO_INBOUND",
		"iptables -t mangle -A ISTIO_DIVERT -j MARK --set-mark 1337",
		"iptables -t mangle -A ISTIO_DIVERT -j ACCEPT",
		"iptables -t mangle -A ISTIO_TPROXY ! -d 127.0.0.1/32 -p tcp -j TPROXY --tproxy-mark 1337/0xffffffff --on-port 15001",
		"iptables -t mangle -A PREROUTING -p tcp -j ISTIO_INBOUND",
		"iptables -t mangle -A ISTIO_INBOUND -p tcp --dport 32000 -m conntrack --ctstate RELATED,ESTABLISHED -j ISTIO_DIVERT",
		"iptables -t mangle -A ISTIO_INBOUND -p tcp --dport 32000 -j ISTIO_TPROXY",
		"iptables -t mangle -A ISTIO_INBOUND -p tcp --dport 31000 -m conntrack --ctstate RELATED,ESTABLISHED -j ISTIO_DIVERT",
		"iptables -t mangle -A ISTIO_INBOUND -p tcp --dport 31000 -j ISTIO_TPROXY",
	}
	if !reflect.DeepEqual(ip4Rules, expectedIpv4Rules) {
		t.Errorf("Output mismatch\nExpected: %#v\nActual: %#v", expectedIpv4Rules, ip4Rules)
	}
}

func TestHandleInboundPortsIncludeWithWildcardInboundPortsAndTproxy(t *testing.T) {
	cfg := constructTestConfig()
	cfg.InboundPortsInclude = "*"
	cfg.InboundInterceptionMode = "TPROXY"

	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	iptConfigurator.handleInboundPortsInclude()

	ip4Rules := FormatIptablesCommands(iptConfigurator.iptables.BuildV4())
	ip6Rules := FormatIptablesCommands(iptConfigurator.iptables.BuildV6())
	if !reflect.DeepEqual([]string{}, ip6Rules) {
		t.Errorf("Expected ip6Rules to be empty; instead got %#v", ip6Rules)
	}
	expectedIpv4Rules := []string{
		"iptables -t mangle -N ISTIO_DIVERT",
		"iptables -t mangle -N ISTIO_TPROXY",
		"iptables -t mangle -N ISTIO_INBOUND",
		"iptables -t mangle -A ISTIO_DIVERT -j MARK --set-mark 1337",
		"iptables -t mangle -A ISTIO_DIVERT -j ACCEPT",
		"iptables -t mangle -A ISTIO_TPROXY ! -d 127.0.0.1/32 -p tcp -j TPROXY --tproxy-mark 1337/0xffffffff --on-port 15001",
		"iptables -t mangle -A PREROUTING -p tcp -j ISTIO_INBOUND",
		"iptables -t mangle -A ISTIO_INBOUND -p tcp --dport 22 -j RETURN",
		"iptables -t mangle -A ISTIO_INBOUND -p tcp -m conntrack --ctstate RELATED,ESTABLISHED -j ISTIO_DIVERT",
		"iptables -t mangle -A ISTIO_INBOUND -p tcp -j ISTIO_TPROXY",
	}
	if !reflect.DeepEqual(ip4Rules, expectedIpv4Rules) {
		t.Errorf("Output mismatch\nExpected: %#v\nActual: %#v", expectedIpv4Rules, ip4Rules)
	}
}

func TestHandleInboundIpv4RulesWithUidGid(t *testing.T) {
	cfg := constructConfig()
	cfg.DryRun = true
	dnsCaptureByEnvoy.DefaultValue = ""
	dnsCaptureByAgent.DefaultValue = "ALL"
	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	iptConfigurator.cfg.EnableInboundIPv6 = false
	iptConfigurator.cfg.ProxyGID = "1,2"
	iptConfigurator.cfg.ProxyUID = "3,4"
	iptConfigurator.run()
	actual := FormatIptablesCommands(iptConfigurator.iptables.BuildV4())
	expected := []string{
		"iptables -t nat -N ISTIO_INBOUND",
		"iptables -t nat -N ISTIO_REDIRECT",
		"iptables -t nat -N ISTIO_IN_REDIRECT",
		"iptables -t nat -N ISTIO_OUTPUT",
		"iptables -t nat -A ISTIO_INBOUND -p tcp --dport 15008 -j RETURN",
		"iptables -t nat -A ISTIO_REDIRECT -p tcp -j REDIRECT --to-ports 15001",
		"iptables -t nat -A ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-ports 15006",
		"iptables -t nat -A OUTPUT -p tcp -j ISTIO_OUTPUT",
		"iptables -t nat -A ISTIO_OUTPUT -o lo -s 127.0.0.6/32 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -o lo ! -d 127.0.0.1/32 -m owner --uid-owner 3 -j ISTIO_IN_REDIRECT",
		"iptables -t nat -A ISTIO_OUTPUT -o lo -m owner ! --uid-owner 3 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -m owner --uid-owner 3 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -o lo ! -d 127.0.0.1/32 -m owner --uid-owner 4 -j ISTIO_IN_REDIRECT",
		"iptables -t nat -A ISTIO_OUTPUT -o lo -m owner ! --uid-owner 4 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -m owner --uid-owner 4 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -o lo ! -d 127.0.0.1/32 -m owner --gid-owner 1 -j ISTIO_IN_REDIRECT",
		"iptables -t nat -A ISTIO_OUTPUT -o lo -m owner ! --gid-owner 1 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -m owner --gid-owner 1 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -o lo ! -d 127.0.0.1/32 -m owner --gid-owner 2 -j ISTIO_IN_REDIRECT",
		"iptables -t nat -A ISTIO_OUTPUT -o lo -m owner ! --gid-owner 2 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -m owner --gid-owner 2 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -d 127.0.0.1/32 -j RETURN",
		"iptables -t nat -A OUTPUT -p udp --dport 53 -m owner --uid-owner 3 -j RETURN",
		"iptables -t nat -A OUTPUT -p udp --dport 53 -m owner --uid-owner 4 -j RETURN",
		"iptables -t nat -A OUTPUT -p udp --dport 53 -m owner --gid-owner 1 -j RETURN",
		"iptables -t nat -A OUTPUT -p udp --dport 53 -m owner --gid-owner 2 -j RETURN",
		"iptables -t nat -A OUTPUT -p udp --dport 53 -j DNAT --to-destination 127.0.0.1:15053",
		"iptables -t nat -A POSTROUTING -p udp --dport 15053 -j SNAT --to-source 127.0.0.1",
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Output mismatch.\nExpected: %#v\nActual: %#v", expected, actual)
	}
}

func TestGenerateEmptyV6ConfigOnV4OnlyEnv(t *testing.T) {
	cfg := constructConfig()
	cfg.DryRun = true
	dnsCaptureByEnvoy.DefaultValue = ""
	dnsCaptureByAgent.DefaultValue = "ALL"
	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	iptConfigurator.cfg.EnableInboundIPv6 = false
	iptConfigurator.cfg.ProxyGID = "1,2"
	iptConfigurator.cfg.ProxyUID = "3,4"
	iptConfigurator.run()
	actual := FormatIptablesCommands(iptConfigurator.iptables.BuildV6())
	expected := []string{}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Output mismatch.\nExpected: empty\nActual: %#v", actual)
	}
}

func TestHandleOutboundPortsIncludeWithOutboundPorts(t *testing.T) {
	cfg := constructTestConfig()
	cfg.OutboundPortsInclude = "32000,31000"

	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	iptConfigurator.handleOutboundPortsInclude()

	ip4Rules := FormatIptablesCommands(iptConfigurator.iptables.BuildV4())
	ip6Rules := FormatIptablesCommands(iptConfigurator.iptables.BuildV6())
	if !reflect.DeepEqual([]string{}, ip6Rules) {
		t.Errorf("Expected ip6Rules to be empty; instead got %#v", ip6Rules)
	}
	expectedIpv4Rules := []string{
		"iptables -t nat -N ISTIO_OUTPUT",
		"iptables -t nat -A ISTIO_OUTPUT -p tcp --dport 32000 -j ISTIO_REDIRECT",
		"iptables -t nat -A ISTIO_OUTPUT -p tcp --dport 31000 -j ISTIO_REDIRECT",
	}
	if !reflect.DeepEqual(ip4Rules, expectedIpv4Rules) {
		t.Errorf("Output mismatch\nExpected: %#v\nActual: %#v", expectedIpv4Rules, ip4Rules)
	}
}

func TestRulesWithLoopbackIpInOutboundIpRanges(t *testing.T) {
	cfg := constructTestConfig()
	cfg.OutboundIPRangesInclude = "127.1.2.3/32"
	cfg.DryRun = true
	dnsCaptureByEnvoy.DefaultValue = "ALL"
	dnsCaptureByAgent.DefaultValue = ""
	iptConfigurator := NewIptablesConfigurator(cfg, &dep.StdoutStubDependencies{})
	iptConfigurator.cfg.EnableInboundIPv6 = false
	iptConfigurator.cfg.ProxyGID = "1,2"
	iptConfigurator.cfg.ProxyUID = "3,4"
	iptConfigurator.run()
	actual := FormatIptablesCommands(iptConfigurator.iptables.BuildV4())
	expected := []string{
		"iptables -t nat -N ISTIO_INBOUND",
		"iptables -t nat -N ISTIO_REDIRECT",
		"iptables -t nat -N ISTIO_IN_REDIRECT",
		"iptables -t nat -N ISTIO_OUTPUT",
		"iptables -t nat -A ISTIO_INBOUND -p tcp --dport 15008 -j RETURN",
		"iptables -t nat -A ISTIO_REDIRECT -p tcp -j REDIRECT --to-ports 15001",
		"iptables -t nat -A ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-ports 15006",
		"iptables -t nat -A OUTPUT -p tcp -j ISTIO_OUTPUT",
		"iptables -t nat -A ISTIO_OUTPUT -o lo -s 127.0.0.6/32 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -o lo ! -d 127.0.0.1/32 -m owner --uid-owner 3 -j ISTIO_IN_REDIRECT",
		"iptables -t nat -A ISTIO_OUTPUT -m owner --uid-owner 3 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -o lo ! -d 127.0.0.1/32 -m owner --uid-owner 4 -j ISTIO_IN_REDIRECT",
		"iptables -t nat -A ISTIO_OUTPUT -m owner --uid-owner 4 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -o lo ! -d 127.0.0.1/32 -m owner --gid-owner 1 -j ISTIO_IN_REDIRECT",
		"iptables -t nat -A ISTIO_OUTPUT -m owner --gid-owner 1 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -o lo ! -d 127.0.0.1/32 -m owner --gid-owner 2 -j ISTIO_IN_REDIRECT",
		"iptables -t nat -A ISTIO_OUTPUT -m owner --gid-owner 2 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -d 127.0.0.1/32 -j RETURN",
		"iptables -t nat -A ISTIO_OUTPUT -d 127.1.2.3/32 -j ISTIO_REDIRECT",
		"iptables -t nat -A ISTIO_OUTPUT -j RETURN",
		"iptables -t nat -A OUTPUT -p udp --dport 53 -m owner --uid-owner 3 -j RETURN",
		"iptables -t nat -A OUTPUT -p udp --dport 53 -m owner --uid-owner 4 -j RETURN",
		"iptables -t nat -A OUTPUT -p udp --dport 53 -m owner --gid-owner 1 -j RETURN",
		"iptables -t nat -A OUTPUT -p udp --dport 53 -m owner --gid-owner 2 -j RETURN",
		"iptables -t nat -A OUTPUT -p udp --dport 53 -j DNAT --to-destination 127.0.0.1:15013",
		"iptables -t nat -A POSTROUTING -p udp --dport 15013 -j SNAT --to-source 127.0.0.1",
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Output mismatch. Expected: \n%#v ; Actual: \n%#v", expected, actual)
	}
}
