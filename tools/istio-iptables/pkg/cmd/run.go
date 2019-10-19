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
	"fmt"
	"net"
	"os"
	"strings"

	"istio.io/istio/tools/istio-iptables/pkg/constants"

	"istio.io/pkg/env"

	"istio.io/istio/tools/istio-iptables/pkg/config"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

type NetworkRange struct {
	IsWildcard bool
	IPNets     []*net.IPNet
}

func split(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func separateV4V6(cidrList string) (NetworkRange, NetworkRange, error) {
	if cidrList == "*" {
		return NetworkRange{IsWildcard: true}, NetworkRange{IsWildcard: true}, nil
	}
	ipv6Ranges := NetworkRange{IsWildcard: false, IPNets: make([]*net.IPNet, 0)}
	ipv4Ranges := NetworkRange{IsWildcard: false, IPNets: make([]*net.IPNet, 0)}
	for _, ipRange := range split(cidrList) {
		ip, ipNet, err := net.ParseCIDR(ipRange)
		if err != nil {
			_, err = fmt.Fprintf(os.Stderr, "Ignoring error for bug compatibility with istio-iptables.sh: %s\n", err.Error())
			if err != nil {
				return ipv4Ranges, ipv6Ranges, err
			}
			continue
		}
		if ip.To4() != nil {
			ipv4Ranges.IPNets = append(ipv4Ranges.IPNets, ipNet)
		} else {
			ipv6Ranges.IPNets = append(ipv6Ranges.IPNets, ipNet)
		}
	}
	return ipv4Ranges, ipv6Ranges, nil
}

func logConfig(c *config.Config) {
	// Dump out our environment for debugging purposes.
	fmt.Println("Environment:")
	fmt.Println("------------")
	fmt.Println(fmt.Sprintf("ENVOY_PORT=%s", os.Getenv("ENVOY_PORT")))
	fmt.Println(fmt.Sprintf("INBOUND_CAPTURE_PORT=%s", os.Getenv("INBOUND_CAPTURE_PORT")))
	fmt.Println(fmt.Sprintf("ISTIO_INBOUND_INTERCEPTION_MODE=%s", os.Getenv("ISTIO_INBOUND_INTERCEPTION_MODE")))
	fmt.Println(fmt.Sprintf("ISTIO_INBOUND_TPROXY_MARK=%s", os.Getenv("ISTIO_INBOUND_TPROXY_MARK")))
	fmt.Println(fmt.Sprintf("ISTIO_INBOUND_TPROXY_ROUTE_TABLE=%s", os.Getenv("ISTIO_INBOUND_TPROXY_ROUTE_TABLE")))
	fmt.Println(fmt.Sprintf("ISTIO_INBOUND_PORTS=%s", os.Getenv("ISTIO_INBOUND_PORTS")))
	fmt.Println(fmt.Sprintf("ISTIO_LOCAL_EXCLUDE_PORTS=%s", os.Getenv("ISTIO_LOCAL_EXCLUDE_PORTS")))
	fmt.Println(fmt.Sprintf("ISTIO_SERVICE_CIDR=%s", os.Getenv("ISTIO_SERVICE_CIDR")))
	fmt.Println(fmt.Sprintf("ISTIO_SERVICE_EXCLUDE_CIDR=%s", os.Getenv("ISTIO_SERVICE_EXCLUDE_CIDR")))
	fmt.Println("")
	c.Print()
}

func handleInboundPortsInclude(ext dep.Dependencies, config *config.Config) {
	// Handling of inbound ports. Traffic will be redirected to Envoy, which will process and forward
	// to the local service. If not set, no inbound port will be intercepted by istio iptablesOrFail.
	var table string
	if config.InboundPortsInclude != "" {
		if config.InboundInterceptionMode == constants.TPROXY {
			// When using TPROXY, create a new chain for routing all inbound traffic to
			// Envoy. Any packet entering this chain gets marked with the ${INBOUND_TPROXY_MARK} mark,
			// so that they get routed to the loopback interface in order to get redirected to Envoy.
			// In the ISTIOINBOUND chain, '-j ISTIODIVERT' reroutes to the loopback
			// interface.
			// Mark all inbound packets.
			ext.RunOrFail(dep.IPTABLES, "-t", constants.MANGLE, "-N", constants.ISTIODIVERT)
			ext.RunOrFail(dep.IPTABLES, "-t", constants.MANGLE, "-A", constants.ISTIODIVERT, "-j", constants.MARK, "--set-mark", config.InboundTProxyMark)
			ext.RunOrFail(dep.IPTABLES, "-t", constants.MANGLE, "-A", constants.ISTIODIVERT, "-j", constants.ACCEPT)
			// Route all packets marked in chain ISTIODIVERT using routing table ${INBOUND_TPROXY_ROUTE_TABLE}.
			ext.RunOrFail(dep.IP, "-f", "inet", "rule", "add", "fwmark", config.InboundTProxyMark, "lookup", config.InboundTProxyRouteTable)
			// In routing table ${INBOUND_TPROXY_ROUTE_TABLE}, create a single default rule to route all traffic to
			// the loopback interface.
			err := ext.Run(dep.IP, "-f", "inet", "route", "add", "local", "default", "dev", "lo", "table", config.InboundTProxyRouteTable)
			if err != nil {
				ext.RunOrFail(dep.IP, "route", "show", "table", "all")
			}
			// Create a new chain for redirecting inbound traffic to the common Envoy
			// port.
			// In the ISTIOINBOUND chain, '-j RETURN' bypasses Envoy and
			// '-j ISTIOTPROXY' redirects to Envoy.
			ext.RunOrFail(dep.IPTABLES, "-t", constants.MANGLE, "-N", constants.ISTIOTPROXY)
			ext.RunOrFail(dep.IPTABLES, "-t", constants.MANGLE, "-A", constants.ISTIOTPROXY, "!", "-d", "127.0.0.1/32", "-p", constants.TCP, "-j", constants.TPROXY,
				"--tproxy-mark", config.InboundTProxyMark+"/0xffffffff", "--on-port", config.ProxyPort)
			table = constants.MANGLE
		} else {
			table = constants.NAT
		}
		ext.RunOrFail(dep.IPTABLES, "-t", table, "-N", constants.ISTIOINBOUND)
		ext.RunOrFail(dep.IPTABLES, "-t", table, "-A", constants.PREROUTING, "-p", constants.TCP, "-j", constants.ISTIOINBOUND)

		if config.InboundPortsInclude == "*" {
			// Makes sure SSH is not redirected
			ext.RunOrFail(dep.IPTABLES, "-t", table, "-A", constants.ISTIOINBOUND, "-p", constants.TCP, "--dport", "22", "-j", constants.RETURN)
			// Apply any user-specified port exclusions.
			if config.InboundPortsExclude != "" {
				for _, port := range split(config.InboundPortsExclude) {
					ext.RunOrFail(dep.IPTABLES, "-t", table, "-A", constants.ISTIOINBOUND, "-p", constants.TCP, "--dport", port, "-j", constants.RETURN)
				}
			}
			// Redirect remaining inbound traffic to Envoy.
			if config.InboundInterceptionMode == constants.TPROXY {
				// If an inbound packet belongs to an established socket, route it to the
				// loopback interface.
				err := ext.Run(dep.IPTABLES, "-t", constants.MANGLE, "-A", constants.ISTIOINBOUND, "-p", constants.TCP, "-m", "socket", "-j", constants.ISTIODIVERT)
				if err != nil {
					fmt.Println("No socket match support")
				}
				// Otherwise, it's a new connection. Redirect it using TPROXY.
				ext.RunOrFail(dep.IPTABLES, "-t", constants.MANGLE, "-A", constants.ISTIOINBOUND, "-p", constants.TCP, "-j", constants.ISTIOTPROXY)
			} else {
				ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-A", constants.ISTIOINBOUND, "-p", constants.TCP, "-j", constants.ISTIOINREDIRECT)
			}
		} else {
			// User has specified a non-empty list of ports to be redirected to Envoy.
			for _, port := range split(config.InboundPortsInclude) {
				if config.InboundInterceptionMode == constants.TPROXY {
					err := ext.Run(dep.IPTABLES, "-t", constants.MANGLE, "-A", constants.ISTIOINBOUND, "-p", constants.TCP,
						"--dport", port, "-m", "socket", "-j", constants.ISTIODIVERT)
					if err != nil {
						fmt.Println("No socket match support")
					}
					err = ext.Run(dep.IPTABLES, "-t", constants.MANGLE, "-A", constants.ISTIOINBOUND, "-p", constants.TCP, "--dport", port, "-m",
						"socket", "-j", constants.ISTIODIVERT)
					if err != nil {
						fmt.Println("No socket match support")
					}
					ext.RunOrFail(dep.IPTABLES, "-t", constants.MANGLE, "-A", constants.ISTIOINBOUND, "-p", constants.TCP, "--dport", port, "-j", constants.ISTIOTPROXY)
				} else {
					ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-A", constants.ISTIOINBOUND, "-p", constants.TCP, "--dport", port, "-j", constants.ISTIOINREDIRECT)
				}
			}
		}
	}
}

func handleInboundIpv6Rules(ext dep.Dependencies, config *config.Config, ipv6RangesExclude NetworkRange, ipv6RangesInclude NetworkRange) {
	// If ENABLE_INBOUND_IPV6 is unset (default unset), restrict IPv6 traffic.
	if config.EnableInboundIPv6s != nil {
		var table string
		// Create a new chain for redirecting outbound traffic to the common Envoy port.
		// In both chains, '-j RETURN' bypasses Envoy and '-j ISTIOREDIRECT'
		// redirects to Envoy.
		ext.RunOrFail(dep.IP6TABLES, "-t", constants.NAT, "-N", constants.ISTIOREDIRECT)
		ext.RunOrFail(dep.IP6TABLES, "-t", constants.NAT, "-A", constants.ISTIOREDIRECT, "-p", constants.TCP, "-j", constants.REDIRECT, "--to-port", config.ProxyPort)
		// Use this chain also for redirecting inbound traffic to the common Envoy port
		// when not using TPROXY.
		ext.RunOrFail(dep.IP6TABLES, "-t", constants.NAT, "-N", constants.ISTIOINREDIRECT)
		if config.InboundPortsInclude == "*" {
			ext.RunOrFail(dep.IP6TABLES, "-t", constants.NAT, "-A", constants.ISTIOINREDIRECT, "-p", constants.TCP, "-j",
				constants.REDIRECT, "--to-port", config.InboundCapturePort)
		} else {
			ext.RunOrFail(dep.IP6TABLES, "-t", constants.NAT, "-A", constants.ISTIOINREDIRECT, "-p", constants.TCP, "-j",
				constants.REDIRECT, "--to-port", config.ProxyPort)
		}
		// Handling of inbound ports. Traffic will be redirected to Envoy, which will process and forward
		// to the local service. If not set, no inbound port will be intercepted by istio iptablesOrFail.
		if config.InboundPortsInclude != "" {
			table = constants.NAT
			ext.RunOrFail(dep.IP6TABLES, "-t", table, "-N", constants.ISTIOINBOUND)
			ext.RunOrFail(dep.IP6TABLES, "-t", table, "-A", constants.PREROUTING, "-p", constants.TCP, "-j", constants.ISTIOINBOUND)

			if config.InboundPortsInclude == "*" {
				// Makes sure SSH is not redirected
				ext.RunOrFail(dep.IP6TABLES, "-t", table, "-A", constants.ISTIOINBOUND, "-p", constants.TCP, "--dport", "22", "-j", constants.RETURN)
				// Apply any user-specified port exclusions.
				if config.InboundPortsExclude != "" {
					for _, port := range split(config.InboundPortsExclude) {
						ext.RunOrFail(dep.IP6TABLES, "-t", table, "-A", constants.ISTIOINBOUND, "-p", constants.TCP, "--dport", port, "-j", constants.RETURN)
					}
				}
				// Redirect left inbound traffic
				ext.RunOrFail(dep.IP6TABLES, "-t", table, "-A", constants.ISTIOINBOUND, "-p", constants.TCP, "-j", constants.ISTIOINREDIRECT)
			} else {
				// User has specified a non-empty list of ports to be redirected to Envoy.
				for _, port := range split(config.InboundPortsInclude) {
					ext.RunOrFail(dep.IP6TABLES, "-t", constants.NAT, "-A", constants.ISTIOINBOUND, "-p", constants.TCP, "--dport", port, "-j", constants.ISTIOINREDIRECT)
				}
			}
		}
		// Create a new chain for selectively redirecting outbound packets to Envoy.
		ext.RunOrFail(dep.IP6TABLES, "-t", constants.NAT, "-N", constants.ISTIOOUTPUT)
		// Jump to the ISTIOOUTPUT chain from OUTPUT chain for all tcp traffic.
		ext.RunOrFail(dep.IP6TABLES, "-t", constants.NAT, "-A", constants.OUTPUT, "-p", constants.TCP, "-j", constants.ISTIOOUTPUT)
		// Apply port based exclusions. Must be applied before connections back to self are redirected.
		if config.OutboundPortsExclude != "" {
			for _, port := range split(config.OutboundPortsExclude) {
				ext.RunOrFail(dep.IP6TABLES, "-t", constants.NAT, "-A", constants.ISTIOOUTPUT, "-p", constants.TCP, "--dport", port, "-j", constants.RETURN)
			}
		}

		// ::6 is bind connect from inbound passthrough cluster
		ext.RunOrFail(dep.IP6TABLES, "-t", constants.NAT, "-A", constants.ISTIOOUTPUT, "-o", "lo", "-s", "::6/128", "-j", constants.RETURN)

		// Redirect app calls to back itself via Envoy when using the service VIP or endpoint
		// address, e.g. appN => Envoy (client) => Envoy (server) => appN.
		ext.RunOrFail(dep.IP6TABLES, "-t", constants.NAT, "-A", constants.ISTIOOUTPUT, "-o", "lo", "!", "-d", "::1/128", "-j", constants.ISTIOINREDIRECT)

		for _, uid := range split(config.ProxyUID) {
			// Avoid infinite loops. Don't redirect Envoy traffic directly back to
			// Envoy for non-loopback traffic.
			ext.RunOrFail(dep.IP6TABLES, "-t", constants.NAT, "-A", constants.ISTIOOUTPUT, "-m", "owner", "--uid-owner", uid, "-j", constants.RETURN)
		}

		for _, gid := range split(config.ProxyGID) {
			// Avoid infinite loops. Don't redirect Envoy traffic directly back to
			// Envoy for non-loopback traffic.
			ext.RunOrFail(dep.IP6TABLES, "-t", constants.NAT, "-A", constants.ISTIOOUTPUT, "-m", "owner", "--gid-owner", gid, "-j", constants.RETURN)
		}
		// Skip redirection for Envoy-aware applications and
		// container-to-container traffic both of which explicitly use
		// localhost.
		ext.RunOrFail(dep.IP6TABLES, "-t", constants.NAT, "-A", constants.ISTIOOUTPUT, "-d", "::1/128", "-j", constants.RETURN)
		// Apply outbound IPv6 exclusions. Must be applied before inclusions.
		for _, cidr := range ipv6RangesExclude.IPNets {
			ext.RunOrFail(dep.IP6TABLES, "-t", constants.NAT, "-A", constants.ISTIOOUTPUT, "-d", cidr.String(), "-j", constants.RETURN)
		}
		// Apply outbound IPv6 inclusions.
		if ipv6RangesInclude.IsWildcard {
			// Wildcard specified. Redirect all remaining outbound traffic to Envoy.
			ext.RunOrFail(dep.IP6TABLES, "-t", constants.NAT, "-A", constants.ISTIOOUTPUT, "-j", constants.ISTIOREDIRECT)
			for _, internalInterface := range split(config.KubevirtInterfaces) {
				ext.RunOrFail(dep.IP6TABLES, "-t", constants.NAT, "-I", constants.PREROUTING, "1", "-i", internalInterface, "-j", constants.RETURN)
			}
		} else if len(ipv6RangesInclude.IPNets) > 0 {
			// User has specified a non-empty list of cidrs to be redirected to Envoy.
			for _, cidr := range ipv6RangesInclude.IPNets {
				for _, internalInterface := range split(config.KubevirtInterfaces) {
					ext.RunOrFail(dep.IP6TABLES, "-t", constants.NAT, "-I", constants.PREROUTING, "1", "-i", internalInterface,
						"-d", cidr.String(), "-j", constants.ISTIOREDIRECT)
				}
				ext.RunOrFail(dep.IP6TABLES, "-t", constants.NAT, "-A", constants.ISTIOOUTPUT, "-d", cidr.String(), "-j", constants.ISTIOREDIRECT)
			}
			// All other traffic is not redirected.
			ext.RunOrFail(dep.IP6TABLES, "-t", constants.NAT, "-A", constants.ISTIOOUTPUT, "-j", constants.RETURN)
		}
	} else {
		// Drop all inbound traffic except established connections.
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-F", constants.INPUT)
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-A", constants.INPUT, "-m", "state", "--state", "ESTABLISHED", "-j", constants.ACCEPT)
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-A", constants.INPUT, "-i", "lo", "-d", "::1", "-j", constants.ACCEPT)
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-A", constants.INPUT, "-j", constants.REJECT)
	}
}

func handleInboundIpv4Rules(ext dep.Dependencies, config *config.Config, ipv4RangesInclude NetworkRange) {
	// Apply outbound IP inclusions.
	if ipv4RangesInclude.IsWildcard {
		// Wildcard specified. Redirect all remaining outbound traffic to Envoy.
		ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-A", constants.ISTIOOUTPUT, "-j", constants.ISTIOREDIRECT)
		for _, internalInterface := range split(config.KubevirtInterfaces) {
			ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-I", constants.PREROUTING, "1", "-i", internalInterface, "-j", constants.ISTIOREDIRECT)
		}
	} else if len(ipv4RangesInclude.IPNets) > 0 {
		// User has specified a non-empty list of cidrs to be redirected to Envoy.
		for _, cidr := range ipv4RangesInclude.IPNets {
			for _, internalInterface := range split(config.KubevirtInterfaces) {
				ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-I", constants.PREROUTING, "1", "-i", internalInterface,
					"-d", cidr.String(), "-j", constants.ISTIOREDIRECT)
			}
			ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-A", constants.ISTIOOUTPUT, "-d", cidr.String(), "-j", constants.ISTIOREDIRECT)
		}
		// All other traffic is not redirected.
		ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-A", constants.ISTIOOUTPUT, "-j", constants.RETURN)
	}
}

func run(config *config.Config) {
	var ext dep.Dependencies
	if config.DryRun {
		ext = &dep.StdoutStubDependencies{}
	} else {
		ext = &dep.RealDependencies{}
	}

	defer func() {
		ext.RunOrFail(dep.IPTABLESSAVE)
		ext.RunOrFail(dep.IP6TABLESSAVE)
	}()

	// TODO: more flexibility - maybe a whitelist of users to be captured for output instead of a blacklist.
	if config.ProxyUID == "" {
		usr, err := ext.LookupUser()
		var userID string
		// Default to the UID of ENVOY_USER and root
		if err != nil {
			userID = "1337"
		} else {
			userID = usr.Uid
		}
		// If ENVOY_UID is not explicitly defined (as it would be in k8s env), we add root to the list,
		// for ca agent.
		config.ProxyUID = userID + ",0"
	}

	// for TPROXY as its uid and gid are same
	if config.ProxyGID == "" {
		config.ProxyGID = config.ProxyUID
	}

	podIP, err := ext.GetLocalIP()
	if err != nil {
		panic(err)
	}
	// Check if pod's ip is ipv4 or ipv6, in case of ipv6 set variable
	// to program ip6tablesOrFail
	if podIP.To4() == nil {
		config.EnableInboundIPv6s = podIP
	}

	//
	// Since OUTBOUND_IP_RANGES_EXCLUDE could carry ipv4 and ipv6 ranges
	// need to split them in different arrays one for ipv4 and one for ipv6
	// in order to not to fail
	ipv4RangesExclude, ipv6RangesExclude, err := separateV4V6(config.OutboundIPRangesExclude)
	if err != nil {
		panic(err)
	}
	if ipv4RangesExclude.IsWildcard {
		panic("Invalid value for OUTBOUND_IP_RANGES_EXCLUDE")
	}
	// FixMe: Do we need similar check for ipv6RangesExclude as well ??

	ipv4RangesInclude, ipv6RangesInclude, err := separateV4V6(config.OutboundIPRangesInclude)
	if err != nil {
		panic(err)
	}

	logConfig(config)

	if config.EnableInboundIPv6s != nil {
		ext.RunOrFail(dep.IP, "-6", "addr", "add", "::6/128", "dev", "lo")
	}

	// Create a new chain for redirecting outbound traffic to the common Envoy port.
	// In both chains, '-j RETURN' bypasses Envoy and '-j ISTIOREDIRECT'
	// redirects to Envoy.
	ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-N", constants.ISTIOREDIRECT)
	ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-A", constants.ISTIOREDIRECT, "-p", constants.TCP, "-j", constants.REDIRECT, "--to-port", config.ProxyPort)
	// Use this chain also for redirecting inbound traffic to the common Envoy port
	// when not using TPROXY.
	ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-N", constants.ISTIOINREDIRECT)

	// PROXY_INBOUND_CAPTURE_PORT should be used only user explicitly set INBOUND_PORTS_INCLUDE to capture all
	if config.InboundPortsInclude == "*" {
		ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-A", constants.ISTIOINREDIRECT, "-p", constants.TCP, "-j", constants.REDIRECT,
			"--to-port", config.InboundCapturePort)
	} else {
		ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-A", constants.ISTIOINREDIRECT, "-p", constants.TCP, "-j", constants.REDIRECT,
			"--to-port", config.ProxyPort)
	}

	handleInboundPortsInclude(ext, config)

	// TODO: change the default behavior to not intercept any output - user may use http_proxy or another
	// iptablesOrFail wrapper (like ufw). Current default is similar with 0.1
	// Create a new chain for selectively redirecting outbound packets to Envoy.
	ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-N", constants.ISTIOOUTPUT)
	// Jump to the ISTIOOUTPUT chain from OUTPUT chain for all tcp traffic.
	ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-A", constants.OUTPUT, "-p", constants.TCP, "-j", constants.ISTIOOUTPUT)

	// Apply port based exclusions. Must be applied before connections back to self are redirected.
	if config.OutboundPortsExclude != "" {
		for _, port := range split(config.OutboundPortsExclude) {
			ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-A", constants.ISTIOOUTPUT, "-p", constants.TCP, "--dport", port, "-j", constants.RETURN)
		}
	}

	// 127.0.0.6 is bind connect from inbound passthrough cluster
	ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-A", constants.ISTIOOUTPUT, "-o", "lo", "-s", "127.0.0.6/32", "-j", constants.RETURN)

	if env.RegisterStringVar("DISABLE_REDIRECTION_ON_LOCAL_LOOPBACK", "", "").Get() == "" {
		// Redirect app calls back to itself via Envoy when using the service VIP or endpoint
		// address, e.g. appN => Envoy (client) => Envoy (server) => appN.
		ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-A", constants.ISTIOOUTPUT, "-o", "lo", "!", "-d", "127.0.0.1/32", "-j", constants.ISTIOINREDIRECT)
	}

	for _, uid := range split(config.ProxyUID) {
		// Avoid infinite loops. Don't redirect Envoy traffic directly back to
		// Envoy for non-loopback traffic.
		ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-A", constants.ISTIOOUTPUT, "-m", "owner", "--uid-owner", uid, "-j", constants.RETURN)
	}

	for _, gid := range split(config.ProxyGID) {
		// Avoid infinite loops. Don't redirect Envoy traffic directly back to
		// Envoy for non-loopback traffic.
		ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-A", constants.ISTIOOUTPUT, "-m", "owner", "--gid-owner", gid, "-j", constants.RETURN)
	}
	// Skip redirection for Envoy-aware applications and
	// container-to-container traffic both of which explicitly use
	// localhost.
	ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-A", constants.ISTIOOUTPUT, "-d", "127.0.0.1/32", "-j", constants.RETURN)
	// Apply outbound IPv4 exclusions. Must be applied before inclusions.
	for _, cidr := range ipv4RangesExclude.IPNets {
		ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-A", constants.ISTIOOUTPUT, "-d", cidr.String(), "-j", constants.RETURN)
	}

	for _, internalInterface := range split(config.KubevirtInterfaces) {
		ext.RunOrFail(dep.IPTABLES, "-t", constants.NAT, "-I", constants.PREROUTING, "1", "-i", internalInterface, "-j", constants.RETURN)
	}

	handleInboundIpv4Rules(ext, config, ipv4RangesInclude)
	handleInboundIpv6Rules(ext, config, ipv6RangesExclude, ipv6RangesInclude)
}
