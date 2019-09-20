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
	"os"
	"strings"

	"istio.io/istio/tools/istio-iptables/pkg/config"

	"istio.io/istio/tools/istio-iptables/pkg/constants"

	"github.com/spf13/viper"

	"github.com/spf13/cobra"
	"istio.io/pkg/log"
)

var rootCmd = &cobra.Command{
	Use:  "istio-iptables",
	Long: "Script responsible for setting up port forwarding for Istio sidecar.",
	Run: func(cmd *cobra.Command, args []string) {
		config := constructConfig()
		run(config)
	},
}

func constructConfig() *config.Config {
	return &config.Config{
		ProxyPort:               viper.GetString(constants.EnvoyPort),
		InboundCapturePort:      viper.GetString(constants.InboundCapturePort),
		ProxyUID:                viper.GetString(constants.ProxyUid),
		ProxyGID:                viper.GetString(constants.ProxyGid),
		InboundInterceptionMode: viper.GetString(constants.InboundInterceptionMode),
		InboundTProxyMark:       viper.GetString(constants.InboundTProxyMark),
		InboundTProxyRouteTable: viper.GetString(constants.InboundTProxyRouteTable),
		InboundPortsInclude:     viper.GetString(constants.InboundPorts),
		InboundPortsExclude:     viper.GetString(constants.LocalExcludePorts),
		OutboundPortsExclude:    viper.GetString(constants.LocalOutboundPortsExclude),
		OutboundIPRangesInclude: viper.GetString(constants.ServiceCidr),
		OutboundIPRangesExclude: viper.GetString(constants.ServiceExcludeCidr),
		KubevirtInterfaces:      viper.GetString(constants.KubeVirtInterfaces),
		DryRun:                  viper.GetBool(constants.DryRun),
		EnableInboundIPv6s:      nil,
		Clean:                   viper.GetBool(constants.Clean),
	}
}

func init() {
	// Read in all environment variables
	viper.AutomaticEnv()
	// Replace - with _; so that environment variables are looked up correctly.
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	var envoyPort = "15001"
	var inboundPort = "15006"

	rootCmd.Flags().StringP(constants.EnvoyPort, "p", "", "Specify the envoy port to which redirect all TCP traffic (default $ENVOY_PORT = 15001)")
	viper.BindPFlag(constants.EnvoyPort, rootCmd.Flags().Lookup(constants.EnvoyPort))
	viper.SetDefault(constants.EnvoyPort, envoyPort)

	rootCmd.Flags().StringP(constants.InboundCapturePort, "z", "", "Port to which all inbound TCP traffic to the pod/VM should be redirected to (default $INBOUND_CAPTURE_PORT = 15006)")
	viper.BindPFlag(constants.InboundCapturePort, rootCmd.Flags().Lookup(constants.InboundCapturePort))
	viper.SetDefault(constants.InboundCapturePort, inboundPort)

	rootCmd.Flags().StringP(constants.ProxyUid, "u", "", "Specify the UID of the user for which the redirection is not applied. Typically, this is the UID of the proxy container")
	viper.BindPFlag(constants.ProxyUid, rootCmd.Flags().Lookup(constants.ProxyUid))
	viper.SetDefault(constants.ProxyUid, "")

	rootCmd.Flags().StringP(constants.ProxyGid, "g", "", "Specify the GID of the user for which the redirection is not applied. (same default value as -u param)")
	viper.BindPFlag(constants.ProxyGid, rootCmd.Flags().Lookup(constants.ProxyGid))
	viper.SetDefault(constants.ProxyGid, "")

	rootCmd.Flags().StringP(constants.InboundInterceptionMode, "m", "", "The mode used to redirect inbound connections to Envoy, either \"REDIRECT\" or \"TPROXY\"")
	viper.BindPFlag(constants.InboundInterceptionMode, rootCmd.Flags().Lookup(constants.InboundInterceptionMode))
	viper.SetDefault(constants.InboundInterceptionMode, "")

	rootCmd.Flags().StringP(constants.InboundPorts, "b", "", "Comma separated list of inbound ports for which traffic is to be redirected to Envoy (optional). "+
		"The wildcard character \"*\" can be used to configure redirection for all ports. An empty list will disable")
	viper.BindPFlag(constants.InboundPorts, rootCmd.Flags().Lookup(constants.InboundPorts))
	viper.SetDefault(constants.InboundPorts, "")

	rootCmd.Flags().StringP(constants.LocalExcludePorts, "d", "", "Comma separated list of inbound ports to be excluded from redirection to Envoy (optional). "+
		"Only applies  when all inbound traffic (i.e. \"*\") is being redirected (default to $ISTIO_LOCAL_EXCLUDE_PORTS)")
	viper.BindPFlag(constants.LocalExcludePorts, rootCmd.Flags().Lookup(constants.LocalExcludePorts))
	viper.SetDefault(constants.LocalExcludePorts, "")

	rootCmd.Flags().StringP(constants.ServiceCidr, "i", "", "Comma separated list of IP ranges in CIDR form to redirect to envoy (optional). "+
		"The wildcard character \"*\" can be used to redirect all outbound traffic. An empty list will disable all outbound")
	viper.BindPFlag(constants.ServiceCidr, rootCmd.Flags().Lookup(constants.ServiceCidr))
	viper.SetDefault(constants.ServiceCidr, "")

	rootCmd.Flags().StringP(constants.ServiceExcludeCidr, "x", "", "Comma separated list of IP ranges in CIDR form to be excluded from redirection. "+
		"Only applies when all  outbound traffic (i.e. \"*\") is being redirected (default to $ISTIO_SERVICE_EXCLUDE_CIDR)")
	viper.BindPFlag(constants.ServiceExcludeCidr, rootCmd.Flags().Lookup(constants.ServiceExcludeCidr))
	viper.SetDefault(constants.ServiceExcludeCidr, "")

	rootCmd.Flags().StringP(constants.LocalOutboundPortsExclude, "o", "", "Comma separated list of outbound ports to be excluded from redirection to Envoy")
	viper.BindPFlag(constants.LocalOutboundPortsExclude, rootCmd.Flags().Lookup(constants.LocalOutboundPortsExclude))
	viper.SetDefault(constants.LocalOutboundPortsExclude, "")

	rootCmd.Flags().StringP(constants.KubeVirtInterfaces, "k", "", "Comma separated list of virtual interfaces whose inbound traffic (from VM) will be treated as outbound")
	viper.BindPFlag(constants.KubeVirtInterfaces, rootCmd.Flags().Lookup(constants.KubeVirtInterfaces))
	viper.SetDefault(constants.KubeVirtInterfaces, "")

	rootCmd.Flags().StringP(constants.InboundTProxyMark, "t", "", "")
	viper.BindPFlag(constants.InboundTProxyMark, rootCmd.Flags().Lookup(constants.InboundTProxyMark))
	viper.SetDefault(constants.InboundTProxyMark, "1337")

	rootCmd.Flags().StringP(constants.InboundTProxyRouteTable, "r", "", "")
	viper.BindPFlag(constants.InboundTProxyRouteTable, rootCmd.Flags().Lookup(constants.InboundTProxyRouteTable))
	viper.SetDefault(constants.InboundTProxyRouteTable, "133")

	rootCmd.Flags().BoolP(constants.DryRun, "n", true, "Do not call any external dependencies like iptables")
	viper.BindPFlag(constants.DryRun, rootCmd.Flags().Lookup(constants.DryRun))
	viper.SetDefault(constants.DryRun, false)

	rootCmd.Flags().Bool(constants.Clean, false, "only clean iptables rules")
	viper.BindPFlag(constants.Clean, rootCmd.Flags().Lookup(constants.Clean))
	viper.SetDefault(constants.Clean, false)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		log.Errora(err)
		os.Exit(1)
	}
}
