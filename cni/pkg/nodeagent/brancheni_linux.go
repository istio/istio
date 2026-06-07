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
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sync"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	pconstants "istio.io/istio/cni/pkg/constants"
)

// branchENIRoute holds the routing table, veth name, and the priority of aws-vpc-cni's
// iif rule for a branch ENI pod.
type branchENIRoute struct {
	table       int
	vethName    string
	iifPriority int // priority of aws-vpc-cni's "iif <veth> lookup <table>" rule
}

var (
	earlyDemuxOnce     sync.Once
	earlyDemuxDisabled bool // true if net.ipv4.tcp_early_demux=0 (prerequisite for SGP)
)

// tcpEarlyDemuxIsDisabled returns true if net.ipv4.tcp_early_demux=0, which AWS
// requires operators to set on every node running Security Groups for Pods.
// Since the kernel default is 1, this sysctl being 0 is an explicit operator
// signal that the node is prepared for SGP - we use it as a gate so branch ENI
// detection is fully inert on any other node.
// See https://docs.aws.amazon.com/eks/latest/userguide/security-groups-pods-deployment.html
func tcpEarlyDemuxIsDisabled() bool {
	earlyDemuxOnce.Do(func() {
		path := pconstants.HostMountsPath + "/proc/sys/net/ipv4/tcp_early_demux"
		data, err := os.ReadFile(path)
		if err != nil {
			log.Debugf("could not read %s, assuming default (enabled): %v", path, err)
			return
		}
		earlyDemuxDisabled = len(data) > 0 && data[0] == '0'
		if earlyDemuxDisabled {
			log.Infof("net.ipv4.tcp_early_demux=0; AWS branch ENI probe detection enabled")
		} else {
			log.Debugf("net.ipv4.tcp_early_demux=1 (default); AWS branch ENI probe detection skipped")
		}
	})
	return earlyDemuxDisabled
}

// detectBranchENI checks if a pod IP is routed via a branch ENI (i.e., the route
// exists only in a non-main routing table, indicating aws-vpc-cni SGP setup).
// Returns nil if the pod uses standard veth routing (route in main table).
//
// Gated on net.ipv4.tcp_early_demux=0, which is required by AWS for SGP. On any
// node without that sysctl set, we return nil immediately - there's no SGP
// configuration to find and no rules we could meaningfully add.
//
// Detection: scan ip rules for iif-based entries whose routing table
// contains a host route to the pod IP. If found, this is a branch ENI pod.
// If not found, it's a regular veth pod (no action needed).
//
// Regular veth pods have destination-based rules ("512: to <podIP> lookup main")
// and never have iif rules. Only branch ENI pods get iif rules from aws-vpc-cni.
//
// This approach avoids hardcoding table ranges or priorities and works for
// both IPv4 and IPv6.
func detectBranchENI(podIP netip.Addr) *branchENIRoute {
	if !tcpEarlyDemuxIsDisabled() {
		return nil
	}

	ip := net.IP(podIP.AsSlice())

	family := unix.AF_INET
	if podIP.Is6() {
		family = unix.AF_INET6
	}

	rules, err := netlink.RuleList(family)
	if err != nil {
		log.Debugf("failed to list ip rules: %v", err)
		return nil
	}

	// Build a map of table -> iif rules for quick lookup.
	type iifRule struct {
		iifName  string
		priority int
	}
	tableIIFRules := make(map[int][]iifRule)
	for _, rule := range rules {
		if rule.IifName == "" || rule.Table <= 0 || rule.Table == 254 || rule.Table == 255 {
			continue
		}
		tableIIFRules[rule.Table] = append(tableIIFRules[rule.Table], iifRule{
			iifName:  rule.IifName,
			priority: rule.Priority,
		})
	}

	// For each table that has iif rules, check if it contains a host route to our pod.
	for table, iifRules := range tableIIFRules {
		tableRoutes, err := netlink.RouteListFiltered(unix.AF_UNSPEC, &netlink.Route{
			Table: table,
		}, netlink.RT_FILTER_TABLE)
		if err != nil {
			continue
		}

		for _, r := range tableRoutes {
			if r.Dst == nil || !r.Dst.IP.Equal(ip) {
				continue
			}
			ones, _ := r.Dst.Mask.Size()
			if ones != 32 && ones != 128 {
				continue
			}
			// Found a host route to the pod in this table.
			// Now find the iif rule whose device matches the route's link index.
			routeLink, err := netlink.LinkByIndex(r.LinkIndex)
			if err != nil {
				continue
			}
			routeDevName := routeLink.Attrs().Name

			for _, ir := range iifRules {
				if ir.iifName == routeDevName {
					log.Debugf("detected branch ENI pod %s: veth=%s table=%d iifPriority=%d",
						podIP, routeDevName, table, ir.priority)
					return &branchENIRoute{
						table:       table,
						vethName:    routeDevName,
						iifPriority: ir.priority,
					}
				}
			}
		}
	}
	return nil
}

// addBranchENIRules adds ip rules to route probe traffic via the veth for a branch ENI pod.
// This is needed because aws-vpc-cni routes branch ENI pod traffic through VPC fabric,
// which cannot handle the link-local SNAT address (169.254.7.127) used by istio-cni.
//
// Two rules are added:
//   - Forward path: "ip rule add to <podIP> lookup <table> priority 512"
//     Routes host-originated traffic to the pod via the veth instead of VPC gateway.
//     Priority 512 matches what aws-vpc-cni uses for regular veth pods.
//   - Return path: "ip rule add iif <veth> lookup local priority <N>"
//     Ensures reply traffic from the pod is delivered locally (to conntrack/kubelet)
//     instead of being hijacked by aws-vpc-cni's iif rule that routes it back to VPC.
//     Priority is set to one less than aws-vpc-cni's iif rule priority.
func addBranchENIRules(podIP netip.Addr, info *branchENIRoute) error {
	ip := net.IP(podIP.AsSlice())
	family := unix.AF_INET
	bits := 32
	if podIP.Is6() {
		family = unix.AF_INET6
		bits = 128
	}

	// Forward path: route to pod via veth
	fwdRule := netlink.NewRule()
	fwdRule.Family = family
	fwdRule.Dst = &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)}
	fwdRule.Table = info.table
	fwdRule.Priority = 512

	fwdAdded := false
	if err := netlink.RuleAdd(fwdRule); err != nil {
		if !errors.Is(err, unix.EEXIST) {
			return fmt.Errorf("failed to add forward ip rule for branch ENI pod %s: %w", podIP, err)
		}
	} else {
		fwdAdded = true
	}
	log.Debugf("added branch ENI forward rule: to %s lookup %d priority 512", podIP, info.table)

	// Return path: deliver locally before aws-vpc-cni's iif rule.
	// Insert at priority one less than the existing iif rule so we run first.
	retPriority := info.iifPriority - 1
	if retPriority < 0 {
		retPriority = 0
	}

	retRule := netlink.NewRule()
	retRule.Family = family
	retRule.IifName = info.vethName
	retRule.Table = 255 // 255 = local table
	retRule.Priority = retPriority

	if err := netlink.RuleAdd(retRule); err != nil {
		if !errors.Is(err, unix.EEXIST) {
			// Rollback the forward rule so we don't leave a stale entry on the host.
			if fwdAdded {
				if delErr := netlink.RuleDel(fwdRule); delErr != nil {
					log.Warnf("failed to rollback forward ip rule for branch ENI pod %s: %v", podIP, delErr)
				}
			}
			return fmt.Errorf("failed to add return ip rule for branch ENI veth %s: %w", info.vethName, err)
		}
	}
	log.Debugf("added branch ENI return rule: iif %s lookup local priority %d", info.vethName, retPriority)

	return nil
}

// delBranchENIRules removes the ip rules added by addBranchENIRules.
// Errors are logged but not returned since removal is best-effort
// (the pod may already be gone, rules may have been cleaned up, etc).
func delBranchENIRules(podIP netip.Addr, info *branchENIRoute) {
	ip := net.IP(podIP.AsSlice())
	family := unix.AF_INET
	bits := 32
	if podIP.Is6() {
		family = unix.AF_INET6
		bits = 128
	}

	fwdRule := netlink.NewRule()
	fwdRule.Family = family
	fwdRule.Dst = &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)}
	fwdRule.Table = info.table
	fwdRule.Priority = 512
	if err := netlink.RuleDel(fwdRule); err != nil {
		log.Debugf("failed to remove branch ENI forward rule for %s: %v", podIP, err)
	}

	retPriority := info.iifPriority - 1
	if retPriority < 0 {
		retPriority = 0
	}
	retRule := netlink.NewRule()
	retRule.Family = family
	retRule.IifName = info.vethName
	retRule.Table = 255
	retRule.Priority = retPriority
	if err := netlink.RuleDel(retRule); err != nil {
		log.Debugf("failed to remove branch ENI return rule for %s: %v", info.vethName, err)
	}

	log.Debugf("removed branch ENI rules for pod %s veth %s", podIP, info.vethName)
}
