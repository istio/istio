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

package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"strings"

	"istio.io/pkg/env"

	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
)

// Constants used in iptables commands
const (
	MANGLE = "mangle"
	NAT    = "nat"

	TCP = "tcp"

	TPROXY          = "TPROXY"
	PREROUTING      = "PREROUTING"
	RETURN          = "RETURN"
	ACCEPT          = "ACCEPT"
	REJECT          = "REJECT"
	INPUT           = "INPUT"
	OUTPUT          = "OUTPUT"
	REDIRECT        = "REDIRECT"
	MARK            = "MARK"
	ISTIOOUTPUT     = "ISTIO_OUTPUT"
	ISTIOINBOUND    = "ISTIO_INBOUND"
	ISTIODIVERT     = "ISTIO_DIVERT"
	ISTIOTPROXY     = "ISTIO_TPROXY"
	ISTIOREDIRECT   = "ISTIO_REDIRECT"
	ISTIOINREDIRECT = "ISTIO_IN_REDIRECT"
)

type NetworkRange struct {
	IsWildcard bool
	IPNets     []*net.IPNet
}

type Config struct {
	ProxyPort               string
	InboundCapturePort      string
	ProxyUID                string
	ProxyGID                string
	InboundInterceptionMode string
	InboundTProxyMark       string
	InboundTProxyRouteTable string
	InboundPortsInclude     string
	InboundPortsExclude     string
	OutboundPortsExclude    string
	OutboundIPRangesInclude string
	OutboundIPRangesExclude string
	KubevirtInterfaces      string
	DryRun                  bool
	EnableInboundIPv6s      net.IP
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

func constructConfigFromFlags(args []string, flagSet *flag.FlagSet) *Config {
	config := &Config{}
	proxyUID := ""
	proxyGID := ""
	kubevirtInterfaces := ""

	inboundInterceptionMode := env.RegisterStringVar("ISTIO_INBOUND_INTERCEPTION_MODE", "", "").Get()
	config.InboundTProxyMark = env.RegisterStringVar("ISTIO_INBOUND_TPROXY_MARK", "1337", "").Get()
	config.InboundTProxyRouteTable = env.RegisterStringVar("ISTIOINBOUND_TPROXY_ROUTE_TABLE", "133", "").Get()
	inboundPortsInclude := env.RegisterStringVar("ISTIO_INBOUND_PORTS", "", "").Get()
	inboundPortsExclude := env.RegisterStringVar("ISTIO_LOCAL_EXCLUDE_PORTS", "", "").Get()
	outboundIPRangesInclude := env.RegisterStringVar("ISTIO_SERVICE_CIDR", "", "").Get()
	outboundIPRangesExclude := env.RegisterStringVar("ISTIO_SERVICE_EXCLUDE_CIDR", "", "").Get()
	outboundPortsExclude := env.RegisterStringVar("ISTIO_LOCAL_OUTBOUND_PORTS_EXCLUDE", "", "").Get()
	proxyPort := env.RegisterStringVar("ENVOY_PORT", "15001", "").Get()
	inboundCapturePort := env.RegisterStringVar("INBOUND_CAPTURE_PORT", "15006", "").Get()

	flagSet.StringVar(&config.ProxyPort, "p", proxyPort,
		"Specify the envoy port to which redirect all TCP traffic (default $ENVOY_PORT = 15001)")
	flagSet.StringVar(&config.InboundCapturePort, "z", inboundCapturePort,
		"Port to which all inbound TCP traffic to the pod/VM should be redirected to (default $INBOUND_CAPTURE_PORT = 15006)")
	flagSet.StringVar(&config.ProxyUID, "u", proxyUID,
		"Specify the UID of the user for which the redirection is not applied. Typically, this is the UID of the proxy container")
	flagSet.StringVar(&config.ProxyGID, "g", proxyGID,
		"Specify the GID of the user for which the redirection is not applied. (same default value as -u param)")
	flagSet.StringVar(&config.InboundInterceptionMode, "m", inboundInterceptionMode,
		"The mode used to redirect inbound connections to Envoy, either \"REDIRECT\" or \"TPROXY\"")
	flagSet.StringVar(&config.InboundPortsInclude, "b", inboundPortsInclude,
		"Comma separated list of inbound ports for which traffic is to be redirected to Envoy (optional). "+
			"The wildcard character \"*\" can be used to configure redirection for all ports. An empty list will disable")
	flagSet.StringVar(&config.InboundPortsExclude, "d", inboundPortsExclude,
		"Comma separated list of inbound ports to be excluded from redirection to Envoy (optional). "+
			"Only applies  when all inbound traffic (i.e. \"*\") is being redirected (default to $ISTIO_LOCAL_EXCLUDE_PORTS)")
	flagSet.StringVar(&config.OutboundIPRangesInclude, "i", outboundIPRangesInclude,
		"Comma separated list of IP ranges in CIDR form to redirect to envoy (optional). "+
			"The wildcard character \"*\" can be used to redirect all outbound traffic. An empty list will disable all outbound")
	flagSet.StringVar(&config.OutboundIPRangesExclude, "x", outboundIPRangesExclude,
		"Comma separated list of IP ranges in CIDR form to be excluded from redirection. "+
			"Only applies when all  outbound traffic (i.e. \"*\") is being redirected (default to $ISTIO_SERVICE_EXCLUDE_CIDR)")
	flagSet.StringVar(&config.OutboundPortsExclude, "o", outboundPortsExclude,
		"Comma separated list of outbound ports to be excluded from redirection to Envoy (optional")
	flagSet.StringVar(&config.KubevirtInterfaces, "k", kubevirtInterfaces,
		"Comma separated list of virtual interfaces whose inbound traffic (from VM) will be treated as outbound (optional)")
	flagSet.BoolVar(&config.DryRun, "dryRun", false, "Do not call any external dependencies like iptables")

	err := flagSet.Parse(args)
	if err != nil {
		os.Exit(1)
	}
	return config
}

func removeOldChains(ext dep.Dependencies) {
	// Remove the old chains, to generate new configs.
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", NAT, "-D", PREROUTING, "-p", TCP, "-j", ISTIOINBOUND)
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", MANGLE, "-D", PREROUTING, "-p", TCP, "-j", ISTIOINBOUND)
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", NAT, "-D", OUTPUT, "-p", TCP, "-j", ISTIOOUTPUT)
	// Flush and delete the istio chains.
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", NAT, "-F", ISTIOOUTPUT)
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", NAT, "-X", ISTIOOUTPUT)
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", NAT, "-F", ISTIOINBOUND)
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", NAT, "-X", ISTIOINBOUND)
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", MANGLE, "-F", ISTIOINBOUND)
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", MANGLE, "-X", ISTIOINBOUND)
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", MANGLE, "-F", ISTIODIVERT)
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", MANGLE, "-X", ISTIODIVERT)
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", MANGLE, "-F", ISTIOTPROXY)
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", MANGLE, "-X", ISTIOTPROXY)
	// Must be last, the others refer to it
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", NAT, "-F", ISTIOREDIRECT)
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", NAT, "-X", ISTIOREDIRECT)
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", NAT, "-F", ISTIOINREDIRECT)
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", NAT, "-X", ISTIOINREDIRECT)
}

func dumpEnvVariables(config *Config) {
	// Dump out our environment for debugging purposes.
	fmt.Println("Environment:")
	fmt.Println("------------")
	fmt.Printf("ENVOY_PORT=%s\n", os.Getenv("ENVOY_PORT"))
	fmt.Printf("INBOUND_CAPTURE_PORT=%s\n", os.Getenv("INBOUND_CAPTURE_PORT"))
	fmt.Printf("ISTIO_INBOUND_INTERCEPTION_MODE=%s\n", os.Getenv("ISTIO_INBOUND_INTERCEPTION_MODE"))
	fmt.Printf("ISTIO_INBOUND_TPROXY_MARK=%s\n", os.Getenv("ISTIO_INBOUND_TPROXY_MARK"))
	fmt.Printf("ISTIO_INBOUND_TPROXY_ROUTE_TABLE=%s\n", os.Getenv("ISTIO_INBOUND_TPROXY_ROUTE_TABLE"))
	fmt.Printf("ISTIO_INBOUND_PORTS=%s\n", os.Getenv("ISTIO_INBOUND_PORTS"))
	fmt.Printf("ISTIO_LOCAL_EXCLUDE_PORTS=%s\n", os.Getenv("ISTIO_LOCAL_EXCLUDE_PORTS"))
	fmt.Printf("ISTIO_SERVICE_CIDR=%s\n", os.Getenv("ISTIO_SERVICE_CIDR"))
	fmt.Printf("ISTIO_SERVICE_EXCLUDE_CIDR=%s\n", os.Getenv("ISTIO_SERVICE_EXCLUDE_CIDR"))
	fmt.Println("")
	fmt.Println("Variables:")
	fmt.Println("----------")
	fmt.Printf("PROXY_PORT=%s\n", config.ProxyPort)
	fmt.Printf("PROXY_INBOUND_CAPTURE_PORT=%s\n", config.InboundCapturePort)
	fmt.Printf("PROXY_UID=%s\n", config.ProxyUID)
	fmt.Printf("INBOUND_INTERCEPTION_MODE=%s\n", config.InboundInterceptionMode)
	fmt.Printf("INBOUND_TPROXY_MARK=%s\n", config.InboundTProxyMark)
	fmt.Printf("INBOUND_TPROXY_ROUTE_TABLE=%s\n", config.InboundTProxyRouteTable)
	fmt.Printf("INBOUND_PORTS_INCLUDE=%s\n", config.InboundPortsInclude)
	fmt.Printf("INBOUND_PORTS_EXCLUDE=%s\n", config.InboundPortsExclude)
	fmt.Printf("OUTBOUND_IP_RANGES_INCLUDE=%s\n", config.OutboundIPRangesInclude)
	fmt.Printf("OUTBOUND_IP_RANGES_EXCLUDE=%s\n", config.OutboundIPRangesExclude)
	fmt.Printf("OUTBOUND_PORTS_EXCLUDE=%s\n", config.OutboundPortsExclude)
	fmt.Printf("KUBEVIRT_INTERFACES=%s\n", config.KubevirtInterfaces)
	// Print "" instead of <nil> to produce same output as script and satisfy golden tests
	if config.EnableInboundIPv6s == nil {
		fmt.Printf("ENABLE_INBOUND_IPV6=%s\n", "")
	} else {
		fmt.Printf("ENABLE_INBOUND_IPV6=%s\n", config.EnableInboundIPv6s)
	}
	fmt.Println("")
}

func handleInboundPortsInclude(ext dep.Dependencies, config *Config) {
	// Handling of inbound ports. Traffic will be redirected to Envoy, which will process and forward
	// to the local service. If not set, no inbound port will be intercepted by istio iptablesOrFail.
	var table string
	if config.InboundPortsInclude != "" {
		if config.InboundInterceptionMode == TPROXY {
			// When using TPROXY, create a new chain for routing all inbound traffic to
			// Envoy. Any packet entering this chain gets marked with the ${INBOUND_TPROXY_MARK} mark,
			// so that they get routed to the loopback interface in order to get redirected to Envoy.
			// In the ISTIOINBOUND chain, '-j ISTIODIVERT' reroutes to the loopback
			// interface.
			// Mark all inbound packets.
			ext.RunOrFail(dep.IPTABLES, "-t", MANGLE, "-N", ISTIODIVERT)
			ext.RunOrFail(dep.IPTABLES, "-t", MANGLE, "-A", ISTIODIVERT, "-j", MARK, "--set-mark", config.InboundTProxyMark)
			ext.RunOrFail(dep.IPTABLES, "-t", MANGLE, "-A", ISTIODIVERT, "-j", ACCEPT)
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
			ext.RunOrFail(dep.IPTABLES, "-t", MANGLE, "-N", ISTIOTPROXY)
			ext.RunOrFail(dep.IPTABLES, "-t", MANGLE, "-A", ISTIOTPROXY, "!", "-d", "127.0.0.1/32", "-p", TCP, "-j", TPROXY,
				"--tproxy-mark", config.InboundTProxyMark+"/0xffffffff", "--on-port", config.ProxyPort)
			table = MANGLE
		} else {
			table = NAT
		}
		ext.RunOrFail(dep.IPTABLES, "-t", table, "-N", ISTIOINBOUND)
		ext.RunOrFail(dep.IPTABLES, "-t", table, "-A", PREROUTING, "-p", TCP, "-j", ISTIOINBOUND)

		if config.InboundPortsInclude == "*" {
			// Makes sure SSH is not redirected
			ext.RunOrFail(dep.IPTABLES, "-t", table, "-A", ISTIOINBOUND, "-p", TCP, "--dport", "22", "-j", RETURN)
			// Apply any user-specified port exclusions.
			if config.InboundPortsExclude != "" {
				for _, port := range split(config.InboundPortsExclude) {
					ext.RunOrFail(dep.IPTABLES, "-t", table, "-A", ISTIOINBOUND, "-p", TCP, "--dport", port, "-j", RETURN)
				}
			}
			// Redirect remaining inbound traffic to Envoy.
			if config.InboundInterceptionMode == TPROXY {
				// If an inbound packet belongs to an established socket, route it to the
				// loopback interface.
				err := ext.Run(dep.IPTABLES, "-t", MANGLE, "-A", ISTIOINBOUND, "-p", TCP, "-m", "socket", "-j", ISTIODIVERT)
				if err != nil {
					fmt.Println("No socket match support")
				}
				// Otherwise, it's a new connection. Redirect it using TPROXY.
				ext.RunOrFail(dep.IPTABLES, "-t", MANGLE, "-A", ISTIOINBOUND, "-p", TCP, "-j", ISTIOTPROXY)
			} else {
				ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-A", ISTIOINBOUND, "-p", TCP, "-j", ISTIOINREDIRECT)
			}
		} else {
			// User has specified a non-empty list of ports to be redirected to Envoy.
			for _, port := range split(config.InboundPortsInclude) {
				if config.InboundInterceptionMode == TPROXY {
					err := ext.Run(dep.IPTABLES, "-t", MANGLE, "-A", ISTIOINBOUND, "-p", TCP, "--dport", port, "-m", "socket", "-j", ISTIODIVERT)
					if err != nil {
						fmt.Println("No socket match support")
					}
					err = ext.Run(dep.IPTABLES, "-t", MANGLE, "-A", ISTIOINBOUND, "-p", TCP, "--dport", port, "-m", "socket", "-j", ISTIODIVERT)
					if err != nil {
						fmt.Println("No socket match support")
					}
					ext.RunOrFail(dep.IPTABLES, "-t", MANGLE, "-A", ISTIOINBOUND, "-p", TCP, "--dport", port, "-j", ISTIOTPROXY)
				} else {
					ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-A", ISTIOINBOUND, "-p", TCP, "--dport", port, "-j", ISTIOINREDIRECT)
				}
			}
		}
	}
}

func handleInboundIpv6Rules(ext dep.Dependencies, config *Config, ipv6RangesExclude NetworkRange, ipv6RangesInclude NetworkRange) {
	// If ENABLE_INBOUND_IPV6 is unset (default unset), restrict IPv6 traffic.
	if config.EnableInboundIPv6s != nil {
		var table string
		// Remove the old chains, to generate new configs.
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", NAT, "-D", PREROUTING, "-p", TCP, "-j", ISTIOINBOUND)
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", MANGLE, "-D", PREROUTING, "-p", TCP, "-j", ISTIOINBOUND)
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", NAT, "-D", OUTPUT, "-p", TCP, "-j", ISTIOOUTPUT)
		// Flush and delete the istio chains.
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", NAT, "-F", ISTIOOUTPUT)
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", NAT, "-X", ISTIOOUTPUT)
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", NAT, "-F", ISTIOINBOUND)
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", NAT, "-X", ISTIOINBOUND)
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", MANGLE, "-F", ISTIOINBOUND)
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", MANGLE, "-X", ISTIOINBOUND)
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", MANGLE, "-F", ISTIODIVERT)
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", MANGLE, "-X", ISTIODIVERT)
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", MANGLE, "-F", ISTIOTPROXY)
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", MANGLE, "-X", ISTIOTPROXY)

		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", NAT, "-F", ISTIOREDIRECT)
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", NAT, "-X", ISTIOREDIRECT)
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", NAT, "-F", ISTIOINREDIRECT)
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", NAT, "-X", ISTIOINREDIRECT)
		// Create a new chain for redirecting outbound traffic to the common Envoy port.
		// In both chains, '-j RETURN' bypasses Envoy and '-j ISTIOREDIRECT'
		// redirects to Envoy.
		ext.RunOrFail(dep.IP6TABLES, "-t", NAT, "-N", ISTIOREDIRECT)
		ext.RunOrFail(dep.IP6TABLES, "-t", NAT, "-A", ISTIOREDIRECT, "-p", TCP, "-j", REDIRECT, "--to-port", config.ProxyPort)
		// Use this chain also for redirecting inbound traffic to the common Envoy port
		// when not using TPROXY.
		ext.RunOrFail(dep.IP6TABLES, "-t", NAT, "-N", ISTIOINREDIRECT)
		if config.InboundPortsInclude == "*" {
			ext.RunOrFail(dep.IP6TABLES, "-t", NAT, "-A", ISTIOINREDIRECT, "-p", TCP, "-j", REDIRECT, "--to-port", config.InboundCapturePort)
		} else {
			ext.RunOrFail(dep.IP6TABLES, "-t", NAT, "-A", ISTIOINREDIRECT, "-p", TCP, "-j", REDIRECT, "--to-port", config.ProxyPort)
		}
		// Handling of inbound ports. Traffic will be redirected to Envoy, which will process and forward
		// to the local service. If not set, no inbound port will be intercepted by istio iptablesOrFail.
		if config.InboundPortsInclude != "" {
			table = NAT
			ext.RunOrFail(dep.IP6TABLES, "-t", table, "-N", ISTIOINBOUND)
			ext.RunOrFail(dep.IP6TABLES, "-t", table, "-A", PREROUTING, "-p", TCP, "-j", ISTIOINBOUND)

			if config.InboundPortsInclude == "*" {
				// Makes sure SSH is not redirected
				ext.RunOrFail(dep.IP6TABLES, "-t", table, "-A", ISTIOINBOUND, "-p", TCP, "--dport", "22", "-j", RETURN)
				// Apply any user-specified port exclusions.
				if config.InboundPortsExclude != "" {
					for _, port := range split(config.InboundPortsExclude) {
						ext.RunOrFail(dep.IP6TABLES, "-t", table, "-A", ISTIOINBOUND, "-p", TCP, "--dport", port, "-j", RETURN)
					}
				}
			} else {
				// User has specified a non-empty list of ports to be redirected to Envoy.
				for _, port := range split(config.InboundPortsInclude) {
					ext.RunOrFail(dep.IP6TABLES, "-t", NAT, "-A", ISTIOINBOUND, "-p", TCP, "--dport", port, "-j", ISTIOINREDIRECT)
				}
			}
		}
		// Create a new chain for selectively redirecting outbound packets to Envoy.
		ext.RunOrFail(dep.IP6TABLES, "-t", NAT, "-N", ISTIOOUTPUT)
		// Jump to the ISTIOOUTPUT chain from OUTPUT chain for all tcp traffic.
		ext.RunOrFail(dep.IP6TABLES, "-t", NAT, "-A", OUTPUT, "-p", TCP, "-j", ISTIOOUTPUT)
		// Apply port based exclusions. Must be applied before connections back to self are redirected.
		if config.OutboundPortsExclude != "" {
			for _, port := range split(config.OutboundPortsExclude) {
				ext.RunOrFail(dep.IP6TABLES, "-t", NAT, "-A", ISTIOOUTPUT, "-p", TCP, "--dport", port, "-j", RETURN)
			}
		}

		// ::6 is bind connect from inbound passthrough cluster
		ext.RunOrFail(dep.IP6TABLES, "-t", NAT, "-A", ISTIOOUTPUT, "-o", "lo", "-s", "::6/128", "-j", RETURN)

		// Redirect app calls to back itself via Envoy when using the service VIP or endpoint
		// address, e.g. appN => Envoy (client) => Envoy (server) => appN.
		ext.RunOrFail(dep.IP6TABLES, "-t", NAT, "-A", ISTIOOUTPUT, "-o", "lo", "!", "-d", "::1/128", "-j", ISTIOINREDIRECT)

		for _, uid := range split(config.ProxyUID) {
			// Avoid infinite loops. Don't redirect Envoy traffic directly back to
			// Envoy for non-loopback traffic.
			ext.RunOrFail(dep.IP6TABLES, "-t", NAT, "-A", ISTIOOUTPUT, "-m", "owner", "--uid-owner", uid, "-j", RETURN)
		}

		for _, gid := range split(config.ProxyGID) {
			// Avoid infinite loops. Don't redirect Envoy traffic directly back to
			// Envoy for non-loopback traffic.
			ext.RunOrFail(dep.IP6TABLES, "-t", NAT, "-A", ISTIOOUTPUT, "-m", "owner", "--gid-owner", gid, "-j", RETURN)
		}
		// Skip redirection for Envoy-aware applications and
		// container-to-container traffic both of which explicitly use
		// localhost.
		ext.RunOrFail(dep.IP6TABLES, "-t", NAT, "-A", ISTIOOUTPUT, "-d", "::1/128", "-j", RETURN)
		// Apply outbound IPv6 exclusions. Must be applied before inclusions.
		for _, cidr := range ipv6RangesExclude.IPNets {
			ext.RunOrFail(dep.IP6TABLES, "-t", NAT, "-A", ISTIOOUTPUT, "-d", cidr.String(), "-j", RETURN)
		}
		// Apply outbound IPv6 inclusions.
		if ipv6RangesInclude.IsWildcard {
			// Wildcard specified. Redirect all remaining outbound traffic to Envoy.
			ext.RunOrFail(dep.IP6TABLES, "-t", NAT, "-A", ISTIOOUTPUT, "-j", ISTIOREDIRECT)
			for _, internalInterface := range split(config.KubevirtInterfaces) {
				ext.RunOrFail(dep.IP6TABLES, "-t", NAT, "-I", PREROUTING, "1", "-i", internalInterface, "-j", RETURN)
			}
		} else if len(ipv6RangesInclude.IPNets) > 0 {
			// User has specified a non-empty list of cidrs to be redirected to Envoy.
			for _, cidr := range ipv6RangesInclude.IPNets {
				for _, internalInterface := range split(config.KubevirtInterfaces) {
					ext.RunOrFail(dep.IP6TABLES, "-t", NAT, "-I", PREROUTING, "1", "-i", internalInterface, "-d", cidr.String(), "-j", ISTIOREDIRECT)
				}
				ext.RunOrFail(dep.IP6TABLES, "-t", NAT, "-A", ISTIOOUTPUT, "-d", cidr.String(), "-j", ISTIOREDIRECT)
			}
			// All other traffic is not redirected.
			ext.RunOrFail(dep.IP6TABLES, "-t", NAT, "-A", ISTIOOUTPUT, "-j", RETURN)
		}
	} else {
		// Drop all inbound traffic except established connections.
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-F", INPUT)
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-A", INPUT, "-m", "state", "--state", "ESTABLISHED", "-j", ACCEPT)
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-A", INPUT, "-i", "lo", "-d", "::1", "-j", ACCEPT)
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-A", INPUT, "-j", REJECT)
	}
}

func handleInboundIpv4Rules(ext dep.Dependencies, config *Config, ipv4RangesInclude NetworkRange) {
	// Apply outbound IP inclusions.
	if ipv4RangesInclude.IsWildcard {
		// Wildcard specified. Redirect all remaining outbound traffic to Envoy.
		ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-A", ISTIOOUTPUT, "-j", ISTIOREDIRECT)
		for _, internalInterface := range split(config.KubevirtInterfaces) {
			ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-I", PREROUTING, "1", "-i", internalInterface, "-j", ISTIOREDIRECT)
		}
	} else if len(ipv4RangesInclude.IPNets) > 0 {
		// User has specified a non-empty list of cidrs to be redirected to Envoy.
		for _, cidr := range ipv4RangesInclude.IPNets {
			for _, internalInterface := range split(config.KubevirtInterfaces) {
				ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-I", PREROUTING, "1", "-i", internalInterface, "-d", cidr.String(), "-j", ISTIOREDIRECT)
			}
			ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-A", ISTIOOUTPUT, "-d", cidr.String(), "-j", ISTIOREDIRECT)
		}
		// All other traffic is not redirected.
		ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-A", ISTIOOUTPUT, "-j", RETURN)
	}
}

func run(args []string, flagSet *flag.FlagSet) {
	config := constructConfigFromFlags(args, flagSet)
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

	removeOldChains(ext)
	if len(flagSet.Args()) > 0 && flagSet.Arg(0) == "clean" {
		fmt.Println("Only cleaning, no new rules added")
		return
	}

	dumpEnvVariables(config)

	if config.EnableInboundIPv6s != nil {
		ext.RunOrFail(dep.IP, "-6", "addr", "add", "::6/128", "dev", "lo")
	}

	// Create a new chain for redirecting outbound traffic to the common Envoy port.
	// In both chains, '-j RETURN' bypasses Envoy and '-j ISTIOREDIRECT'
	// redirects to Envoy.
	ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-N", ISTIOREDIRECT)
	ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-A", ISTIOREDIRECT, "-p", TCP, "-j", REDIRECT, "--to-port", config.ProxyPort)
	// Use this chain also for redirecting inbound traffic to the common Envoy port
	// when not using TPROXY.
	ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-N", ISTIOINREDIRECT)

	// PROXY_INBOUND_CAPTURE_PORT should be used only user explicitly set INBOUND_PORTS_INCLUDE to capture all
	if config.InboundPortsInclude == "*" {
		ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-A", ISTIOINREDIRECT, "-p", TCP, "-j", REDIRECT, "--to-port", config.InboundCapturePort)
	} else {
		ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-A", ISTIOINREDIRECT, "-p", TCP, "-j", REDIRECT, "--to-port", config.ProxyPort)
	}

	handleInboundPortsInclude(ext, config)

	// TODO: change the default behavior to not intercept any output - user may use http_proxy or another
	// iptablesOrFail wrapper (like ufw). Current default is similar with 0.1
	// Create a new chain for selectively redirecting outbound packets to Envoy.
	ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-N", ISTIOOUTPUT)
	// Jump to the ISTIOOUTPUT chain from OUTPUT chain for all tcp traffic.
	ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-A", OUTPUT, "-p", TCP, "-j", ISTIOOUTPUT)

	// Apply port based exclusions. Must be applied before connections back to self are redirected.
	if config.OutboundPortsExclude != "" {
		for _, port := range split(config.OutboundPortsExclude) {
			ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-A", ISTIOOUTPUT, "-p", TCP, "--dport", port, "-j", RETURN)
		}
	}

	// 127.0.0.6 is bind connect from inbound passthrough cluster
	ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-A", ISTIOOUTPUT, "-o", "lo", "-s", "127.0.0.6/32", "-j", RETURN)

	if env.RegisterStringVar("DISABLE_REDIRECTION_ON_LOCAL_LOOPBACK", "", "").Get() == "" {
		// Redirect app calls back to itself via Envoy when using the service VIP or endpoint
		// address, e.g. appN => Envoy (client) => Envoy (server) => appN.
		ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-A", ISTIOOUTPUT, "-o", "lo", "!", "-d", "127.0.0.1/32", "-j", ISTIOINREDIRECT)
	}

	for _, uid := range split(config.ProxyUID) {
		// Avoid infinite loops. Don't redirect Envoy traffic directly back to
		// Envoy for non-loopback traffic.
		ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-A", ISTIOOUTPUT, "-m", "owner", "--uid-owner", uid, "-j", RETURN)
	}

	for _, gid := range split(config.ProxyGID) {
		// Avoid infinite loops. Don't redirect Envoy traffic directly back to
		// Envoy for non-loopback traffic.
		ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-A", ISTIOOUTPUT, "-m", "owner", "--gid-owner", gid, "-j", RETURN)
	}
	// Skip redirection for Envoy-aware applications and
	// container-to-container traffic both of which explicitly use
	// localhost.
	ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-A", ISTIOOUTPUT, "-d", "127.0.0.1/32", "-j", RETURN)
	// Apply outbound IPv4 exclusions. Must be applied before inclusions.
	for _, cidr := range ipv4RangesExclude.IPNets {
		ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-A", ISTIOOUTPUT, "-d", cidr.String(), "-j", RETURN)
	}

	for _, internalInterface := range split(config.KubevirtInterfaces) {
		ext.RunOrFail(dep.IPTABLES, "-t", NAT, "-I", PREROUTING, "1", "-i", internalInterface, "-j", RETURN)
	}

	handleInboundIpv4Rules(ext, config, ipv4RangesInclude)
	handleInboundIpv6Rules(ext, config, ipv6RangesExclude, ipv6RangesInclude)
}

func main() {
	run(os.Args[1:], flag.CommandLine)
}
