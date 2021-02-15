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

package cmd

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"strconv"
	"strings"

	"github.com/miekg/dns"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"istio.io/istio/tools/istio-iptables/pkg/config"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
	dep "istio.io/istio/tools/istio-iptables/pkg/dependencies"
	"istio.io/istio/tools/istio-iptables/pkg/validation"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

var (
	envoyUserVar = env.RegisterStringVar(constants.EnvoyUser, "istio-proxy", "Envoy proxy username")
	// Enable interception of DNS.
	dnsCaptureByAgent = env.RegisterBoolVar("ISTIO_META_DNS_CAPTURE", false,
		"If set to true, enable the capture of outgoing DNS packets on port 53, redirecting to istio-agent on :15053").Get()
)

var rootCmd = &cobra.Command{
	Use:    "istio-iptables",
	Short:  "Set up iptables rules for Istio Sidecar",
	Long:   "istio-iptables is responsible for setting up port forwarding for Istio Sidecar.",
	PreRun: bindFlags,
	Run: func(cmd *cobra.Command, args []string) {
		cfg := constructConfig()
		var ext dep.Dependencies
		if cfg.DryRun {
			ext = &dep.StdoutStubDependencies{}
		} else {
			ext = &dep.RealDependencies{}
		}

		iptConfigurator := NewIptablesConfigurator(cfg, ext)
		if !cfg.SkipRuleApply {
			iptConfigurator.run()
		}
		if cfg.RunValidation {
			hostIP, err := getLocalIP()
			if err != nil {
				// Assume it is not handled by istio-cni and won't reuse the ValidationErrorCode
				panic(err)
			}
			validator := validation.NewValidator(cfg, hostIP)

			if err := validator.Run(); err != nil {
				handleErrorWithCode(err, constants.ValidationErrorCode)
			}
		}
	},
}

func constructConfig() *config.Config {
	cfg := &config.Config{
		DryRun:                  viper.GetBool(constants.DryRun),
		RestoreFormat:           viper.GetBool(constants.RestoreFormat),
		ProxyPort:               viper.GetString(constants.EnvoyPort),
		InboundCapturePort:      viper.GetString(constants.InboundCapturePort),
		InboundTunnelPort:       viper.GetString(constants.InboundTunnelPort),
		ProxyUID:                viper.GetString(constants.ProxyUID),
		ProxyGID:                viper.GetString(constants.ProxyGID),
		InboundInterceptionMode: viper.GetString(constants.InboundInterceptionMode),
		InboundTProxyMark:       viper.GetString(constants.InboundTProxyMark),
		InboundTProxyRouteTable: viper.GetString(constants.InboundTProxyRouteTable),
		InboundPortsInclude:     viper.GetString(constants.InboundPorts),
		InboundPortsExclude:     viper.GetString(constants.LocalExcludePorts),
		OutboundPortsInclude:    viper.GetString(constants.OutboundPorts),
		OutboundPortsExclude:    viper.GetString(constants.LocalOutboundPortsExclude),
		OutboundIPRangesInclude: viper.GetString(constants.ServiceCidr),
		OutboundIPRangesExclude: viper.GetString(constants.ServiceExcludeCidr),
		KubevirtInterfaces:      viper.GetString(constants.KubeVirtInterfaces),
		IptablesProbePort:       uint16(viper.GetUint(constants.IptablesProbePort)),
		ProbeTimeout:            viper.GetDuration(constants.ProbeTimeout),
		SkipRuleApply:           viper.GetBool(constants.SkipRuleApply),
		RunValidation:           viper.GetBool(constants.RunValidation),
		RedirectDNS:             viper.GetBool(constants.RedirectDNS),
	}

	// TODO: Make this more configurable, maybe with an allowlist of users to be captured for output instead of a denylist.
	if cfg.ProxyUID == "" {
		usr, err := user.Lookup(envoyUserVar.Get())
		var userID string
		// Default to the UID of ENVOY_USER
		if err != nil {
			userID = constants.DefaultProxyUID
		} else {
			userID = usr.Uid
		}
		cfg.ProxyUID = userID
	}
	// For TPROXY as its uid and gid are same.
	if cfg.ProxyGID == "" {
		cfg.ProxyGID = cfg.ProxyUID
	}

	// Detect whether IPv6 is enabled by checking if the pod's IP address is IPv4 or IPv6.
	podIP, err := getLocalIP()
	if err != nil {
		panic(err)
	}
	cfg.EnableInboundIPv6 = podIP.To4() == nil

	// Lookup DNS nameservers. We only do this if DNS is enabled in case of some obscure theoretical
	// case where reading /etc/resolv.conf could fail.
	if cfg.RedirectDNS {
		dnsConfig, err := dns.ClientConfigFromFile("/etc/resolv.conf")
		if err != nil {
			panic(fmt.Sprintf("failed to load /etc/resolv.conf: %v", err))
		}
		cfg.DNSServersV4, cfg.DNSServersV6 = SplitV4V6(dnsConfig.Servers)
	}
	return cfg
}

// getLocalIP returns the local IP address
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

func handleError(err error) {
	handleErrorWithCode(err, 1)
}

func handleErrorWithCode(err error, code int) {
	log.Error(err)
	os.Exit(code)
}

// https://github.com/spf13/viper/issues/233.
// Any viper mutation and binding should be placed in `PreRun` since they should be dynamically bound to the subcommand being executed.
func bindFlags(cmd *cobra.Command, args []string) {
	// Read in all environment variables
	viper.AutomaticEnv()
	// Replace - with _; so that environment variables are looked up correctly.
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	envoyPort := "15001"
	inboundPort := "15006"
	inboundTunnelPort := "15008"

	if err := viper.BindPFlag(constants.EnvoyPort, cmd.Flags().Lookup(constants.EnvoyPort)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.EnvoyPort, envoyPort)

	if err := viper.BindPFlag(constants.InboundCapturePort, cmd.Flags().Lookup(constants.InboundCapturePort)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.InboundCapturePort, inboundPort)

	if err := viper.BindPFlag(constants.InboundTunnelPort, cmd.Flags().Lookup(constants.InboundTunnelPort)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.InboundTunnelPort, inboundTunnelPort)

	if err := viper.BindPFlag(constants.ProxyUID, cmd.Flags().Lookup(constants.ProxyUID)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.ProxyUID, "")

	if err := viper.BindPFlag(constants.ProxyGID, cmd.Flags().Lookup(constants.ProxyGID)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.ProxyGID, "")

	if err := viper.BindPFlag(constants.InboundInterceptionMode, cmd.Flags().Lookup(constants.InboundInterceptionMode)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.InboundInterceptionMode, "")

	if err := viper.BindPFlag(constants.InboundPorts, cmd.Flags().Lookup(constants.InboundPorts)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.InboundPorts, "")

	if err := viper.BindPFlag(constants.LocalExcludePorts, cmd.Flags().Lookup(constants.LocalExcludePorts)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.LocalExcludePorts, "")

	if err := viper.BindPFlag(constants.ServiceCidr, cmd.Flags().Lookup(constants.ServiceCidr)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.ServiceCidr, "")

	if err := viper.BindPFlag(constants.ServiceExcludeCidr, cmd.Flags().Lookup(constants.ServiceExcludeCidr)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.ServiceExcludeCidr, "")

	if err := viper.BindPFlag(constants.OutboundPorts, cmd.Flags().Lookup(constants.OutboundPorts)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.OutboundPorts, "")

	if err := viper.BindPFlag(constants.LocalOutboundPortsExclude, cmd.Flags().Lookup(constants.LocalOutboundPortsExclude)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.LocalOutboundPortsExclude, "")

	if err := viper.BindPFlag(constants.KubeVirtInterfaces, cmd.Flags().Lookup(constants.KubeVirtInterfaces)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.KubeVirtInterfaces, "")

	if err := viper.BindPFlag(constants.InboundTProxyMark, cmd.Flags().Lookup(constants.InboundTProxyMark)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.InboundTProxyMark, "1337")

	if err := viper.BindPFlag(constants.InboundTProxyRouteTable, cmd.Flags().Lookup(constants.InboundTProxyRouteTable)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.InboundTProxyRouteTable, "133")

	if err := viper.BindPFlag(constants.DryRun, cmd.Flags().Lookup(constants.DryRun)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.DryRun, false)

	if err := viper.BindPFlag(constants.RestoreFormat, cmd.Flags().Lookup(constants.RestoreFormat)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.RestoreFormat, true)

	if err := viper.BindPFlag(constants.IptablesProbePort, cmd.Flags().Lookup(constants.IptablesProbePort)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.IptablesProbePort, strconv.Itoa(constants.DefaultIptablesProbePort))

	if err := viper.BindPFlag(constants.ProbeTimeout, cmd.Flags().Lookup(constants.ProbeTimeout)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.ProbeTimeout, constants.DefaultProbeTimeout)

	if err := viper.BindPFlag(constants.SkipRuleApply, cmd.Flags().Lookup(constants.SkipRuleApply)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.SkipRuleApply, false)

	if err := viper.BindPFlag(constants.RunValidation, cmd.Flags().Lookup(constants.RunValidation)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.RunValidation, false)

	if err := viper.BindPFlag(constants.RedirectDNS, cmd.Flags().Lookup(constants.RedirectDNS)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.RedirectDNS, dnsCaptureByAgent)
}

// https://github.com/spf13/viper/issues/233.
// Only adding flags in `init()` while moving its binding to Viper and value defaulting as part of the command execution.
// Otherwise, the flag with the same name shared across subcommands will be overwritten by the last.
func init() {
	rootCmd.Flags().StringP(constants.EnvoyPort, "p", "", "Specify the envoy port to which redirect all TCP traffic (default $ENVOY_PORT = 15001)")

	rootCmd.Flags().StringP(constants.InboundCapturePort, "z", "",
		"Port to which all inbound TCP traffic to the pod/VM should be redirected to (default $INBOUND_CAPTURE_PORT = 15006)")

	rootCmd.Flags().StringP(constants.InboundTunnelPort, "e", "",
		"Specify the istio tunnel port for inbound tcp traffic (default $INBOUND_TUNNEL_PORT = 15008)")

	rootCmd.Flags().StringP(constants.ProxyUID, "u", "",
		"Specify the UID of the user for which the redirection is not applied. Typically, this is the UID of the proxy container")

	rootCmd.Flags().StringP(constants.ProxyGID, "g", "",
		"Specify the GID of the user for which the redirection is not applied. (same default value as -u param)")

	rootCmd.Flags().StringP(constants.InboundInterceptionMode, "m", "",
		"The mode used to redirect inbound connections to Envoy, either \"REDIRECT\" or \"TPROXY\"")

	rootCmd.Flags().StringP(constants.InboundPorts, "b", "",
		"Comma separated list of inbound ports for which traffic is to be redirected to Envoy (optional). "+
			"The wildcard character \"*\" can be used to configure redirection for all ports. An empty list will disable")

	rootCmd.Flags().StringP(constants.LocalExcludePorts, "d", "",
		"Comma separated list of inbound ports to be excluded from redirection to Envoy (optional). "+
			"Only applies  when all inbound traffic (i.e. \"*\") is being redirected (default to $ISTIO_LOCAL_EXCLUDE_PORTS)")

	rootCmd.Flags().StringP(constants.ServiceCidr, "i", "",
		"Comma separated list of IP ranges in CIDR form to redirect to envoy (optional). "+
			"The wildcard character \"*\" can be used to redirect all outbound traffic. An empty list will disable all outbound")

	rootCmd.Flags().StringP(constants.ServiceExcludeCidr, "x", "",
		"Comma separated list of IP ranges in CIDR form to be excluded from redirection. "+
			"Only applies when all  outbound traffic (i.e. \"*\") is being redirected (default to $ISTIO_SERVICE_EXCLUDE_CIDR)")

	rootCmd.Flags().StringP(constants.OutboundPorts, "q", "",
		"Comma separated list of outbound ports to be explicitly included for redirection to Envoy")

	rootCmd.Flags().StringP(constants.LocalOutboundPortsExclude, "o", "",
		"Comma separated list of outbound ports to be excluded from redirection to Envoy")

	rootCmd.Flags().StringP(constants.KubeVirtInterfaces, "k", "",
		"Comma separated list of virtual interfaces whose inbound traffic (from VM) will be treated as outbound")

	rootCmd.Flags().StringP(constants.InboundTProxyMark, "t", "", "")

	rootCmd.Flags().StringP(constants.InboundTProxyRouteTable, "r", "", "")

	rootCmd.Flags().BoolP(constants.DryRun, "n", false, "Do not call any external dependencies like iptables")

	rootCmd.Flags().BoolP(constants.RestoreFormat, "f", true, "Print iptables rules in iptables-restore interpretable format")

	rootCmd.Flags().String(constants.IptablesProbePort, strconv.Itoa(constants.DefaultIptablesProbePort), "set listen port for failure detection")

	rootCmd.Flags().Duration(constants.ProbeTimeout, constants.DefaultProbeTimeout, "failure detection timeout")

	rootCmd.Flags().Bool(constants.SkipRuleApply, false, "Skip iptables apply")

	rootCmd.Flags().Bool(constants.RunValidation, false, "Validate iptables")

	rootCmd.Flags().Bool(constants.RedirectDNS, dnsCaptureByAgent, "Enable capture of dns traffic by istio-agent")
}

func GetCommand() *cobra.Command {
	return rootCmd
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		handleError(err)
	}
}
