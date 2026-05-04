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

package nodeagent

import (
	"net"
	"net/netip"
	"sync"
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// forceEarlyDemuxDisabled sets the one-shot sysctl gate to "disabled"
// so tests can exercise detection without depending on the host sysctl.
func forceEarlyDemuxDisabled(t *testing.T) {
	t.Helper()
	earlyDemuxOnce = sync.Once{}
	earlyDemuxOnce.Do(func() {})
	earlyDemuxDisabled = true
}

func TestDetectBranchENI_NoBranchENI(t *testing.T) {
	// When there are no iif rules, detectBranchENI should return nil.
	// This test verifies that on a system without branch ENI pods,
	// the detection is a no-op.
	forceEarlyDemuxDisabled(t)
	podIP := netip.MustParseAddr("10.0.0.1")
	result := detectBranchENI(podIP)
	if result != nil {
		t.Errorf("expected nil for non-branch-ENI pod, got %+v", result)
	}
}

func TestDetectBranchENI_IPv6NoBranchENI(t *testing.T) {
	forceEarlyDemuxDisabled(t)
	podIP := netip.MustParseAddr("fd00::1")
	result := detectBranchENI(podIP)
	if result != nil {
		t.Errorf("expected nil for non-branch-ENI IPv6 pod, got %+v", result)
	}
}

func TestAddDelBranchENIRules_IPv4(t *testing.T) {
	// Skip if not root - netlink rule operations require CAP_NET_ADMIN
	if unix.Getuid() != 0 {
		t.Skip("requires root for netlink operations")
	}

	podIP := netip.MustParseAddr("10.99.99.99")
	info := &branchENIRoute{
		table:       199,
		vethName:    "lo", // use loopback as a stand-in
		iifPriority: 10,
	}

	// Create the routing table with a host route (needed for cleanup detection)
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		t.Fatalf("failed to get lo: %v", err)
	}
	route := &netlink.Route{
		Dst:       &net.IPNet{IP: net.ParseIP("10.99.99.99"), Mask: net.CIDRMask(32, 32)},
		Table:     199,
		LinkIndex: lo.Attrs().Index,
		Scope:     netlink.SCOPE_LINK,
	}
	if err := netlink.RouteAdd(route); err != nil {
		t.Fatalf("failed to add test route: %v", err)
	}
	defer netlink.RouteDel(route)

	// Add rules
	if err := addBranchENIRules(podIP, info); err != nil {
		t.Fatalf("addBranchENIRules failed: %v", err)
	}

	// Verify forward rule exists
	rules, _ := netlink.RuleList(unix.AF_INET)
	foundFwd := false
	foundRet := false
	for _, r := range rules {
		if r.Priority == 512 && r.Table == 199 && r.Dst != nil && r.Dst.IP.Equal(net.ParseIP("10.99.99.99")) {
			foundFwd = true
		}
		if r.Priority == 9 && r.Table == 255 && r.IifName == "lo" {
			foundRet = true
		}
	}
	if !foundFwd {
		t.Error("forward rule not found after addBranchENIRules")
	}
	if !foundRet {
		t.Error("return rule not found after addBranchENIRules")
	}

	// Delete rules
	delBranchENIRules(podIP, info)

	// Verify rules are gone
	rules, _ = netlink.RuleList(unix.AF_INET)
	for _, r := range rules {
		if r.Priority == 512 && r.Table == 199 && r.Dst != nil && r.Dst.IP.Equal(net.ParseIP("10.99.99.99")) {
			t.Error("forward rule still exists after delBranchENIRules")
		}
		if r.Priority == 9 && r.Table == 255 && r.IifName == "lo" {
			t.Error("return rule still exists after delBranchENIRules")
		}
	}
}

func TestAddDelBranchENIRules_IPv6(t *testing.T) {
	if unix.Getuid() != 0 {
		t.Skip("requires root for netlink operations")
	}

	podIP := netip.MustParseAddr("fd99::1")
	info := &branchENIRoute{
		table:       198,
		vethName:    "lo",
		iifPriority: 10,
	}

	lo, err := netlink.LinkByName("lo")
	if err != nil {
		t.Fatalf("failed to get lo: %v", err)
	}
	route := &netlink.Route{
		Dst:       &net.IPNet{IP: net.ParseIP("fd99::1"), Mask: net.CIDRMask(128, 128)},
		Table:     198,
		LinkIndex: lo.Attrs().Index,
		Scope:     netlink.SCOPE_LINK,
	}
	if err := netlink.RouteAdd(route); err != nil {
		t.Fatalf("failed to add test route: %v", err)
	}
	defer netlink.RouteDel(route)

	if err := addBranchENIRules(podIP, info); err != nil {
		t.Fatalf("addBranchENIRules IPv6 failed: %v", err)
	}

	rules, _ := netlink.RuleList(unix.AF_INET6)
	foundFwd := false
	foundRet := false
	for _, r := range rules {
		if r.Priority == 512 && r.Table == 198 && r.Dst != nil && r.Dst.IP.Equal(net.ParseIP("fd99::1")) {
			foundFwd = true
		}
		if r.Priority == 9 && r.Table == 255 && r.IifName == "lo" {
			foundRet = true
		}
	}
	if !foundFwd {
		t.Error("IPv6 forward rule not found")
	}
	if !foundRet {
		t.Error("IPv6 return rule not found")
	}

	delBranchENIRules(podIP, info)

	rules, _ = netlink.RuleList(unix.AF_INET6)
	for _, r := range rules {
		if r.Priority == 512 && r.Table == 198 && r.Dst != nil && r.Dst.IP.Equal(net.ParseIP("fd99::1")) {
			t.Error("IPv6 forward rule still exists after delete")
		}
		if r.Priority == 9 && r.Table == 255 && r.IifName == "lo" {
			t.Error("IPv6 return rule still exists after delete")
		}
	}
}

func TestTCPEarlyDemuxIsDisabled(t *testing.T) {
	// On a stock host, tcp_early_demux defaults to 1, so detection should skip.
	// This mainly verifies the gate returns without panicking; the actual sysctl
	// value is host-dependent, so we don't assert a specific result.
	earlyDemuxOnce = sync.Once{}
	earlyDemuxDisabled = false
	_ = tcpEarlyDemuxIsDisabled()
}

func TestDetectBranchENI_WithIIFRule(t *testing.T) {
	if unix.Getuid() != 0 {
		t.Skip("requires root for netlink operations")
	}
	forceEarlyDemuxDisabled(t)

	// Set up a fake branch ENI environment:
	// 1. Create a route in table 197 for a test IP via loopback
	// 2. Create an iif rule for loopback pointing to table 197
	// 3. detectBranchENI should find it

	podIP := netip.MustParseAddr("10.88.88.88")

	lo, err := netlink.LinkByName("lo")
	if err != nil {
		t.Fatalf("failed to get lo: %v", err)
	}

	// Add host route in table 197
	route := &netlink.Route{
		Dst:       &net.IPNet{IP: net.ParseIP("10.88.88.88"), Mask: net.CIDRMask(32, 32)},
		Table:     197,
		LinkIndex: lo.Attrs().Index,
		Scope:     netlink.SCOPE_LINK,
	}
	if err := netlink.RouteAdd(route); err != nil {
		t.Fatalf("failed to add test route: %v", err)
	}
	defer netlink.RouteDel(route)

	// Add iif rule
	rule := netlink.NewRule()
	rule.Family = unix.AF_INET
	rule.IifName = "lo"
	rule.Table = 197
	rule.Priority = 10
	if err := netlink.RuleAdd(rule); err != nil {
		t.Fatalf("failed to add test rule: %v", err)
	}
	defer netlink.RuleDel(rule)

	// Detect
	result := detectBranchENI(podIP)
	if result == nil {
		t.Fatal("expected to detect branch ENI, got nil")
	}
	if result.table != 197 {
		t.Errorf("expected table 197, got %d", result.table)
	}
	if result.vethName != "lo" {
		t.Errorf("expected veth 'lo', got %s", result.vethName)
	}
	if result.iifPriority != 10 {
		t.Errorf("expected iifPriority 10, got %d", result.iifPriority)
	}
}

func TestFeatureFlag(t *testing.T) {
	// Verify the feature flag variable exists and defaults to true
	if !EnableAWSBranchENIProbe {
		t.Error("EnableAWSBranchENIProbe should default to true")
	}
}
