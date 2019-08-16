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

func run(args []string, flagSet *flag.FlagSet) {

	proxyUID := ""
	proxyGID := ""
	inboundInterceptionMode := env.RegisterStringVar("ISTIO_INBOUND_INTERCEPTION_MODE", "", "").Get()
	inboundTProxyMark := env.RegisterStringVar("ISTIO_INBOUND_TPROXY_MARK", "1337", "").Get()
	inboundTProxyRouteTable := env.RegisterStringVar("ISTIO_INBOUND_TPROXY_ROUTE_TABLE", "133", "").Get()
	inboundPortsInclude := env.RegisterStringVar("ISTIO_INBOUND_PORTS", "", "").Get()
	inboundPortsExclude := env.RegisterStringVar("ISTIO_LOCAL_EXCLUDE_PORTS", "", "").Get()
	outboundIPRangesInclude := env.RegisterStringVar("ISTIO_SERVICE_CIDR", "", "").Get()
	outboundIPRangesExclude := env.RegisterStringVar("ISTIO_SERVICE_EXCLUDE_CIDR", "", "").Get()
	outboundPortsExclude := env.RegisterStringVar("ISTIO_LOCAL_OUTBOUND_PORTS_EXCLUDE", "", "").Get()
	kubevirtInterfaces := ""
	var enableInboundIPv6s net.IP

	proxyPort := env.RegisterStringVar("ENVOY_PORT", "15001", "").Get()
	inboundCapturePort := env.RegisterStringVar("INBOUND_CAPTURE_PORT", "15006", "").Get()
	flagSet.StringVar(&proxyPort, "p", proxyPort,
		"Specify the envoy port to which redirect all TCP traffic (default $ENVOY_PORT = 15001)")
	flagSet.StringVar(&inboundCapturePort, "z", inboundCapturePort,
		"Port to which all inbound TCP traffic to the pod/VM should be redirected to (default $INBOUND_CAPTURE_PORT = 15006)")
	flagSet.StringVar(&proxyUID, "u", proxyUID,
		"Specify the UID of the user for which the redirection is not applied. Typically, this is the UID of the proxy container")
	flagSet.StringVar(&proxyGID, "g", proxyGID,
		"Specify the GID of the user for which the redirection is not applied. (same default value as -u param)")
	flagSet.StringVar(&inboundInterceptionMode, "m", inboundInterceptionMode,
		"The mode used to redirect inbound connections to Envoy, either \"REDIRECT\" or \"TPROXY\"")
	flagSet.StringVar(&inboundPortsInclude, "b", inboundPortsInclude,
		"Comma separated list of inbound ports for which traffic is to be redirected to Envoy (optional). "+
			"The wildcard character \"*\" can be used to configure redirection for all ports. An empty list will disable")
	flagSet.StringVar(&inboundPortsExclude, "d", inboundPortsExclude,
		"Comma separated list of inbound ports to be excluded from redirection to Envoy (optional). "+
			"Only applies  when all inbound traffic (i.e. \"*\") is being redirected (default to $ISTIO_LOCAL_EXCLUDE_PORTS)")
	flagSet.StringVar(&outboundIPRangesInclude, "i", outboundIPRangesInclude,
		"Comma separated list of IP ranges in CIDR form to redirect to envoy (optional). "+
			"The wildcard character \"*\" can be used to redirect all outbound traffic. An empty list will disable all outbound")
	flagSet.StringVar(&outboundIPRangesExclude, "x", outboundIPRangesExclude,
		"Comma separated list of IP ranges in CIDR form to be excluded from redirection. "+
			"Only applies when all  outbound traffic (i.e. \"*\") is being redirected (default to $ISTIO_SERVICE_EXCLUDE_CIDR)")
	flagSet.StringVar(&outboundPortsExclude, "o", outboundPortsExclude,
		"Comma separated list of outbound ports to be excluded from redirection to Envoy (optional")
	flagSet.StringVar(&kubevirtInterfaces, "k", kubevirtInterfaces,
		"Comma separated list of virtual interfaces whose inbound traffic (from VM) will be treated as outbound (optional)")

	var dryRun bool
	flagSet.BoolVar(&dryRun, "dryRun", false, "Do not call any external dependencies like iptables")
	err := flagSet.Parse(args)
	if err != nil {
		return
	}

	var ext dep.Dependencies
	if dryRun {
		ext = &dep.StdoutStubDependencies{}
	} else {
		ext = &dep.RealDependencies{}
	}

	defer func() {
		ext.RunOrFail(dep.IPTABLESSAVE)
		ext.RunOrFail(dep.IP6TABLESSAVE)
	}()

	// TODO: more flexibility - maybe a whitelist of users to be captured for output instead of a blacklist.
	if proxyUID == "" {
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
		proxyUID = userID + ",0"
	}

	// for TPROXY as its uid and gid are same
	if proxyGID == "" {
		proxyGID = proxyUID
	}

	podIP, err := ext.GetLocalIP()
	if err != nil {
		panic(err)
	}
	// Check if pod's ip is ipv4 or ipv6, in case of ipv6 set variable
	// to program ip6tablesOrFail
	if podIP.To4() == nil {
		enableInboundIPv6s = podIP
	}
	//
	// Since OUTBOUND_IP_RANGES_EXCLUDE could carry ipv4 and ipv6 ranges
	// need to split them in different arrays one for ipv4 and one for ipv6
	// in order to not to fail
	ipv4RangesExclude, ipv6RangesExclude, err := separateV4V6(outboundIPRangesExclude)
	if err != nil {
		panic(err)
	}
	if ipv4RangesExclude.IsWildcard {
		panic("Invalid value for OUTBOUND_IP_RANGES_EXCLUDE")
	}

	ipv4RangesInclude, ipv6RangesInclude, err := separateV4V6(outboundIPRangesInclude)
	if err != nil {
		panic(err)
	}

	// Remove the old chains, to generate new configs.
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "nat", "-D", "PREROUTING", "-p", "tcp", "-j", "ISTIO_INBOUND")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "mangle", "-D", "PREROUTING", "-p", "tcp", "-j", "ISTIO_INBOUND")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "nat", "-D", "OUTPUT", "-p", "tcp", "-j", "ISTIO_OUTPUT")
	// Flush and delete the istio chains.
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "nat", "-F", "ISTIO_OUTPUT")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "nat", "-X", "ISTIO_OUTPUT")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "nat", "-F", "ISTIO_INBOUND")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "nat", "-X", "ISTIO_INBOUND")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "mangle", "-F", "ISTIO_INBOUND")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "mangle", "-X", "ISTIO_INBOUND")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "mangle", "-F", "ISTIO_DIVERT")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "mangle", "-X", "ISTIO_DIVERT")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "mangle", "-F", "ISTIO_TPROXY")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "mangle", "-X", "ISTIO_TPROXY")
	// Must be last, the others refer to it
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "nat", "-F", "ISTIO_REDIRECT")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "nat", "-X", "ISTIO_REDIRECT")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "nat", "-F", "ISTIO_IN_REDIRECT")
	ext.RunQuietlyAndIgnore(dep.IPTABLES, "-t", "nat", "-X", "ISTIO_IN_REDIRECT")

	if len(flagSet.Args()) > 0 && flagSet.Arg(0) == "clean" {
		fmt.Println("Only cleaning, no new rules added")
		return
	}
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
	fmt.Printf("PROXY_PORT=%s\n", proxyPort)
	fmt.Printf("PROXY_INBOUND_CAPTURE_PORT=%s\n", inboundCapturePort)
	fmt.Printf("PROXY_UID=%s\n", proxyUID)
	fmt.Printf("INBOUND_INTERCEPTION_MODE=%s\n", inboundInterceptionMode)
	fmt.Printf("INBOUND_TPROXY_MARK=%s\n", inboundTProxyMark)
	fmt.Printf("INBOUND_TPROXY_ROUTE_TABLE=%s\n", inboundTProxyRouteTable)
	fmt.Printf("INBOUND_PORTS_INCLUDE=%s\n", inboundPortsInclude)
	fmt.Printf("INBOUND_PORTS_EXCLUDE=%s\n", inboundPortsExclude)
	fmt.Printf("OUTBOUND_IP_RANGES_INCLUDE=%s\n", outboundIPRangesInclude)
	fmt.Printf("OUTBOUND_IP_RANGES_EXCLUDE=%s\n", outboundIPRangesExclude)
	fmt.Printf("OUTBOUND_PORTS_EXCLUDE=%s\n", outboundPortsExclude)
	fmt.Printf("KUBEVIRT_INTERFACES=%s\n", kubevirtInterfaces)
	// Print "" instead of <nil> to produce same output as script and satisfy golden tests
	if enableInboundIPv6s == nil {
		fmt.Printf("ENABLE_INBOUND_IPV6=%s\n", "")
	} else {
		fmt.Printf("ENABLE_INBOUND_IPV6=%s\n", enableInboundIPv6s)
	}
	fmt.Println("")

	if enableInboundIPv6s != nil {
		ext.RunOrFail(dep.IP, "-6", "addr", "add", "::6/128", "dev", "lo")
	}

	// Create a new chain for redirecting outbound traffic to the common Envoy port.
	// In both chains, '-j RETURN' bypasses Envoy and '-j ISTIO_REDIRECT'
	// redirects to Envoy.
	ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-N", "ISTIO_REDIRECT")
	ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-A", "ISTIO_REDIRECT", "-p", "tcp", "-j", "REDIRECT", "--to-port", proxyPort)
	// Use this chain also for redirecting inbound traffic to the common Envoy port
	// when not using TPROXY.
	ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-N", "ISTIO_IN_REDIRECT")

	// PROXY_INBOUND_CAPTURE_PORT should be used only user explicitly set INBOUND_PORTS_INCLUDE to capture all
	if inboundPortsInclude == "*" {
		ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-A", "ISTIO_IN_REDIRECT", "-p", "tcp", "-j", "REDIRECT", "--to-port", inboundCapturePort)
	} else {
		ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-A", "ISTIO_IN_REDIRECT", "-p", "tcp", "-j", "REDIRECT", "--to-port", proxyPort)
	}

	var table string
	// Handling of inbound ports. Traffic will be redirected to Envoy, which will process and forward
	// to the local service. If not set, no inbound port will be intercepted by istio iptablesOrFail.
	if inboundPortsInclude != "" {
		if inboundInterceptionMode == "TPROXY" {
			// When using TPROXY, create a new chain for routing all inbound traffic to
			// Envoy. Any packet entering this chain gets marked with the ${INBOUND_TPROXY_MARK} mark,
			// so that they get routed to the loopback interface in order to get redirected to Envoy.
			// In the ISTIO_INBOUND chain, '-j ISTIO_DIVERT' reroutes to the loopback
			// interface.
			// Mark all inbound packets.
			ext.RunOrFail(dep.IPTABLES, "-t", "mangle", "-N", "ISTIO_DIVERT")
			ext.RunOrFail(dep.IPTABLES, "-t", "mangle", "-A", "ISTIO_DIVERT", "-j", "MARK", "--set-mark", inboundTProxyMark)
			ext.RunOrFail(dep.IPTABLES, "-t", "mangle", "-A", "ISTIO_DIVERT", "-j", "ACCEPT")
			// Route all packets marked in chain ISTIO_DIVERT using routing table ${INBOUND_TPROXY_ROUTE_TABLE}.
			ext.RunOrFail(dep.IP, "-f", "inet", "rule", "add", "fwmark", inboundTProxyMark, "lookup", inboundTProxyRouteTable)
			// In routing table ${INBOUND_TPROXY_ROUTE_TABLE}, create a single default rule to route all traffic to
			// the loopback interface.
			err = ext.Run(dep.IP, "-f", "inet", "route", "add", "local", "default", "dev", "lo", "table", inboundTProxyRouteTable)
			if err != nil {
				ext.RunOrFail(dep.IP, "route", "show", "table", "all")
			}
			// Create a new chain for redirecting inbound traffic to the common Envoy
			// port.
			// In the ISTIO_INBOUND chain, '-j RETURN' bypasses Envoy and
			// '-j ISTIO_TPROXY' redirects to Envoy.
			ext.RunOrFail(dep.IPTABLES, "-t", "mangle", "-N", "ISTIO_TPROXY")
			ext.RunOrFail(dep.IPTABLES, "-t", "mangle", "-A", "ISTIO_TPROXY", "!", "-d", "127.0.0.1/32", "-p", "tcp", "-j", "TPROXY",
				"--tproxy-mark", inboundTProxyMark+"/0xffffffff", "--on-port", proxyPort)

			table = "mangle"
		} else {
			table = "nat"
		}
		ext.RunOrFail(dep.IPTABLES, "-t", table, "-N", "ISTIO_INBOUND")
		ext.RunOrFail(dep.IPTABLES, "-t", table, "-A", "PREROUTING", "-p", "tcp", "-j", "ISTIO_INBOUND")

		if inboundPortsInclude == "*" {
			// Makes sure SSH is not redirected
			ext.RunOrFail(dep.IPTABLES, "-t", table, "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", "22", "-j", "RETURN")
			// Apply any user-specified port exclusions.
			if inboundPortsExclude != "" {
				for _, port := range split(inboundPortsExclude) {
					ext.RunOrFail(dep.IPTABLES, "-t", table, "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", port, "-j", "RETURN")
				}
			}
			// Redirect remaining inbound traffic to Envoy.
			if inboundInterceptionMode == "TPROXY" {
				// If an inbound packet belongs to an established socket, route it to the
				// loopback interface.
				err := ext.Run(dep.IPTABLES, "-t", "mangle", "-A", "ISTIO_INBOUND", "-p", "tcp", "-m", "socket", "-j", "ISTIO_DIVERT")
				if err != nil {
					fmt.Println("No socket match support")
				}
				// Otherwise, it's a new connection. Redirect it using TPROXY.
				ext.RunOrFail(dep.IPTABLES, "-t", "mangle", "-A", "ISTIO_INBOUND", "-p", "tcp", "-j", "ISTIO_TPROXY")
			} else {
				ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-A", "ISTIO_INBOUND", "-p", "tcp", "-j", "ISTIO_IN_REDIRECT")
			}
		} else {
			// User has specified a non-empty list of ports to be redirected to Envoy.
			for _, port := range split(inboundPortsInclude) {
				if inboundInterceptionMode == "TPROXY" {
					err := ext.Run(dep.IPTABLES, "-t", "mangle", "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", port, "-m", "socket", "-j", "ISTIO_DIVERT")
					if err != nil {
						fmt.Println("No socket match support")
					}
					err = ext.Run(dep.IPTABLES, "-t", "mangle", "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", port, "-m", "socket", "-j", "ISTIO_DIVERT")
					if err != nil {
						fmt.Println("No socket match support")
					}
					ext.RunOrFail(dep.IPTABLES, "-t", "mangle", "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", port, "-j", "ISTIO_TPROXY")
				} else {
					ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", port, "-j", "ISTIO_IN_REDIRECT")
				}
			}
		}
	}
	// TODO: change the default behavior to not intercept any output - user may use http_proxy or another
	// iptablesOrFail wrapper (like ufw). Current default is similar with 0.1
	// Create a new chain for selectively redirecting outbound packets to Envoy.
	ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-N", "ISTIO_OUTPUT")
	// Jump to the ISTIO_OUTPUT chain from OUTPUT chain for all tcp traffic.
	ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-A", "OUTPUT", "-p", "tcp", "-j", "ISTIO_OUTPUT")

	// Apply port based exclusions. Must be applied before connections back to self are redirected.
	if outboundPortsExclude != "" {
		for _, port := range split(outboundPortsExclude) {
			ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-A", "ISTIO_OUTPUT", "-p", "tcp", "--dport", port, "-j", "RETURN")
		}
	}

	// 127.0.0.6 is bind connect from inbound passthrough cluster
	ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-A", "ISTIO_OUTPUT", "-o", "lo", "-s", "127.0.0.6/32", "-j", "RETURN")

	if env.RegisterStringVar("DISABLE_REDIRECTION_ON_LOCAL_LOOPBACK", "", "").Get() == "" {
		// Redirect app calls back to itself via Envoy when using the service VIP or endpoint
		// address, e.g. appN => Envoy (client) => Envoy (server) => appN.
		ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-A", "ISTIO_OUTPUT", "-o", "lo", "!", "-d", "127.0.0.1/32", "-j", "ISTIO_IN_REDIRECT")
	}

	for _, uid := range split(proxyUID) {
		// Avoid infinite loops. Don't redirect Envoy traffic directly back to
		// Envoy for non-loopback traffic.
		ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-A", "ISTIO_OUTPUT", "-m", "owner", "--uid-owner", uid, "-j", "RETURN")
	}

	for _, gid := range split(proxyGID) {
		// Avoid infinite loops. Don't redirect Envoy traffic directly back to
		// Envoy for non-loopback traffic.
		ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-A", "ISTIO_OUTPUT", "-m", "owner", "--gid-owner", gid, "-j", "RETURN")
	}
	// Skip redirection for Envoy-aware applications and
	// container-to-container traffic both of which explicitly use
	// localhost.
	ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-A", "ISTIO_OUTPUT", "-d", "127.0.0.1/32", "-j", "RETURN")
	// Apply outbound IPv4 exclusions. Must be applied before inclusions.
	for _, cidr := range ipv4RangesExclude.IPNets {
		ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-A", "ISTIO_OUTPUT", "-d", cidr.String(), "-j", "RETURN")
	}

	for _, internalInterface := range split(kubevirtInterfaces) {
		ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-I", "PREROUTING", "1", "-i", internalInterface, "-j", "RETURN")
	}
	// Apply outbound IP inclusions.
	if ipv4RangesInclude.IsWildcard {
		// Wildcard specified. Redirect all remaining outbound traffic to Envoy.
		ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-A", "ISTIO_OUTPUT", "-j", "ISTIO_REDIRECT")
		for _, internalInterface := range split(kubevirtInterfaces) {
			ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-I", "PREROUTING", "1", "-i", internalInterface, "-j", "ISTIO_REDIRECT")
		}
	} else if len(ipv4RangesInclude.IPNets) > 0 {
		// User has specified a non-empty list of cidrs to be redirected to Envoy.
		for _, cidr := range ipv4RangesInclude.IPNets {
			for _, internalInterface := range split(kubevirtInterfaces) {
				ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-I", "PREROUTING", "1", "-i", internalInterface, "-d", cidr.String(), "-j", "ISTIO_REDIRECT")
			}
			ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-A", "ISTIO_OUTPUT", "-d", cidr.String(), "-j", "ISTIO_REDIRECT")
		}
		// All other traffic is not redirected.
		ext.RunOrFail(dep.IPTABLES, "-t", "nat", "-A", "ISTIO_OUTPUT", "-j", "RETURN")
	}
	// If ENABLE_INBOUND_IPV6 is unset (default unset), restrict IPv6 traffic.
	if enableInboundIPv6s != nil {
		// Remove the old chains, to generate new configs.
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", "nat", "-D", "PREROUTING", "-p", "tcp", "-j", "ISTIO_INBOUND")
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", "mangle", "-D", "PREROUTING", "-p", "tcp", "-j", "ISTIO_INBOUND")
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", "nat", "-D", "OUTPUT", "-p", "tcp", "-j", "ISTIO_OUTPUT")
		// Flush and delete the istio chains.
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", "nat", "-F", "ISTIO_OUTPUT")
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", "nat", "-X", "ISTIO_OUTPUT")
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", "nat", "-F", "ISTIO_INBOUND")
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", "nat", "-X", "ISTIO_INBOUND")
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", "mangle", "-F", "ISTIO_INBOUND")
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", "mangle", "-X", "ISTIO_INBOUND")
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", "mangle", "-F", "ISTIO_DIVERT")
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", "mangle", "-X", "ISTIO_DIVERT")
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", "mangle", "-F", "ISTIO_TPROXY")
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", "mangle", "-X", "ISTIO_TPROXY")

		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", "nat", "-F", "ISTIO_REDIRECT")
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", "nat", "-X", "ISTIO_REDIRECT")
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", "nat", "-F", "ISTIO_IN_REDIRECT")
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-t", "nat", "-X", "ISTIO_IN_REDIRECT")
		// Create a new chain for redirecting outbound traffic to the common Envoy port.
		// In both chains, '-j RETURN' bypasses Envoy and '-j ISTIO_REDIRECT'
		// redirects to Envoy.
		ext.RunOrFail(dep.IP6TABLES, "-t", "nat", "-N", "ISTIO_REDIRECT")
		ext.RunOrFail(dep.IP6TABLES, "-t", "nat", "-A", "ISTIO_REDIRECT", "-p", "tcp", "-j", "REDIRECT", "--to-port", proxyPort)
		// Use this chain also for redirecting inbound traffic to the common Envoy port
		// when not using TPROXY.
		ext.RunOrFail(dep.IP6TABLES, "-t", "nat", "-N", "ISTIO_IN_REDIRECT")
		if inboundPortsInclude == "*" {
			ext.RunOrFail(dep.IP6TABLES, "-t", "nat", "-A", "ISTIO_IN_REDIRECT", "-p", "tcp", "-j", "REDIRECT", "--to-port", inboundCapturePort)
		} else {
			ext.RunOrFail(dep.IP6TABLES, "-t", "nat", "-A", "ISTIO_IN_REDIRECT", "-p", "tcp", "-j", "REDIRECT", "--to-port", proxyPort)
		}
		// Handling of inbound ports. Traffic will be redirected to Envoy, which will process and forward
		// to the local service. If not set, no inbound port will be intercepted by istio iptablesOrFail.
		if inboundPortsInclude != "" {
			table = "nat"
			ext.RunOrFail(dep.IP6TABLES, "-t", table, "-N", "ISTIO_INBOUND")
			ext.RunOrFail(dep.IP6TABLES, "-t", table, "-A", "PREROUTING", "-p", "tcp", "-j", "ISTIO_INBOUND")

			if inboundPortsInclude == "*" {
				// Makes sure SSH is not redirected
				ext.RunOrFail(dep.IP6TABLES, "-t", table, "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", "22", "-j", "RETURN")
				// Apply any user-specified port exclusions.
				if inboundPortsExclude != "" {
					for _, port := range split(inboundPortsExclude) {
						ext.RunOrFail(dep.IP6TABLES, "-t", table, "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", port, "-j", "RETURN")
					}
				}
			} else {
				// User has specified a non-empty list of ports to be redirected to Envoy.
				for _, port := range split(inboundPortsInclude) {
					ext.RunOrFail(dep.IP6TABLES, "-t", "nat", "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", port, "-j", "ISTIO_IN_REDIRECT")
				}
			}
		}
		// Create a new chain for selectively redirecting outbound packets to Envoy.
		ext.RunOrFail(dep.IP6TABLES, "-t", "nat", "-N", "ISTIO_OUTPUT")
		// Jump to the ISTIO_OUTPUT chain from OUTPUT chain for all tcp traffic.
		ext.RunOrFail(dep.IP6TABLES, "-t", "nat", "-A", "OUTPUT", "-p", "tcp", "-j", "ISTIO_OUTPUT")
		// Apply port based exclusions. Must be applied before connections back to self are redirected.
		if outboundPortsExclude != "" {
			for _, port := range split(outboundPortsExclude) {
				ext.RunOrFail(dep.IP6TABLES, "-t", "nat", "-A", "ISTIO_OUTPUT", "-p", "tcp", "--dport", port, "-j", "RETURN")
			}
		}

		// ::6 is bind connect from inbound passthrough cluster
		ext.RunOrFail(dep.IP6TABLES, "-t", "nat", "-A", "ISTIO_OUTPUT", "-o", "lo", "-s", "::6/128", "-j", "RETURN")

		// Redirect app calls to back itself via Envoy when using the service VIP or endpoint
		// address, e.g. appN => Envoy (client) => Envoy (server) => appN.
		ext.RunOrFail(dep.IP6TABLES, "-t", "nat", "-A", "ISTIO_OUTPUT", "-o", "lo", "!", "-d", "::1/128", "-j", "ISTIO_IN_REDIRECT")

		for _, uid := range split(proxyUID) {
			// Avoid infinite loops. Don't redirect Envoy traffic directly back to
			// Envoy for non-loopback traffic.
			ext.RunOrFail(dep.IP6TABLES, "-t", "nat", "-A", "ISTIO_OUTPUT", "-m", "owner", "--uid-owner", uid, "-j", "RETURN")
		}

		for _, gid := range split(proxyGID) {
			// Avoid infinite loops. Don't redirect Envoy traffic directly back to
			// Envoy for non-loopback traffic.
			ext.RunOrFail(dep.IP6TABLES, "-t", "nat", "-A", "ISTIO_OUTPUT", "-m", "owner", "--gid-owner", gid, "-j", "RETURN")
		}
		// Skip redirection for Envoy-aware applications and
		// container-to-container traffic both of which explicitly use
		// localhost.
		ext.RunOrFail(dep.IP6TABLES, "-t", "nat", "-A", "ISTIO_OUTPUT", "-d", "::1/128", "-j", "RETURN")
		// Apply outbound IPv6 exclusions. Must be applied before inclusions.
		for _, cidr := range ipv6RangesExclude.IPNets {
			ext.RunOrFail(dep.IP6TABLES, "-t", "nat", "-A", "ISTIO_OUTPUT", "-d", cidr.String(), "-j", "RETURN")
		}
		// Apply outbound IPv6 inclusions.
		if ipv6RangesInclude.IsWildcard {
			// Wildcard specified. Redirect all remaining outbound traffic to Envoy.
			ext.RunOrFail(dep.IP6TABLES, "-t", "nat", "-A", "ISTIO_OUTPUT", "-j", "ISTIO_REDIRECT")
			for _, internalInterface := range split(kubevirtInterfaces) {
				ext.RunOrFail(dep.IP6TABLES, "-t", "nat", "-I", "PREROUTING", "1", "-i", internalInterface, "-j", "RETURN")
			}
		} else if len(ipv6RangesInclude.IPNets) > 0 {
			// User has specified a non-empty list of cidrs to be redirected to Envoy.
			for _, cidr := range ipv6RangesInclude.IPNets {
				for _, internalInterface := range split(kubevirtInterfaces) {
					ext.RunOrFail(dep.IP6TABLES, "-t", "nat", "-I", "PREROUTING", "1", "-i", internalInterface, "-d", cidr.String(), "-j", "ISTIO_REDIRECT")
				}
				ext.RunOrFail(dep.IP6TABLES, "-t", "nat", "-A", "ISTIO_OUTPUT", "-d", cidr.String(), "-j", "ISTIO_REDIRECT")
			}
			// All other traffic is not redirected.
			ext.RunOrFail(dep.IP6TABLES, "-t", "nat", "-A", "ISTIO_OUTPUT", "-j", "RETURN")
		}
	} else {
		// Drop all inbound traffic except established connections.
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-F", "INPUT")
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-A", "INPUT", "-m", "state", "--state", "ESTABLISHED", "-j", "ACCEPT")
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-A", "INPUT", "-i", "lo", "-d", "::1", "-j", "ACCEPT")
		ext.RunQuietlyAndIgnore(dep.IP6TABLES, "-A", "INPUT", "-j", "REJECT")
	}
}

func main() {
	run(os.Args[1:], flag.CommandLine)
}
