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

package cmd

import (
	"net"
	"reflect"
	"testing"
)

func TestHandleInboundIpv6RulesWithoutEnableInboundIpv6s(t *testing.T) {
	cfg := constructConfig()
	iptConfigurator := NewIptablesConfigurator(cfg)
	ipv6Range := NetworkRange{
		IsWildcard: false,
		IPNets:     nil,
	}
	iptConfigurator.handleInboundIpv6Rules(ipv6Range, ipv6Range)
	actual := FormatIptablesCommands(iptConfigurator.iptables.BuildV6())
	expected := []string{
		"ip6tables -t filter -A INPUT -m state --state ESTABLISHED -j ACCEPT",
		"ip6tables -t filter -A INPUT -i lo -d ::1 -j ACCEPT",
		"ip6tables -t filter -A INPUT -j REJECT",
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Output mismatch. Expected: %#v ; Actual: %#v", expected, actual)
	}
	actual = FormatIptablesCommands(iptConfigurator.iptables.BuildV4())
	if !reflect.DeepEqual(actual, []string{}) {
		t.Errorf("Expected output to be empty; instead got: %#v", actual)
	}
}

func TestHandleInboundIpv6RulesWithEmptyInboundPorts(t *testing.T) {
	cfg := constructConfig()
	iptConfigurator := NewIptablesConfigurator(cfg)
	iptConfigurator.cfg.EnableInboundIPv6s = net.IPv6loopback
	ipv6Range := NetworkRange{
		IsWildcard: false,
		IPNets:     nil,
	}
	iptConfigurator.cfg.InboundPortsInclude = ""
	iptConfigurator.handleInboundIpv6Rules(ipv6Range, ipv6Range)
	actual := FormatIptablesCommands(iptConfigurator.iptables.BuildV6())
	expected := []string{
		"ip6tables -t nat -N ISTIO_REDIRECT",
		"ip6tables -t nat -N ISTIO_IN_REDIRECT",
		"ip6tables -t nat -N ISTIO_OUTPUT",
		"ip6tables -t nat -A ISTIO_REDIRECT -p tcp -j REDIRECT --to-port 15001",
		"ip6tables -t nat -A ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-port 15001",
		"ip6tables -t nat -A OUTPUT -p tcp -j ISTIO_OUTPUT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -s ::6/128 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo ! -d ::1/128 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_OUTPUT -d ::1/128 -j RETURN",
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Output mismatch.\nExpected: %#v\nActual: %#v", expected, actual)
	}
}

func TestHandleInboundIpv6RulesWithInboundPorts(t *testing.T) {
	cfg := constructConfig()
	iptConfigurator := NewIptablesConfigurator(cfg)
	iptConfigurator.cfg.EnableInboundIPv6s = net.IPv6loopback
	ipv6Range := NetworkRange{
		IsWildcard: false,
		IPNets:     nil,
	}
	iptConfigurator.cfg.InboundPortsInclude = "4000,5000"
	iptConfigurator.handleInboundIpv6Rules(ipv6Range, ipv6Range)
	actual := FormatIptablesCommands(iptConfigurator.iptables.BuildV6())
	expected := []string{
		"ip6tables -t nat -N ISTIO_REDIRECT", "ip6tables -t nat -N ISTIO_IN_REDIRECT",
		"ip6tables -t nat -N ISTIO_INBOUND", "ip6tables -t nat -N ISTIO_OUTPUT",
		"ip6tables -t nat -A ISTIO_REDIRECT -p tcp -j REDIRECT --to-port 15001",
		"ip6tables -t nat -A ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-port 15001",
		"ip6tables -t nat -A PREROUTING -p tcp -j ISTIO_INBOUND",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 4000 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 5000 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A OUTPUT -p tcp -j ISTIO_OUTPUT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -s ::6/128 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo ! -d ::1/128 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_OUTPUT -d ::1/128 -j RETURN",
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("Output mismatch. Expected: %#v ; Actual: %#v", expected, actual)
	}
}

func TestHandleInboundIpv6RulesWithWildcardRanges(t *testing.T) {
	cfg := constructConfig()
	iptConfigurator := NewIptablesConfigurator(cfg)
	iptConfigurator.cfg.EnableInboundIPv6s = net.IPv6loopback
	ipv6Range := NetworkRange{
		IsWildcard: true,
		IPNets:     nil,
	}
	iptConfigurator.cfg.InboundPortsInclude = "4000,5000"
	iptConfigurator.cfg.KubevirtInterfaces = "eth0,eth1"
	iptConfigurator.handleInboundIpv6Rules(ipv6Range, ipv6Range)
	actual := FormatIptablesCommands(iptConfigurator.iptables.BuildV6())
	expected := []string{
		"ip6tables -t nat -N ISTIO_REDIRECT",
		"ip6tables -t nat -N ISTIO_IN_REDIRECT",
		"ip6tables -t nat -N ISTIO_INBOUND",
		"ip6tables -t nat -N ISTIO_OUTPUT",
		"ip6tables -t nat -A ISTIO_REDIRECT -p tcp -j REDIRECT --to-port 15001",
		"ip6tables -t nat -A ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-port 15001",
		"ip6tables -t nat -A PREROUTING -p tcp -j ISTIO_INBOUND",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 4000 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 5000 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A OUTPUT -p tcp -j ISTIO_OUTPUT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -s ::6/128 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo ! -d ::1/128 -j ISTIO_IN_REDIRECT",
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
	cfg := constructConfig()
	iptConfigurator := NewIptablesConfigurator(cfg)
	iptConfigurator.cfg.EnableInboundIPv6s = net.IPv6loopback
	_, ipnet, _ := net.ParseCIDR("10.0.0.0/8")
	ipv6Range := NetworkRange{
		IsWildcard: false,
		IPNets:     []*net.IPNet{ipnet},
	}
	iptConfigurator.cfg.InboundPortsInclude = "4000,5000"
	iptConfigurator.cfg.InboundPortsExclude = "6000,7000"
	iptConfigurator.cfg.KubevirtInterfaces = "eth0,eth1"
	iptConfigurator.handleInboundIpv6Rules(ipv6Range, ipv6Range)
	actual := FormatIptablesCommands(iptConfigurator.iptables.BuildV6())
	expected := []string{
		"ip6tables -t nat -N ISTIO_REDIRECT", "ip6tables -t nat -N ISTIO_IN_REDIRECT",
		"ip6tables -t nat -N ISTIO_INBOUND", "ip6tables -t nat -N ISTIO_OUTPUT",
		"ip6tables -t nat -A ISTIO_REDIRECT -p tcp -j REDIRECT --to-port 15001",
		"ip6tables -t nat -A ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-port 15001",
		"ip6tables -t nat -A PREROUTING -p tcp -j ISTIO_INBOUND",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 4000 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 5000 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A OUTPUT -p tcp -j ISTIO_OUTPUT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -s ::6/128 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo ! -d ::1/128 -j ISTIO_IN_REDIRECT",
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
	cfg := constructConfig()
	iptConfigurator := NewIptablesConfigurator(cfg)
	iptConfigurator.cfg.EnableInboundIPv6s = net.IPv6loopback
	_, ipnet, _ := net.ParseCIDR("10.0.0.0/8")
	ipv6Range := NetworkRange{
		IsWildcard: false,
		IPNets:     []*net.IPNet{ipnet},
	}
	iptConfigurator.cfg.InboundPortsInclude = "4000,5000"
	iptConfigurator.cfg.InboundPortsExclude = "6000,7000"
	iptConfigurator.cfg.KubevirtInterfaces = "eth0,eth1"
	iptConfigurator.cfg.ProxyGID = "1,2"
	iptConfigurator.cfg.ProxyUID = "3,4"
	iptConfigurator.handleInboundIpv6Rules(ipv6Range, ipv6Range)
	actual := FormatIptablesCommands(iptConfigurator.iptables.BuildV6())
	expected := []string{
		"ip6tables -t nat -N ISTIO_REDIRECT", "ip6tables -t nat -N ISTIO_IN_REDIRECT",
		"ip6tables -t nat -N ISTIO_INBOUND", "ip6tables -t nat -N ISTIO_OUTPUT",
		"ip6tables -t nat -A ISTIO_REDIRECT -p tcp -j REDIRECT --to-port 15001",
		"ip6tables -t nat -A ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-port 15001",
		"ip6tables -t nat -A PREROUTING -p tcp -j ISTIO_INBOUND",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 4000 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport 5000 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A OUTPUT -p tcp -j ISTIO_OUTPUT",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo -s ::6/128 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -o lo ! -d ::1/128 -j ISTIO_IN_REDIRECT",
		"ip6tables -t nat -A ISTIO_OUTPUT -m owner --uid-owner 3 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -m owner --uid-owner 4 -j RETURN",
		"ip6tables -t nat -A ISTIO_OUTPUT -m owner --gid-owner 1 -j RETURN",
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
	cfg := constructConfig()
	iptConfigurator := NewIptablesConfigurator(cfg)
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
	cfg := constructConfig()
	iptConfigurator := NewIptablesConfigurator(cfg)
	ipv4Range := NetworkRange{
		IsWildcard: true,
		IPNets:     nil,
	}
	iptConfigurator.cfg.KubevirtInterfaces = "eth1,eth2"
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
	cfg := constructConfig()
	iptConfigurator := NewIptablesConfigurator(cfg)
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
	cfg := constructConfig()
	iptConfigurator := NewIptablesConfigurator(cfg)
	_, ipnet, _ := net.ParseCIDR("10.0.0.0/8")
	ipv4Range := NetworkRange{
		IsWildcard: false,
		IPNets:     []*net.IPNet{ipnet},
	}
	iptConfigurator.cfg.KubevirtInterfaces = "eth1,eth2"
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
	iptConfigurator := NewIptablesConfigurator(constructConfig())
	v4Range, v6Range, _ := iptConfigurator.separateV4V6("*")
	if !v4Range.IsWildcard || !v6Range.IsWildcard {
		t.Errorf("Expected v4Range and v6Range to be wildcards")
	}
}

func TestSeparateV4V6WithV4OnlyCIDRPrefix(t *testing.T) {
	iptConfigurator := NewIptablesConfigurator(constructConfig())
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
	ips := []string{}
	for _, val := range v4Range.IPNets {
		ips = append(ips, val.String())
	}
	expectedIpv4s := []string{"10.0.0.0/8", "172.16.0.0/16"}
	if !reflect.DeepEqual(ips, expectedIpv4s) {
		t.Errorf("Output mismatch\nExpected: %#v\nActual: %#v", expectedIpv4s, ips)
	}
}

func TestSeparateV4V6WithV6OnlyCIDRPrefix(t *testing.T) {
	iptConfigurator := NewIptablesConfigurator(constructConfig())
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
	ips := []string{}
	for _, val := range v6Range.IPNets {
		ips = append(ips, val.String())
	}
	expectedIpv6s := []string{"fd04:3e42:4a4e:3381::/64", "ffff:ffff:ac10:ac10::/64"}
	if !reflect.DeepEqual(ips, expectedIpv6s) {
		t.Errorf("Output mismatch\nExpected: %#v\nActual: %#v", expectedIpv6s, ips)
	}
}

func TestHandleInboundPortsIncludeWithEmptyInboundPorts(t *testing.T) {
	iptConfigurator := NewIptablesConfigurator(constructConfig())
	iptConfigurator.cfg.InboundPortsInclude = ""
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
