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

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"istio.io/pkg/log"
)

var rootCmd = &cobra.Command{
	Use:  "istio-iptables",
	Long: "Script responsible for setting up port forwarding for Istio sidecar.",
	Run: func(cmd *cobra.Command, args []string) {
		config := constructConfig()
		iptConfigurator := NewIptablesConfigurator(config)
		iptConfigurator.run()
	},
}

func constructConfig() *config.Config {
	return &config.Config{
		ProxyPort:               viper.GetString(constants.EnvoyPort),
		InboundCapturePort:      viper.GetString(constants.InboundCapturePort),
		ProxyUID:                viper.GetString(constants.ProxyUID),
		ProxyGID:                viper.GetString(constants.ProxyGID),
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
		RestoreFormat:           viper.GetBool(constants.RestoreFormat),
	}
}

func handleError(err error) {
	log.Errora(err)
	os.Exit(1)
}

func init() {
	// Read in all environment variables
	viper.AutomaticEnv()
	// Replace - with _; so that environment variables are looked up correctly.
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	var envoyPort = "15001"
	var inboundPort = "15006"

	rootCmd.Flags().StringP(constants.EnvoyPort, "p", "", "Specify the envoy port to which redirect all TCP traffic (default $ENVOY_PORT = 15001)")
	if err := viper.BindPFlag(constants.EnvoyPort, rootCmd.Flags().Lookup(constants.EnvoyPort)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.EnvoyPort, envoyPort)

	rootCmd.Flags().StringP(constants.InboundCapturePort, "z", "",
		"Port to which all inbound TCP traffic to the pod/VM should be redirected to (default $INBOUND_CAPTURE_PORT = 15006)")
	if err := viper.BindPFlag(constants.InboundCapturePort, rootCmd.Flags().Lookup(constants.InboundCapturePort)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.InboundCapturePort, inboundPort)

	rootCmd.Flags().StringP(constants.ProxyUID, "u", "",
		"Specify the UID of the user for which the redirection is not applied. Typically, this is the UID of the proxy container")
	if err := viper.BindPFlag(constants.ProxyUID, rootCmd.Flags().Lookup(constants.ProxyUID)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.ProxyUID, "")

	rootCmd.Flags().StringP(constants.ProxyGID, "g", "",
		"Specify the GID of the user for which the redirection is not applied. (same default value as -u param)")
	if err := viper.BindPFlag(constants.ProxyGID, rootCmd.Flags().Lookup(constants.ProxyGID)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.ProxyGID, "")

	rootCmd.Flags().StringP(constants.InboundInterceptionMode, "m", "",
		"The mode used to redirect inbound connections to Envoy, either \"REDIRECT\" or \"TPROXY\"")
	if err := viper.BindPFlag(constants.InboundInterceptionMode, rootCmd.Flags().Lookup(constants.InboundInterceptionMode)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.InboundInterceptionMode, "")

	rootCmd.Flags().StringP(constants.InboundPorts, "b", "",
		"Comma separated list of inbound ports for which traffic is to be redirected to Envoy (optional). "+
			"The wildcard character \"*\" can be used to configure redirection for all ports. An empty list will disable")
	if err := viper.BindPFlag(constants.InboundPorts, rootCmd.Flags().Lookup(constants.InboundPorts)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.InboundPorts, "")

	rootCmd.Flags().StringP(constants.LocalExcludePorts, "d", "",
		"Comma separated list of inbound ports to be excluded from redirection to Envoy (optional). "+
			"Only applies  when all inbound traffic (i.e. \"*\") is being redirected (default to $ISTIO_LOCAL_EXCLUDE_PORTS)")
	if err := viper.BindPFlag(constants.LocalExcludePorts, rootCmd.Flags().Lookup(constants.LocalExcludePorts)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.LocalExcludePorts, "")

	rootCmd.Flags().StringP(constants.ServiceCidr, "i", "",
		"Comma separated list of IP ranges in CIDR form to redirect to envoy (optional). "+
			"The wildcard character \"*\" can be used to redirect all outbound traffic. An empty list will disable all outbound")
	if err := viper.BindPFlag(constants.ServiceCidr, rootCmd.Flags().Lookup(constants.ServiceCidr)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.ServiceCidr, "")

	rootCmd.Flags().StringP(constants.ServiceExcludeCidr, "x", "",
		"Comma separated list of IP ranges in CIDR form to be excluded from redirection. "+
			"Only applies when all  outbound traffic (i.e. \"*\") is being redirected (default to $ISTIO_SERVICE_EXCLUDE_CIDR)")
	if err := viper.BindPFlag(constants.ServiceExcludeCidr, rootCmd.Flags().Lookup(constants.ServiceExcludeCidr)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.ServiceExcludeCidr, "")

	rootCmd.Flags().StringP(constants.LocalOutboundPortsExclude, "o", "",
		"Comma separated list of outbound ports to be excluded from redirection to Envoy")
	if err := viper.BindPFlag(constants.LocalOutboundPortsExclude, rootCmd.Flags().Lookup(constants.LocalOutboundPortsExclude)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.LocalOutboundPortsExclude, "")

	rootCmd.Flags().StringP(constants.KubeVirtInterfaces, "k", "",
		"Comma separated list of virtual interfaces whose inbound traffic (from VM) will be treated as outbound")
	if err := viper.BindPFlag(constants.KubeVirtInterfaces, rootCmd.Flags().Lookup(constants.KubeVirtInterfaces)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.KubeVirtInterfaces, "")

	rootCmd.Flags().StringP(constants.InboundTProxyMark, "t", "", "")
	if err := viper.BindPFlag(constants.InboundTProxyMark, rootCmd.Flags().Lookup(constants.InboundTProxyMark)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.InboundTProxyMark, "1337")

	rootCmd.Flags().StringP(constants.InboundTProxyRouteTable, "r", "", "")
	if err := viper.BindPFlag(constants.InboundTProxyRouteTable, rootCmd.Flags().Lookup(constants.InboundTProxyRouteTable)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.InboundTProxyRouteTable, "133")

	rootCmd.Flags().BoolP(constants.DryRun, "n", true, "Do not call any external dependencies like iptables")
	if err := viper.BindPFlag(constants.DryRun, rootCmd.Flags().Lookup(constants.DryRun)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.DryRun, false)

	rootCmd.Flags().BoolP(constants.RestoreFormat, "f", false, "Print iptables rules in iptables-restore interpretable format")
	if err := viper.BindPFlag(constants.RestoreFormat, rootCmd.Flags().Lookup(constants.RestoreFormat)); err != nil {
		handleError(err)
	}
	viper.SetDefault(constants.RestoreFormat, false)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		log.Errora(err)
		os.Exit(1)
	}
}
