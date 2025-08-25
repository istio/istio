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

package iptables

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"istio.io/istio/pkg/ptr"
	"istio.io/istio/tools/common/config"
)

func AddInpodMarkIPRule(cfg *IptablesConfig) error {
	err := forEachInpodMarkIPRule(cfg, netlink.RuleAdd)
	if errors.Is(err, unix.EEXIST) {
		log.Debugf("Ignoring exists error adding inpod mark ip rule: %v", err)
		return nil
	}
	return err
}

func DelInpodMarkIPRule(cfg *IptablesConfig) error {
	return forEachInpodMarkIPRule(cfg, netlink.RuleDel)
}

func forEachInpodMarkIPRule(cfg *IptablesConfig, f func(*netlink.Rule) error) error {
	var rules []*netlink.Rule
	families := []int{unix.AF_INET}
	if cfg.EnableIPv6 {
		families = append(families, unix.AF_INET6)
	}
	for _, family := range families {
		// Equiv:
		// ip rule add fwmark 0x111/0xfff pref 32764 lookup 100
		//
		// Adds in-pod rules for marking packets with the istio-specific TPROXY mark.
		// A very similar mechanism is used for sidecar TPROXY.
		//
		// TODO largely identical/copied from tools/istio-iptables/pkg/capture/run_linux.go
		inpodMarkRule := netlink.NewRule()
		inpodMarkRule.Family = family
		inpodMarkRule.Table = RouteTableInbound
		inpodMarkRule.Mark = InpodTProxyMark
		inpodMarkRule.Mask = ptr.Of(uint32(InpodTProxyMask))
		inpodMarkRule.Priority = 32764
		rules = append(rules, inpodMarkRule)
	}

	for _, rule := range rules {
		log.Debugf("processing netlink rule: %+v", rule)
		if err := f(rule); err != nil {
			return fmt.Errorf("failed to configure netlink rule: %w", err)
		}
	}

	return nil
}

func AddLoopbackRoutes(cfg *IptablesConfig) error {
	return forEachLoopbackRoute(cfg, "add", netlink.RouteReplace)
}

func DelLoopbackRoutes(cfg *IptablesConfig) error {
	return forEachLoopbackRoute(cfg, "remove", netlink.RouteDel)
}

const ipv6DisabledLo = "/proc/sys/net/ipv6/conf/lo/disable_ipv6"

func ReadSysctl(key string) (string, error) {
	data, err := os.ReadFile(key)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func forEachLoopbackRoute(cfg *IptablesConfig, operation string, f func(*netlink.Route) error) error {
	loopbackLink, err := config.LinkByNameWithRetries("lo")
	if err != nil {
		return fmt.Errorf("failed to find 'lo' link: %v", err)
	}

	// Set up netlink routes for localhost
	cidrs := []string{"0.0.0.0/0"}
	if cfg.EnableIPv6 {
		// IPv6 may be enabled, but only partially
		v, err := ReadSysctl(ipv6DisabledLo)
		if v != "1" {
			// If we got an error, we will proceed. Maybe it will work anyways
			if err != nil {
				log.Warnf("attempted to read %q got error: %v; attempting to continue", ipv6DisabledLo, err)
			}
			cidrs = append(cidrs, "0::0/0")
		} else {
			log.Debugf("IPv6 is enabled, but the loopback interface has IPv6 disabled; skipping")
		}
	}
	for _, fullCIDR := range cidrs {
		_, localhostDst, err := net.ParseCIDR(fullCIDR)
		if err != nil {
			return fmt.Errorf("parse CIDR: %v", err)
		}

		netlinkRoutes := []*netlink.Route{
			// In routing table ${INBOUND_TPROXY_ROUTE_TABLE}, create a single default rule to route all traffic to
			// the loopback interface.
			// Equiv: "ip route add local 0.0.0.0/0 dev lo table 100"
			{
				Dst:       localhostDst,
				Scope:     netlink.SCOPE_HOST,
				Type:      unix.RTN_LOCAL,
				Table:     RouteTableInbound,
				LinkIndex: loopbackLink.Attrs().Index,
			},
		}

		for _, route := range netlinkRoutes {
			log.Debugf("processing netlink route: %+v", route)
			if err := f(route); err != nil {
				return fmt.Errorf("failed to %v route (%+v): %v", operation, route, err)
			}
		}
	}
	return nil
}
