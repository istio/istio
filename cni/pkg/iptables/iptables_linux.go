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

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func AddInpodMarkIPRule(cfg *Config) error {
	err := forEachInpodMarkIPRule(cfg, netlink.RuleAdd)
	if errors.Is(err, unix.EEXIST) {
		log.Debugf("Ignoring exists error adding inpod mark ip rule: %v", err)
		return nil
	}
	return err
}

func DelInpodMarkIPRule(cfg *Config) error {
	return forEachInpodMarkIPRule(cfg, netlink.RuleDel)
}

func forEachInpodMarkIPRule(cfg *Config, f func(*netlink.Rule) error) error {
	var rules []*netlink.Rule
	families := []int{unix.AF_INET}
	if cfg.EnableInboundIPv6 {
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
		inpodMarkRule.Mask = InpodTProxyMask
		inpodMarkRule.Priority = 32764
		rules = append(rules, inpodMarkRule)
	}

	for _, rule := range rules {
		log.Debugf("Iterating netlink rule : %+v", rule)
		if err := f(rule); err != nil {
			return fmt.Errorf("failed to configure netlink rule: %w", err)
		}
	}

	return nil
}

func AddLoopbackRoutes(cfg *Config) error {
	return forEachLoopbackRoute(cfg, netlink.RouteReplace)
}

func DelLoopbackRoutes(cfg *Config) error {
	return forEachLoopbackRoute(cfg, netlink.RouteDel)
}

func forEachLoopbackRoute(cfg *Config, f func(*netlink.Route) error) error {
	loopbackLink, err := netlink.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("failed to find 'lo' link: %v", err)
	}

	// Set up netlink routes for localhost
	cidrs := []string{"0.0.0.0/0"}
	if cfg.EnableInboundIPv6 {
		cidrs = append(cidrs, "0::0/0")
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
			log.Debugf("Iterating netlink route : %+v", route)
			if err := f(route); err != nil {
				log.Errorf("Failed to add netlink route : %+v", route)
				return fmt.Errorf("failed to add route: %v", err)
			}
		}
	}
	return nil
}
