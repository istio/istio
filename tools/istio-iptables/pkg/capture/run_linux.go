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
package capture

import (
	"fmt"
	"net"
	"strconv"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tools/istio-iptables/pkg/config"
)

// configureTProxyRoutes configures ip firewall rules to enable TPROXY support.
// See https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/original_src_filter
func configureTProxyRoutes(cfg *config.Config) error {
	if cfg.InboundPortsInclude != "" {
		if cfg.InboundInterceptionMode == "TPROXY" {
			link, err := config.LinkByNameWithRetries("lo")
			if err != nil {
				return fmt.Errorf("failed to find 'lo' link: %v", err)
			}
			tproxyTable, err := strconv.Atoi(cfg.InboundTProxyRouteTable)
			if err != nil {
				return fmt.Errorf("failed to parse InboundTProxyRouteTable: %v", err)
			}
			tproxyMark, err := strconv.Atoi(cfg.InboundTProxyMark)
			if err != nil {
				return fmt.Errorf("failed to parse InboundTProxyMark: %v", err)
			}
			// Route all packets marked in chain ISTIODIVERT using routing table ${INBOUND_TPROXY_ROUTE_TABLE}.
			// Equivalent to `ip rule add fwmark <tproxyMark> lookup <tproxyTable>`
			families := []int{unix.AF_INET}
			if cfg.EnableIPv6 {
				families = append(families, unix.AF_INET6)
			}
			for _, family := range families {
				r := netlink.NewRule()
				r.Family = family
				r.Table = tproxyTable
				r.Mark = uint32(tproxyMark)
				if err := netlink.RuleAdd(r); err != nil {
					return fmt.Errorf("failed to configure netlink rule: %v", err)
				}
			}
			// In routing table ${INBOUND_TPROXY_ROUTE_TABLE}, create a single default rule to route all traffic to
			// the loopback interface.
			// Equivalent to `ip route add local default dev lo table <table>`
			cidrs := []string{"0.0.0.0/0"}
			if cfg.EnableIPv6 {
				cidrs = append(cidrs, "0::0/0")
			}
			for _, fullCIDR := range cidrs {
				_, dst, err := net.ParseCIDR(fullCIDR)
				if err != nil {
					return fmt.Errorf("parse CIDR: %v", err)
				}

				if err := netlink.RouteAdd(&netlink.Route{
					Dst:       dst,
					Scope:     netlink.SCOPE_HOST,
					Type:      unix.RTN_LOCAL,
					Table:     tproxyTable,
					LinkIndex: link.Attrs().Index,
				}); ignoreExists(err) != nil {
					return fmt.Errorf("failed to add route: %v", err)
				}
			}
		}
	}
	return nil
}

func ConfigureRoutes(cfg *config.Config) error {
	if cfg.DryRun {
		log.Infof("skipping configuring routes due to dry run mode")
		return nil
	}
	if err := configureIPv6Addresses(cfg); err != nil {
		return err
	}
	if err := configureTProxyRoutes(cfg); err != nil {
		return err
	}
	return nil
}

// configureIPv6Addresses sets up a new IP address on local interface. This is used as the source IP
// for inbound traffic to distinguish traffic we want to capture vs traffic we do not. This is needed
// for IPv6 but not IPv4, as IPv4 defaults to `netmask 255.0.0.0`, which allows binding to addresses
// in the 127.x.y.z range, while IPv6 defaults to `prefixlen 128` which allows binding only to ::1.
// Equivalent to `ip -6 addr add "::6/128" dev lo`
func configureIPv6Addresses(cfg *config.Config) error {
	if !cfg.EnableIPv6 {
		return nil
	}
	link, err := config.LinkByNameWithRetries("lo")
	if err != nil {
		return fmt.Errorf("failed to find 'lo' link: %v", err)
	}
	// Setup a new IP address on local interface. This is used as the source IP for inbound traffic
	// to distinguish traffic we want to capture vs traffic we do not.
	// Equivalent to `ip -6 addr add "::6/128" dev lo`
	address := &net.IPNet{IP: net.ParseIP("::6"), Mask: net.CIDRMask(128, 128)}
	addr := &netlink.Addr{IPNet: address}

	err = netlink.AddrAdd(link, addr)
	if ignoreExists(err) != nil {
		return fmt.Errorf("failed to add IPv6 inbound address: %v", err)
	}
	log.Infof("Added ::6 address on the loopback iface")
	return nil
}
