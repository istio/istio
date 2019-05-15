package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"strings"

	"github.com/joho/godotenv"
)

func getEnvWithDefault(key string, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func dotEnvLoad(key string, defaultPath string) {
	path := getEnvWithDefault(key, defaultPath)

	if _, err := os.Stat(path); err == nil {
		err = godotenv.Load(path)
		if err != nil {
			panic(err)
		}
	}

}

func getLocalIP() (net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}

	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			return ipnet.IP, nil
		}
	}
	return nil, fmt.Errorf("no valid local IP address found")
}

type NetworkRange struct {
	IsWildcard bool
	IPNets     []*net.IPNet
}

type Command struct {
	Command          string
	Args             []string
	RedirectToStdErr bool
}

func defaultCommandRunner(command Command) error {
	fmt.Printf("%s %s\n", command.Command, strings.Join(command.Args, " "))
	cmd := exec.Command(command.Command, command.Args...)
	cmd.Stdout = os.Stdout
	if !command.RedirectToStdErr {
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

var commandRunner = defaultCommandRunner

func (i Command) Run() error {
	return commandRunner(i)
}

func (i Command) RunOrFail() {
	err := i.Run()
	if err != nil {
		panic(err)
	}
}

func (i Command) RunOrIgnore(message string) {
	err := i.Run()
	if err != nil {
		fmt.Println(message)
	}
}

func (i Command) RunQuietlyAndIgnore() {
	i.RedirectToStdErr = true
	_ = i.Run()
}

func iptables(args ...string) Command {
	return Command{Command: "iptables", Args: args, RedirectToStdErr: false}
}

func ip6tables(args ...string) Command {
	return Command{Command: "ip6tables", Args: args, RedirectToStdErr: false}
}

func ip(args ...string) Command {
	return Command{Command: "ip", Args: args, RedirectToStdErr: false}
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

func dump() {
	Command{Command: "iptables-save"}.RunOrFail()
	Command{Command: "ip6tables-save"}.RunOrFail()
}

func run(args []string, flagSet *flag.FlagSet, getLocalIP func() (net.IP, error)) {

	// The cluster env can be used for common cluster settings, pushed to all VMs in the cluster.
	// This allows separating per-machine settings (the list of inbound ports, local path overrides) from cluster wide
	// settings (CIDR range)
	dotEnvLoad("ISTIO_CLUSTER_CONFIG", "/var/lib/istio/envoy/cluster.env")
	dotEnvLoad("ISTIO_SIDECAR_CONFIG", "/var/lib/istio/envoy/sidecar.env")

	proxyPort := getEnvWithDefault("ENVOY_PORT", "15001")
	proxyUID := ""
	proxyGID := ""
	inboundInterceptionMode := os.Getenv("ISTIO_INBOUND_INTERCEPTION_MODE")
	inboundTProxyMark := getEnvWithDefault("ISTIO_INBOUND_TPROXY_MARK", "1337")
	inboundTProxyRouteTable := getEnvWithDefault("ISTIO_INBOUND_TPROXY_ROUTE_TABLE", "133")
	inboundPortsInclude := os.Getenv("ISTIO_INBOUND_PORTS")
	inboundPortsExclude := os.Getenv("ISTIO_LOCAL_EXCLUDE_PORTS")
	outboundIPRangesInclude := os.Getenv("ISTIO_SERVICE_CIDR")
	outboundIPRangesExclude := os.Getenv("ISTIO_SERVICE_EXCLUDE_CIDR")
	kubevirtInterfaces := ""
	var enableInboundIPv6s net.IP

	flagSet.StringVar(&proxyPort, "p", proxyPort,
		"Specify the envoy port to which redirect all TCP traffic (default $ENVOY_PORT = 15001)")
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
	flagSet.StringVar(&kubevirtInterfaces, "k", kubevirtInterfaces,
		"Comma separated list of virtual interfaces whose inbound traffic (from VM) will be treated as outbound (optional)")

	err := flagSet.Parse(args)
	if err != nil {
		return
	}

	defer dump()

	// TODO: more flexibility - maybe a whitelist of users to be captured for output instead of a blacklist.
	if proxyUID == "" {
		usr, err := user.Lookup(getEnvWithDefault("ENVOY_USER", "istio-proxy"))
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

	podIP, err := getLocalIP()
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
	iptables("-t", "nat", "-D", "PREROUTING", "-p", "tcp", "-j", "ISTIO_INBOUND").RunQuietlyAndIgnore()
	iptables("-t", "mangle", "-D", "PREROUTING", "-p", "tcp", "-j", "ISTIO_INBOUND").RunQuietlyAndIgnore()
	iptables("-t", "nat", "-D", "OUTPUT", "-p", "tcp", "-j", "ISTIO_OUTPUT").RunQuietlyAndIgnore()
	// Flush and delete the istio chains.
	iptables("-t", "nat", "-F", "ISTIO_OUTPUT").RunQuietlyAndIgnore()
	iptables("-t", "nat", "-X", "ISTIO_OUTPUT").RunQuietlyAndIgnore()
	iptables("-t", "nat", "-F", "ISTIO_INBOUND").RunQuietlyAndIgnore()
	iptables("-t", "nat", "-X", "ISTIO_INBOUND").RunQuietlyAndIgnore()
	iptables("-t", "mangle", "-F", "ISTIO_INBOUND").RunQuietlyAndIgnore()
	iptables("-t", "mangle", "-X", "ISTIO_INBOUND").RunQuietlyAndIgnore()
	iptables("-t", "mangle", "-F", "ISTIO_DIVERT").RunQuietlyAndIgnore()
	iptables("-t", "mangle", "-X", "ISTIO_DIVERT").RunQuietlyAndIgnore()
	iptables("-t", "mangle", "-F", "ISTIO_TPROXY").RunQuietlyAndIgnore()
	iptables("-t", "mangle", "-X", "ISTIO_TPROXY").RunQuietlyAndIgnore()
	// Must be last, the others refer to it
	iptables("-t", "nat", "-F", "ISTIO_REDIRECT").RunQuietlyAndIgnore()
	iptables("-t", "nat", "-X", "ISTIO_REDIRECT").RunQuietlyAndIgnore()
	iptables("-t", "nat", "-F", "ISTIO_IN_REDIRECT").RunQuietlyAndIgnore()
	iptables("-t", "nat", "-X", "ISTIO_IN_REDIRECT").RunQuietlyAndIgnore()

	if len(flagSet.Args()) > 0 && flagSet.Arg(0) == "clean" {
		fmt.Println("Only cleaning, no new rules added")
		return
	}
	// Dump out our environment for debugging purposes.
	fmt.Println("Environment:")
	fmt.Println("------------")
	fmt.Printf("ENVOY_PORT=%s\n", os.Getenv("ENVOY_PORT"))
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
	fmt.Printf("INBOUND_CAPTURE_PORT=%s\n", getEnvWithDefault("INBOUND_CAPTURE_PORT", proxyPort))
	fmt.Printf("PROXY_UID=%s\n", proxyUID)
	fmt.Printf("INBOUND_INTERCEPTION_MODE=%s\n", inboundInterceptionMode)
	fmt.Printf("INBOUND_TPROXY_MARK=%s\n", inboundTProxyMark)
	fmt.Printf("INBOUND_TPROXY_ROUTE_TABLE=%s\n", inboundTProxyRouteTable)
	fmt.Printf("INBOUND_PORTS_INCLUDE=%s\n", inboundPortsInclude)
	fmt.Printf("INBOUND_PORTS_EXCLUDE=%s\n", inboundPortsExclude)
	fmt.Printf("OUTBOUND_IP_RANGES_INCLUDE=%s\n", outboundIPRangesInclude)
	fmt.Printf("OUTBOUND_IP_RANGES_EXCLUDE=%s\n", outboundIPRangesExclude)
	fmt.Printf("KUBEVIRT_INTERFACES=%s\n", kubevirtInterfaces)
	fmt.Printf("ENABLE_INBOUND_IPV6=%s\n", enableInboundIPv6s)
	fmt.Println("")

	inboundCapturePort := getEnvWithDefault("INBOUND_CAPTURE_PORT", proxyPort)

	// Create a new chain for redirecting outbound traffic to the common Envoy port.
	// In both chains, '-j RETURN' bypasses Envoy and '-j ISTIO_REDIRECT'
	// redirects to Envoy.
	iptables("-t", "nat", "-N", "ISTIO_REDIRECT").RunOrFail()
	iptables("-t", "nat", "-A", "ISTIO_REDIRECT", "-p", "tcp", "-j", "REDIRECT", "--to-port", proxyPort).RunOrFail()
	// Use this chain also for redirecting inbound traffic to the common Envoy port
	// when not using TPROXY.
	iptables("-t", "nat", "-N", "ISTIO_IN_REDIRECT").RunOrFail()
	iptables("-t", "nat", "-A", "ISTIO_IN_REDIRECT", "-p", "tcp", "-j", "REDIRECT", "--to-port", inboundCapturePort).RunOrFail()

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
			iptables("-t", "mangle", "-N", "ISTIO_DIVERT").RunOrFail()
			iptables("-t", "mangle", "-A", "ISTIO_DIVERT", "-j", "MARK", "--set-mark", inboundTProxyMark).RunOrFail()
			iptables("-t", "mangle", "-A", "ISTIO_DIVERT", "-j", "ACCEPT").RunOrFail()
			// Route all packets marked in chain ISTIO_DIVERT using routing table ${INBOUND_TPROXY_ROUTE_TABLE}.
			ip("-f", "inet", "rule", "add", "fwmark", inboundTProxyMark, "lookup", inboundTProxyRouteTable).RunOrFail()
			// In routing table ${INBOUND_TPROXY_ROUTE_TABLE}, create a single default rule to route all traffic to
			// the loopback interface.
			err = ip("-f", "inet", "route", "add", "local", "default", "dev", "lo", "table", inboundTProxyRouteTable).Run()
			if err != nil {
				ip("route", "show", "table", "all").RunOrFail()
			}
			// Create a new chain for redirecting inbound traffic to the common Envoy
			// port.
			// In the ISTIO_INBOUND chain, '-j RETURN' bypasses Envoy and
			// '-j ISTIO_TPROXY' redirects to Envoy.
			iptables("-t", "mangle", "-N", "ISTIO_TPROXY").RunOrFail()
			iptables("-t", "mangle", "-A", "ISTIO_TPROXY", "!", "-d", "127.0.0.1/32", "-p", "tcp", "-j", "TPROXY",
				"--tproxy-mark", inboundTProxyMark+"/0xffffffff", "--on-port", proxyPort).RunOrFail()

			table = "mangle"
		} else {
			table = "nat"
		}
		iptables("-t", table, "-N", "ISTIO_INBOUND").RunOrFail()
		iptables("-t", table, "-A", "PREROUTING", "-p", "tcp", "-j", "ISTIO_INBOUND").RunOrFail()

		if inboundPortsInclude == "*" {
			// Makes sure SSH is not redirected
			iptables("-t", table, "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", "22", "-j", "RETURN").RunOrFail()
			// Apply any user-specified port exclusions.
			if inboundPortsExclude != "" {
				for _, port := range split(inboundPortsExclude) {
					iptables("-t", table, "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", port, "-j", "RETURN").RunOrFail()
				}
			}
			// Redirect remaining inbound traffic to Envoy.
			if inboundInterceptionMode == "TPROXY" {
				// If an inbound packet belongs to an established socket, route it to the
				// loopback interface.
				iptables("-t", "mangle", "-A", "ISTIO_INBOUND", "-p", "tcp", "-m", "socket", "-j", "ISTIO_DIVERT").RunOrIgnore("No socket match support")
				// Otherwise, it's a new connection. Redirect it using TPROXY.
				iptables("-t", "mangle", "-A", "ISTIO_INBOUND", "-p", "tcp", "-j", "ISTIO_TPROXY").RunOrFail()
			} else {
				iptables("-t", "nat", "-A", "ISTIO_INBOUND", "-p", "tcp", "-j", "ISTIO_IN_REDIRECT").RunOrFail()
			}
		} else {
			// User has specified a non-empty list of ports to be redirected to Envoy.
			for _, port := range split(inboundPortsInclude) {
				if inboundInterceptionMode == "TPROXY" {
					iptables("-t", "mangle", "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", port, "-m", "socket", "-j", "ISTIO_DIVERT").RunOrIgnore("No socket match support")
					iptables("-t", "mangle", "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", port, "-m", "socket", "-j", "ISTIO_DIVERT").RunOrIgnore("No socket match support")
					iptables("-t", "mangle", "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", port, "-j", "ISTIO_TPROXY").RunOrFail()
				} else {
					iptables("-t", "nat", "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", port, "-j", "ISTIO_IN_REDIRECT").RunOrFail()
				}
			}
		}
	}
	// TODO: change the default behavior to not intercept any output - user may use http_proxy or another
	// iptablesOrFail wrapper (like ufw). Current default is similar with 0.1
	// Create a new chain for selectively redirecting outbound packets to Envoy.
	iptables("-t", "nat", "-N", "ISTIO_OUTPUT").RunOrFail()
	// Jump to the ISTIO_OUTPUT chain from OUTPUT chain for all tcp traffic.
	iptables("-t", "nat", "-A", "OUTPUT", "-p", "tcp", "-j", "ISTIO_OUTPUT").RunOrFail()

	if os.Getenv("DISABLE_REDIRECTION_ON_LOCAL_LOOPBACK") == "" {
		// Redirect app calls to back itself via Envoy when using the service VIP or endpoint
		// address, e.g. appN => Envoy (client) => Envoy (server) => appN.
		iptables("-t", "nat", "-A", "ISTIO_OUTPUT", "-o", "lo", "!", "-d", "127.0.0.1/32", "-j", "ISTIO_REDIRECT").RunOrFail()
	}

	for _, uid := range split(proxyUID) {
		// Avoid infinite loops. Don't redirect Envoy traffic directly back to
		// Envoy for non-loopback traffic.
		iptables("-t", "nat", "-A", "ISTIO_OUTPUT", "-m", "owner", "--uid-owner", uid, "-j", "RETURN").RunOrFail()
	}

	for _, gid := range split(proxyGID) {
		// Avoid infinite loops. Don't redirect Envoy traffic directly back to
		// Envoy for non-loopback traffic.
		iptables("-t", "nat", "-A", "ISTIO_OUTPUT", "-m", "owner", "--gid-owner", gid, "-j", "RETURN").RunOrFail()
	}
	// Skip redirection for Envoy-aware applications and
	// container-to-container traffic both of which explicitly use
	// localhost.
	iptables("-t", "nat", "-A", "ISTIO_OUTPUT", "-d", "127.0.0.1/32", "-j", "RETURN").RunOrFail()
	// Apply outbound IPv4 exclusions. Must be applied before inclusions.
	for _, cidr := range ipv4RangesExclude.IPNets {
		iptables("-t", "nat", "-A", "ISTIO_OUTPUT", "-d", cidr.String(), "-j", "RETURN").RunOrFail()
	}

	for _, internalInterface := range split(kubevirtInterfaces) {
		iptables("-t", "nat", "-I", "PREROUTING", "1", "-i", internalInterface, "-j", "RETURN").RunOrFail()
	}
	// Apply outbound IP inclusions.
	if ipv4RangesInclude.IsWildcard {
		// Wildcard specified. Redirect all remaining outbound traffic to Envoy.
		iptables("-t", "nat", "-A", "ISTIO_OUTPUT", "-j", "ISTIO_REDIRECT").RunOrFail()
		for _, internalInterface := range split(kubevirtInterfaces) {
			iptables("-t", "nat", "-I", "PREROUTING", "1", "-i", internalInterface, "-j", "ISTIO_REDIRECT").RunOrFail()
		}
	} else if len(ipv4RangesInclude.IPNets) > 0 {
		// User has specified a non-empty list of cidrs to be redirected to Envoy.
		for _, cidr := range ipv4RangesInclude.IPNets {
			for _, internalInterface := range split(kubevirtInterfaces) {
				iptables("-t", "nat", "-I", "PREROUTING", "1", "-i", internalInterface, "-d", cidr.String(), "-j", "ISTIO_REDIRECT").RunOrFail()
			}
			iptables("-t", "nat", "-A", "ISTIO_OUTPUT", "-d", cidr.String(), "-j", "ISTIO_REDIRECT").RunOrFail()
		}
		// All other traffic is not redirected.
		iptables("-t", "nat", "-A", "ISTIO_OUTPUT", "-j", "RETURN").RunOrFail()
	}
	// If ENABLE_INBOUND_IPV6 is unset (default unset), restrict IPv6 traffic.
	if enableInboundIPv6s != nil {
		// Remove the old chains, to generate new configs.
		ip6tables("-t", "nat", "-D", "PREROUTING", "-p", "tcp", "-j", "ISTIO_INBOUND").RunQuietlyAndIgnore()
		ip6tables("-t", "mangle", "-D", "PREROUTING", "-p", "tcp", "-j", "ISTIO_INBOUND").RunQuietlyAndIgnore()
		ip6tables("-t", "nat", "-D", "OUTPUT", "-p", "tcp", "-j", "ISTIO_OUTPUT").RunQuietlyAndIgnore()
		// Flush and delete the istio chains.
		ip6tables("-t", "nat", "-F", "ISTIO_OUTPUT").RunQuietlyAndIgnore()
		ip6tables("-t", "nat", "-X", "ISTIO_OUTPUT").RunQuietlyAndIgnore()
		ip6tables("-t", "nat", "-F", "ISTIO_INBOUND").RunQuietlyAndIgnore()
		ip6tables("-t", "nat", "-X", "ISTIO_INBOUND").RunQuietlyAndIgnore()
		ip6tables("-t", "mangle", "-F", "ISTIO_INBOUND").RunQuietlyAndIgnore()
		ip6tables("-t", "mangle", "-X", "ISTIO_INBOUND").RunQuietlyAndIgnore()
		ip6tables("-t", "mangle", "-F", "ISTIO_DIVERT").RunQuietlyAndIgnore()
		ip6tables("-t", "mangle", "-X", "ISTIO_DIVERT").RunQuietlyAndIgnore()
		ip6tables("-t", "mangle", "-F", "ISTIO_TPROXY").RunQuietlyAndIgnore()
		ip6tables("-t", "mangle", "-X", "ISTIO_TPROXY").RunQuietlyAndIgnore()
		// Must be last, the others refer to it
		ip6tables("-t", "nat", "-F", "ISTIO_REDIRECT").RunQuietlyAndIgnore()
		ip6tables("-t", "nat", "-X", "ISTIO_REDIRECT").RunQuietlyAndIgnore()
		ip6tables("-t", "nat", "-F", "ISTIO_IN_REDIRECT").RunQuietlyAndIgnore()
		ip6tables("-t", "nat", "-X", "ISTIO_IN_REDIRECT").RunQuietlyAndIgnore()
		// Create a new chain for redirecting outbound traffic to the common Envoy port.
		// In both chains, '-j RETURN' bypasses Envoy and '-j ISTIO_REDIRECT'
		// redirects to Envoy.
		ip6tables("-t", "nat", "-N", "ISTIO_REDIRECT").RunOrFail()
		ip6tables("-t", "nat", "-A", "ISTIO_REDIRECT", "-p", "tcp", "-j", "REDIRECT", "--to-port", proxyPort).RunOrFail()
		// Use this chain also for redirecting inbound traffic to the common Envoy port
		// when not using TPROXY.
		ip6tables("-t", "nat", "-N", "ISTIO_IN_REDIRECT").RunOrFail()
		ip6tables("-t", "nat", "-A", "ISTIO_IN_REDIRECT", "-p", "tcp", "-j", "REDIRECT", "--to-port", inboundCapturePort).RunOrFail()
		// Handling of inbound ports. Traffic will be redirected to Envoy, which will process and forward
		// to the local service. If not set, no inbound port will be intercepted by istio iptablesOrFail.
		if inboundPortsInclude != "" {
			table = "nat"
			ip6tables("-t", table, "-N", "ISTIO_INBOUND").RunOrFail()
			ip6tables("-t", table, "-A", "PREROUTING", "-p", "tcp", "-j", "ISTIO_INBOUND").RunOrFail()

			if inboundPortsInclude == "*" {
				// Makes sure SSH is not redirected
				ip6tables("-t", table, "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", "22", "-j", "RETURN").RunOrFail()
				// Apply any user-specified port exclusions.
				if inboundPortsExclude != "" {
					for _, port := range split(inboundPortsExclude) {
						ip6tables("-t", table, "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", port, "-j", "RETURN").RunOrFail()
					}
				}
			} else {
				// User has specified a non-empty list of ports to be redirected to Envoy.
				for _, port := range split(inboundPortsInclude) {
					ip6tables("-t", "nat", "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", port, "-j", "ISTIO_IN_REDIRECT").RunOrFail()
				}
			}
		}
		// Create a new chain for selectively redirecting outbound packets to Envoy.
		ip6tables("-t", "nat", "-N", "ISTIO_OUTPUT").RunOrFail()
		// Jump to the ISTIO_OUTPUT chain from OUTPUT chain for all tcp traffic.
		ip6tables("-t", "nat", "-A", "OUTPUT", "-p", "tcp", "-j", "ISTIO_OUTPUT").RunOrFail()
		// Redirect app calls to back itself via Envoy when using the service VIP or endpoint
		// address, e.g. appN => Envoy (client) => Envoy (server) => appN.
		ip6tables("-t", "nat", "-A", "ISTIO_OUTPUT", "-o", "lo", "!", "-d", "::1/128", "-j", "ISTIO_REDIRECT").RunOrFail()

		for _, uid := range split(proxyUID) {
			// Avoid infinite loops. Don't redirect Envoy traffic directly back to
			// Envoy for non-loopback traffic.
			ip6tables("-t", "nat", "-A", "ISTIO_OUTPUT", "-m", "owner", "--uid-owner", uid, "-j", "RETURN").RunOrFail()
		}

		for _, gid := range split(proxyGID) {
			// Avoid infinite loops. Don't redirect Envoy traffic directly back to
			// Envoy for non-loopback traffic.
			ip6tables("-t", "nat", "-A", "ISTIO_OUTPUT", "-m", "owner", "--gid-owner", gid, "-j", "RETURN").RunOrFail()
		}
		// Skip redirection for Envoy-aware applications and
		// container-to-container traffic both of which explicitly use
		// localhost.
		ip6tables("-t", "nat", "-A", "ISTIO_OUTPUT", "-d", "::1/128", "-j", "RETURN").RunOrFail()
		// Apply outbound IPv6 exclusions. Must be applied before inclusions.
		for _, cidr := range ipv6RangesExclude.IPNets {
			ip6tables("-t", "nat", "-A", "ISTIO_OUTPUT", "-d", cidr.String(), "-j", "RETURN").RunOrFail()
		}
		// Apply outbound IPv6 inclusions.
		if ipv6RangesInclude.IsWildcard {
			// Wildcard specified. Redirect all remaining outbound traffic to Envoy.
			ip6tables("-t", "nat", "-A", "ISTIO_OUTPUT", "-j", "ISTIO_REDIRECT").RunOrFail()
			for _, internalInterface := range split(kubevirtInterfaces) {
				ip6tables("-t", "nat", "-I", "PREROUTING", "1", "-i", internalInterface, "-j", "RETURN").RunOrFail()
			}
		} else if len(ipv6RangesInclude.IPNets) > 0 {
			// User has specified a non-empty list of cidrs to be redirected to Envoy.
			for _, cidr := range ipv6RangesInclude.IPNets {
				for _, internalInterface := range split(kubevirtInterfaces) {
					ip6tables("-t", "nat", "-I", "PREROUTING", "1", "-i", internalInterface, "-d", cidr.String(), "-j", "ISTIO_REDIRECT").RunOrFail()
				}
				ip6tables("-t", "nat", "-A", "ISTIO_OUTPUT", "-d", cidr.String(), "-j", "ISTIO_REDIRECT").RunOrFail()
			}
			// All other traffic is not redirected.
			ip6tables("-t", "nat", "-A", "ISTIO_OUTPUT", "-j", "RETURN").RunOrFail()
		}
	} else {
		// Drop all inbound traffic except established connections.
		ip6tables("-F", "INPUT").RunQuietlyAndIgnore()
		ip6tables("-A", "INPUT", "-m", "state", "--state", "ESTABLISHED", "-j", "ACCEPT").RunQuietlyAndIgnore()
		ip6tables("-A", "INPUT", "-i", "lo", "-d", "::1", "-j", "ACCEPT").RunQuietlyAndIgnore()
		ip6tables("-A", "INPUT", "-j", "REJECT").RunQuietlyAndIgnore()
	}
}

func main() {
	fmt.Println(os.Args)
	run(os.Args[1:], flag.CommandLine, getLocalIP)
	fmt.Println("istio-iptables run successful")
}
