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
		godotenv.Load(path)
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
	return nil, fmt.Errorf("No valid local IP address found")
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

func (i Command) RedirectStdErr() Command {
	i.RedirectToStdErr = true
	return i
}

func (i Command) RunOrIgnore(message string) {
	err := i.Run()
	if err != nil {
		fmt.Println(message)
	}
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

func split(s string, sep string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, sep)
}

func separateV4V6(cidrList string) (NetworkRange, NetworkRange, error) {
	if cidrList == "*" {
		return NetworkRange{IsWildcard: true}, NetworkRange{IsWildcard: true}, nil
	}
	ipv6_ranges := NetworkRange{IsWildcard: false, IPNets: make([]*net.IPNet, 0)}
	ipv4_ranges := NetworkRange{IsWildcard: false, IPNets: make([]*net.IPNet, 0)}
	for _, ipRange := range split(cidrList, ",") {
		ip, ipNet, err := net.ParseCIDR(ipRange)
		if err != nil {
			return ipv4_ranges, ipv6_ranges, err
		}
		if ip.To4() != nil {
			ipv4_ranges.IPNets = append(ipv4_ranges.IPNets, ipNet)
		} else {
			ipv6_ranges.IPNets = append(ipv6_ranges.IPNets, ipNet)
		}
	}
	return ipv4_ranges, ipv6_ranges, nil
}

func dump() {
	iptables("save").RunOrFail()
	ip6tables("save").RunOrFail()
}

func run(args []string, flagSet *flag.FlagSet, getLocalIP func() (net.IP, error)) {

	// The cluster env can be used for common cluster settings, pushed to all VMs in the cluster.
	// This allows separating per-machine settings (the list of inbound ports, local path overrides) from cluster wide
	// settings (CIDR range)
	dotEnvLoad("ISTIO_CLUSTER_CONFIG", "/var/lib/istio/envoy/cluster.env")
	dotEnvLoad("ISTIO_SIDECAR_CONFIG", "/var/lib/istio/envoy/sidecar.env")

	PROXY_PORT := getEnvWithDefault("ENVOY_PORT", "15001")
	PROXY_UID := ""
	PROXY_GID := ""
	INBOUND_INTERCEPTION_MODE := os.Getenv("ISTIO_INBOUND_INTERCEPTION_MODE")
	INBOUND_TPROXY_MARK := getEnvWithDefault("ISTIO_INBOUND_TPROXY_MARK", "1337")
	INBOUND_TPROXY_ROUTE_TABLE := getEnvWithDefault("ISTIO_INBOUND_TPROXY_ROUTE_TABLE", "133")
	INBOUND_PORTS_INCLUDE := os.Getenv("ISTIO_INBOUND_PORTS")
	INBOUND_PORTS_EXCLUDE := os.Getenv("ISTIO_LOCAL_EXCLUDE_PORTS")
	OUTBOUND_IP_RANGES_INCLUDE := os.Getenv("ISTIO_SERVICE_CIDR")
	OUTBOUND_IP_RANGES_EXCLUDE := os.Getenv("ISTIO_SERVICE_EXCLUDE_CIDR")
	KUBEVIRT_INTERFACES := ""
	var ENABLE_INBOUND_IPV6 net.IP = nil

	flagSet.StringVar(&PROXY_PORT, "p", PROXY_PORT, "Specify the envoy port to which redirect all TCP traffic (default $ENVOY_PORT = 15001)")
	flagSet.StringVar(&PROXY_UID, "u", PROXY_UID, "Specify the UID of the user for which the redirection is not applied. Typically, this is the UID of the proxy container")
	flagSet.StringVar(&PROXY_GID, "g", PROXY_GID, "Specify the GID of the user for which the redirection is not applied. (same default value as -u param)")
	flagSet.StringVar(&INBOUND_INTERCEPTION_MODE, "m", INBOUND_INTERCEPTION_MODE, "The mode used to redirect inbound connections to Envoy, either \"REDIRECT\" or \"TPROXY\"")
	flagSet.StringVar(&INBOUND_PORTS_INCLUDE, "b", INBOUND_PORTS_INCLUDE, "Comma separated list of inbound ports for which traffic is to be redirected to Envoy (optional). The wildcard character \"*\" can be used to configure redirection for all ports. An empty list will disable")
	flagSet.StringVar(&INBOUND_PORTS_EXCLUDE, "d", INBOUND_PORTS_EXCLUDE, "Comma separated list of inbound ports to be excluded from redirection to Envoy (optional). Only applies  when all inbound traffic (i.e. \"*\") is being redirected (default to $ISTIO_LOCAL_EXCLUDE_PORTS)")
	flagSet.StringVar(&OUTBOUND_IP_RANGES_INCLUDE, "i", OUTBOUND_IP_RANGES_INCLUDE, "Comma separated list of IP ranges in CIDR form to redirect to envoy (optional). The wildcard character \"*\" can be used to redirect all outbound traffic. An empty list will disable all outbound")
	flagSet.StringVar(&OUTBOUND_IP_RANGES_EXCLUDE, "x", OUTBOUND_IP_RANGES_EXCLUDE, "Comma separated list of IP ranges in CIDR form to be excluded from redirection. Only applies when all  outbound traffic (i.e. \"*\") is being redirected (default to $ISTIO_SERVICE_EXCLUDE_CIDR)")
	flagSet.StringVar(&KUBEVIRT_INTERFACES, "k", KUBEVIRT_INTERFACES, "Comma separated list of virtual interfaces whose inbound traffic (from VM) will be treated as outbound (optional)")

	flagSet.Parse(args)

	defer dump()

	// TODO: more flexibility - maybe a whitelist of users to be captured for output instead of a blacklist.
	if PROXY_UID == "" {
		usr, err := user.Lookup(getEnvWithDefault("ENVOY_USER", "istio-proxy"))
		var userId string
		// Default to the UID of ENVOY_USER and root
		if err != nil {
			userId = "1337"
		} else {
			userId = usr.Uid
		}
		// If ENVOY_UID is not explicitly defined (as it would be in k8s env), we add root to the list,
		// for ca agent.
		PROXY_UID = userId + ",0"
	}

	// for TPROXY as its uid and gid are same
	if PROXY_GID == "" {
		PROXY_GID = PROXY_UID
	}

	POD_IP, err := getLocalIP()
	if err != nil {
		panic(err)
	}
	// Check if pod's ip is ipv4 or ipv6, in case of ipv6 set variable
	// to program ip6tablesOrFail
	if POD_IP.To4() == nil {
		ENABLE_INBOUND_IPV6 = POD_IP
	}
	//
	// Since OUTBOUND_IP_RANGES_EXCLUDE could carry ipv4 and ipv6 ranges
	// need to split them in different arrays one for ipv4 and one for ipv6
	// in order to not to fail
	ipv4_ranges_exclude, ipv6_ranges_exclude, err := separateV4V6(OUTBOUND_IP_RANGES_EXCLUDE)
	if err != nil {
		panic(err)
	}
	if ipv4_ranges_exclude.IsWildcard {
		panic("Invalid value for OUTBOUND_IP_RANGES_EXCLUDE")
	}

	ipv4_ranges_include, ipv6_ranges_include, err := separateV4V6(OUTBOUND_IP_RANGES_INCLUDE)
	if err != nil {
		panic(err)
	}

	// Remove the old chains, to generate new configs.
	iptables("-t", "nat", "-D", "PREROUTING", "-p", "tcp", "-j", "ISTIO_INBOUND").RedirectStdErr().RunOrFail()
	iptables("-t", "mangle", "-D", "PREROUTING", "-p", "tcp", "-j", "ISTIO_INBOUND").RedirectStdErr().RunOrFail()
	iptables("-t", "nat", "-D", "OUTPUT", "-p", "tcp", "-j", "ISTIO_OUTPUT").RedirectStdErr().RunOrFail()
	// Flush and delete the istio chains.
	iptables("-t", "nat", "-F", "ISTIO_OUTPUT").RedirectStdErr().RunOrFail()
	iptables("-t", "nat", "-X", "ISTIO_OUTPUT").RedirectStdErr().RunOrFail()
	iptables("-t", "nat", "-F", "ISTIO_INBOUND").RedirectStdErr().RunOrFail()
	iptables("-t", "nat", "-X", "ISTIO_INBOUND").RedirectStdErr().RunOrFail()
	iptables("-t", "mangle", "-F", "ISTIO_INBOUND").RedirectStdErr().RunOrFail()
	iptables("-t", "mangle", "-X", "ISTIO_INBOUND").RedirectStdErr().RunOrFail()
	iptables("-t", "mangle", "-F", "ISTIO_DIVERT").RedirectStdErr().RunOrFail()
	iptables("-t", "mangle", "-X", "ISTIO_DIVERT").RedirectStdErr().RunOrFail()
	iptables("-t", "mangle", "-F", "ISTIO_TPROXY").RedirectStdErr().RunOrFail()
	iptables("-t", "mangle", "-X", "ISTIO_TPROXY").RedirectStdErr().RunOrFail()
	// Must be last, the others refer to it
	iptables("-t", "nat", "-F", "ISTIO_REDIRECT").RedirectStdErr().RunOrFail()
	iptables("-t", "nat", "-X", "ISTIO_REDIRECT").RedirectStdErr().RunOrFail()
	iptables("-t", "nat", "-F", "ISTIO_IN_REDIRECT").RedirectStdErr().RunOrFail()
	iptables("-t", "nat", "-X", "ISTIO_IN_REDIRECT").RedirectStdErr().RunOrFail()

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
	fmt.Printf("PROXY_PORT=%s\n", PROXY_PORT)
	fmt.Printf("INBOUND_CAPTURE_PORT=%s\n", getEnvWithDefault("INBOUND_CAPTURE_PORT", "$PROXY_PORT"))
	fmt.Printf("PROXY_UID=%s\n", PROXY_UID)
	fmt.Printf("INBOUND_INTERCEPTION_MODE=%s\n", INBOUND_INTERCEPTION_MODE)
	fmt.Printf("INBOUND_TPROXY_MARK=%s\n", INBOUND_TPROXY_MARK)
	fmt.Printf("INBOUND_TPROXY_ROUTE_TABLE=%s\n", INBOUND_TPROXY_ROUTE_TABLE)
	fmt.Printf("INBOUND_PORTS_INCLUDE=%s\n", INBOUND_PORTS_INCLUDE)
	fmt.Printf("INBOUND_PORTS_EXCLUDE=%s\n", INBOUND_PORTS_EXCLUDE)
	fmt.Printf("OUTBOUND_IP_RANGES_INCLUDE=%s\n", OUTBOUND_IP_RANGES_INCLUDE)
	fmt.Printf("OUTBOUND_IP_RANGES_EXCLUDE=%s\n", OUTBOUND_IP_RANGES_EXCLUDE)
	fmt.Printf("KUBEVIRT_INTERFACES=%s\n", KUBEVIRT_INTERFACES)
	fmt.Printf("ENABLE_INBOUND_IPV6=%s\n", ENABLE_INBOUND_IPV6)
	fmt.Println("")

	INBOUND_CAPTURE_PORT := getEnvWithDefault("INBOUND_CAPTURE_PORT", PROXY_PORT)

	// Create a new chain for redirecting outbound traffic to the common Envoy port.
	// In both chains, '-j RETURN' bypasses Envoy and '-j ISTIO_REDIRECT'
	// redirects to Envoy.
	iptables("-t", "nat", "-N", "ISTIO_REDIRECT").RunOrFail()
	iptables("-t", "nat", "-A", "ISTIO_REDIRECT", "-p", "tcp", "-j", "REDIRECT", "--to-port", PROXY_PORT).RunOrFail()
	// Use this chain also for redirecting inbound traffic to the common Envoy port
	// when not using TPROXY.
	iptables("-t", "nat", "-N", "ISTIO_IN_REDIRECT").RunOrFail()
	iptables("-t", "nat", "-A", "ISTIO_IN_REDIRECT", "-p", "tcp", "-j", "REDIRECT", "--to-port", INBOUND_CAPTURE_PORT).RunOrFail()

	table := "nat"
	// Handling of inbound ports. Traffic will be redirected to Envoy, which will process and forward
	// to the local service. If not set, no inbound port will be intercepted by istio iptablesOrFail.
	if INBOUND_PORTS_INCLUDE != "" {
		if INBOUND_INTERCEPTION_MODE == "TPROXY" {
			// When using TPROXY, create a new chain for routing all inbound traffic to
			// Envoy. Any packet entering this chain gets marked with the ${INBOUND_TPROXY_MARK} mark,
			// so that they get routed to the loopback interface in order to get redirected to Envoy.
			// In the ISTIO_INBOUND chain, '-j ISTIO_DIVERT' reroutes to the loopback
			// interface.
			// Mark all inbound packets.
			iptables("-t", "mangle", "-N", "ISTIO_DIVERT").RunOrFail()
			iptables("-t", "mangle", "-A", "ISTIO_DIVERT", "-j", "MARK", "--set-mark", INBOUND_TPROXY_MARK).RunOrFail()
			iptables("-t", "mangle", "-A", "ISTIO_DIVERT", "-j", "ACCEPT").RunOrFail()
			// Route all packets marked in chain ISTIO_DIVERT using routing table ${INBOUND_TPROXY_ROUTE_TABLE}.
			ip("-f", "inet", "rule", "add", "fwmark", INBOUND_TPROXY_MARK, "lookup", INBOUND_TPROXY_ROUTE_TABLE).RunOrFail()
			// In routing table ${INBOUND_TPROXY_ROUTE_TABLE}, create a single default rule to route all traffic to
			// the loopback interface.
			err = ip("-f", "inet", "route", "add", "local", "default", "dev", "lo", "table", INBOUND_TPROXY_ROUTE_TABLE).Run()
			if err != nil {
				ip("route", "show", "table", "all").RunOrFail()
			}
			// Create a new chain for redirecting inbound traffic to the common Envoy
			// port.
			// In the ISTIO_INBOUND chain, '-j RETURN' bypasses Envoy and
			// '-j ISTIO_TPROXY' redirects to Envoy.
			iptables("-t", "mangle", "-N", "ISTIO_TPROXY").RunOrFail()
			iptables("-t", "mangle", "-A", "ISTIO_TPROXY", "!", "-d", "127.0.0.1/32", "-p", "tcp", "-j", "TPROXY", "--tproxy-mark", INBOUND_TPROXY_MARK+"/0xffffffff", "--on-port", PROXY_PORT).RunOrFail()

			table = "mangle"
		} else {
			table = "nat"
		}
		iptables("-t", table, "-N", "ISTIO_INBOUND").RunOrFail()
		iptables("-t", table, "-A", "PREROUTING", "-p", "tcp", "-j", "ISTIO_INBOUND").RunOrFail()

		if INBOUND_PORTS_INCLUDE == "*" {
			// Makes sure SSH is not redirected
			iptables("-t", table, "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", "22", "-j", "RETURN").RunOrFail()
			// Apply any user-specified port exclusions.
			if INBOUND_PORTS_EXCLUDE != "" {
				for _, port := range split(INBOUND_PORTS_EXCLUDE, ",") {
					iptables("-t", table, "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", port, "-j", "RETURN").RunOrFail()
				}
			}
			// Redirect remaining inbound traffic to Envoy.
			if INBOUND_INTERCEPTION_MODE == "TPROXY" {
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
			for _, port := range split(INBOUND_PORTS_INCLUDE, ",") {
				if INBOUND_INTERCEPTION_MODE == "TPROXY" {
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

	for _, uid := range split(PROXY_UID, ",") {
		// Avoid infinite loops. Don't redirect Envoy traffic directly back to
		// Envoy for non-loopback traffic.
		iptables("-t", "nat", "-A", "ISTIO_OUTPUT", "-m", "owner", "--uid-owner", uid, "-j", "RETURN").RunOrFail()
	}

	for _, gid := range split(PROXY_GID, ",") {
		// Avoid infinite loops. Don't redirect Envoy traffic directly back to
		// Envoy for non-loopback traffic.
		iptables("-t", "nat", "-A", "ISTIO_OUTPUT", "-m", "owner", "--gid-owner", gid, "-j", "RETURN").RunOrFail()
	}
	// Skip redirection for Envoy-aware applications and
	// container-to-container traffic both of which explicitly use
	// localhost.
	iptables("-t", "nat", "-A", "ISTIO_OUTPUT", "-d", "127.0.0.1/32", "-j", "RETURN").RunOrFail()
	// Apply outbound IPv4 exclusions. Must be applied before inclusions.
	for _, cidr := range ipv4_ranges_exclude.IPNets {
		iptables("-t", "nat", "-A", "ISTIO_OUTPUT", "-d", cidr.String(), "-j", "RETURN").RunOrFail()
	}

	for _, internalInterface := range split(KUBEVIRT_INTERFACES, ",") {
		iptables("-t", "nat", "-I", "PREROUTING", "1", "-i", internalInterface, "-j", "RETURN").RunOrFail()
	}
	// Apply outbound IP inclusions.
	if ipv4_ranges_include.IsWildcard {
		// Wildcard specified. Redirect all remaining outbound traffic to Envoy.
		iptables("-t", "nat", "-A", "ISTIO_OUTPUT", "-j", "ISTIO_REDIRECT").RunOrFail()
		for _, internalInterface := range split(KUBEVIRT_INTERFACES, ",") {
			iptables("-t", "nat", "-I", "PREROUTING", "1", "-i", internalInterface, "-j", "ISTIO_REDIRECT").RunOrFail()
		}
	} else if len(ipv4_ranges_include.IPNets) > 0 {
		// User has specified a non-empty list of cidrs to be redirected to Envoy.
		for _, cidr := range ipv4_ranges_include.IPNets {
			for _, internalInterface := range split(KUBEVIRT_INTERFACES, ",") {
				iptables("-t", "nat", "-I", "PREROUTING", "1", "-i", internalInterface, "-d", cidr.String(), "-j", "ISTIO_REDIRECT").RunOrFail()
			}
			iptables("-t", "nat", "-A", "ISTIO_OUTPUT", "-d", cidr.String(), "-j", "ISTIO_REDIRECT").RunOrFail()
		}
		// All other traffic is not redirected.
		iptables("-t", "nat", "-A", "ISTIO_OUTPUT", "-j", "RETURN").RunOrFail()
	}
	// If ENABLE_INBOUND_IPV6 is unset (default unset), restrict IPv6 traffic.
	if ENABLE_INBOUND_IPV6 != nil {
		// Remove the old chains, to generate new configs.
		ip6tables("-t", "nat", "-D", "PREROUTING", "-p", "tcp", "-j", "ISTIO_INBOUND").RedirectStdErr().RunOrFail()
		ip6tables("-t", "mangle", "-D", "PREROUTING", "-p", "tcp", "-j", "ISTIO_INBOUND").RedirectStdErr().RunOrFail()
		ip6tables("-t", "nat", "-D", "OUTPUT", "-p", "tcp", "-j", "ISTIO_OUTPUT").RedirectStdErr().RunOrFail()
		// Flush and delete the istio chains.
		ip6tables("-t", "nat", "-F", "ISTIO_OUTPUT").RedirectStdErr().RunOrFail()
		ip6tables("-t", "nat", "-X", "ISTIO_OUTPUT").RedirectStdErr().RunOrFail()
		ip6tables("-t", "nat", "-F", "ISTIO_INBOUND").RedirectStdErr().RunOrFail()
		ip6tables("-t", "nat", "-X", "ISTIO_INBOUND").RedirectStdErr().RunOrFail()
		ip6tables("-t", "mangle", "-F", "ISTIO_INBOUND").RedirectStdErr().RunOrFail()
		ip6tables("-t", "mangle", "-X", "ISTIO_INBOUND").RedirectStdErr().RunOrFail()
		ip6tables("-t", "mangle", "-F", "ISTIO_DIVERT").RedirectStdErr().RunOrFail()
		ip6tables("-t", "mangle", "-X", "ISTIO_DIVERT").RedirectStdErr().RunOrFail()
		ip6tables("-t", "mangle", "-F", "ISTIO_TPROXY").RedirectStdErr().RunOrFail()
		ip6tables("-t", "mangle", "-X", "ISTIO_TPROXY").RedirectStdErr().RunOrFail()
		// Must be last, the others refer to it
		ip6tables("-t", "nat", "-F", "ISTIO_REDIRECT").RedirectStdErr().RunOrFail()
		ip6tables("-t", "nat", "-X", "ISTIO_REDIRECT").RedirectStdErr().RunOrFail()
		ip6tables("-t", "nat", "-F", "ISTIO_IN_REDIRECT").RedirectStdErr().RunOrFail()
		ip6tables("-t", "nat", "-X", "ISTIO_IN_REDIRECT").RedirectStdErr().RunOrFail()
		// Create a new chain for redirecting outbound traffic to the common Envoy port.
		// In both chains, '-j RETURN' bypasses Envoy and '-j ISTIO_REDIRECT'
		// redirects to Envoy.
		ip6tables("-t", "nat", "-N", "ISTIO_REDIRECT").RunOrFail()
		ip6tables("-t", "nat", "-A", "ISTIO_REDIRECT", "-p", "tcp", "-j", "REDIRECT", "--to-port", PROXY_PORT).RunOrFail()
		// Use this chain also for redirecting inbound traffic to the common Envoy port
		// when not using TPROXY.
		ip6tables("-t", "nat", "-N", "ISTIO_IN_REDIRECT").RunOrFail()
		ip6tables("-t", "nat", "-A", "ISTIO_IN_REDIRECT", "-p", "tcp", "-j", "REDIRECT", "--to-port", INBOUND_CAPTURE_PORT).RunOrFail()
		// Handling of inbound ports. Traffic will be redirected to Envoy, which will process and forward
		// to the local service. If not set, no inbound port will be intercepted by istio iptablesOrFail.
		if INBOUND_PORTS_INCLUDE != "" {
			table = "nat"
			ip6tables("-t", table, "-N", "ISTIO_INBOUND").RunOrFail()
			ip6tables("-t", table, "-A", "PREROUTING", "-p", "tcp", "-j", "ISTIO_INBOUND").RunOrFail()

			if INBOUND_PORTS_INCLUDE == "*" {
				// Makes sure SSH is not redirected
				ip6tables("-t", table, "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", "22", "-j", "RETURN").RunOrFail()
				// Apply any user-specified port exclusions.
				if INBOUND_PORTS_EXCLUDE != "" {
					for _, port := range split(INBOUND_PORTS_EXCLUDE, ",") {
						ip6tables("-t", table, "-A", "ISTIO_INBOUND", "-p", "tcp", "--dport", port, "-j", "RETURN").RunOrFail()
					}
				}
			} else {
				// User has specified a non-empty list of ports to be redirected to Envoy.
				for _, port := range split(INBOUND_PORTS_INCLUDE, ",") {
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

		for _, uid := range split(PROXY_UID, ",") {
			// Avoid infinite loops. Don't redirect Envoy traffic directly back to
			// Envoy for non-loopback traffic.
			ip6tables("-t", "nat", "-A", "ISTIO_OUTPUT", "-m", "owner", "--uid-owner", uid, "-j", "RETURN").RunOrFail()
		}

		for _, gid := range split(PROXY_GID, ",") {
			// Avoid infinite loops. Don't redirect Envoy traffic directly back to
			// Envoy for non-loopback traffic.
			ip6tables("-t", "nat", "-A", "ISTIO_OUTPUT", "-m", "owner", "--gid-owner", gid, "-j", "RETURN").RunOrFail()
		}
		// Skip redirection for Envoy-aware applications and
		// container-to-container traffic both of which explicitly use
		// localhost.
		ip6tables("-t", "nat", "-A", "ISTIO_OUTPUT", "-d", "::1/128", "-j", "RETURN").RunOrFail()
		// Apply outbound IPv6 exclusions. Must be applied before inclusions.
		for _, cidr := range ipv6_ranges_exclude.IPNets {
			ip6tables("-t", "nat", "-A", "ISTIO_OUTPUT", "-d", cidr.String(), "-j", "RETURN").RunOrFail()
		}
		// Apply outbound IPv6 inclusions.
		if ipv6_ranges_include.IsWildcard {
			// Wildcard specified. Redirect all remaining outbound traffic to Envoy.
			ip6tables("-t", "nat", "-A", "ISTIO_OUTPUT", "-j", "ISTIO_REDIRECT").RunOrFail()
			for _, internalInterface := range split(KUBEVIRT_INTERFACES, ",") {
				ip6tables("-t", "nat", "-I", "PREROUTING", "1", "-i", internalInterface, "-j", "RETURN").RunOrFail()
			}
		} else if len(ipv6_ranges_include.IPNets) > 0 {
			// User has specified a non-empty list of cidrs to be redirected to Envoy.
			for _, cidr := range ipv6_ranges_include.IPNets {
				for _, internalInterface := range split(KUBEVIRT_INTERFACES, ",") {
					ip6tables("-t", "nat", "-I", "PREROUTING", "1", "-i", internalInterface, "-d", cidr.String(), "-j", "ISTIO_REDIRECT").RunOrFail()
				}
				ip6tables("-t", "nat", "-A", "ISTIO_OUTPUT", "-d", cidr.String(), "-j", "ISTIO_REDIRECT").RunOrFail()
			}
			// All other traffic is not redirected.
			ip6tables("-t", "nat", "-A", "ISTIO_OUTPUT", "-j", "RETURN").RunOrFail()
		}
	} else {
		// Drop all inbound traffic except established connections.
		ip6tables("-F", "INPUT").RedirectStdErr().RunOrFail()
		ip6tables("-A", "INPUT", "-m", "state", "--state", "ESTABLISHED", "-j", "ACCEPT").RedirectStdErr().RunOrFail()
		ip6tables("-A", "INPUT", "-i", "lo", "-d", "::1", "-j", "ACCEPT").RedirectStdErr().RunOrFail()
		ip6tables("-A", "INPUT", "-j", "REJECT").RedirectStdErr().RunOrFail()
	}
}

func main() {
	run(os.Args, flag.CommandLine, getLocalIP)
}
