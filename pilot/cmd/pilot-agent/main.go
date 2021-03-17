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

package main

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/gogo/protobuf/types"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/cmd/pilot-agent/config"
	"istio.io/istio/pilot/cmd/pilot-agent/options"
	"istio.io/istio/pilot/cmd/pilot-agent/status"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/network"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/envoy"
	istio_agent "istio.io/istio/pkg/istio-agent"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/util/gogoprotomarshal"
	stsserver "istio.io/istio/security/pkg/stsservice/server"
	"istio.io/istio/security/pkg/stsservice/tokenmanager"
	cleaniptables "istio.io/istio/tools/istio-clean-iptables/pkg/cmd"
	iptables "istio.io/istio/tools/istio-iptables/pkg/cmd"
	"istio.io/pkg/collateral"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

const (
	localHostIPv4 = "127.0.0.1"
	localHostIPv6 = "[::1]"
)

// TODO: Move most of this to pkg options.

var (
	dnsDomain          string
	stsPort            int
	tokenManagerPlugin string

	meshConfigFile string

	// proxy config flags (named identically)
	serviceCluster         string
	proxyLogLevel          string
	proxyComponentLogLevel string
	concurrency            int
	templateFile           string
	loggingOptions         = log.DefaultOptions()
	outlierLogPath         string

	rootCmd = &cobra.Command{
		Use:          "pilot-agent",
		Short:        "Istio Pilot agent.",
		Long:         "Istio Pilot agent runs in the sidecar or gateway container and bootstraps Envoy.",
		SilenceUsage: true,
		FParseErrWhitelist: cobra.FParseErrWhitelist{
			// Allow unknown flags for backward-compatibility.
			UnknownFlags: true,
		},
	}

	proxyCmd = &cobra.Command{
		Use:   "proxy",
		Short: "Envoy proxy agent",
		FParseErrWhitelist: cobra.FParseErrWhitelist{
			// Allow unknown flags for backward-compatibility.
			UnknownFlags: true,
		},
		PersistentPreRunE: configureLogging,
		RunE: func(c *cobra.Command, args []string) error {
			cmd.PrintFlags(c.Flags())
			log.Infof("Version %s", version.Info.String())

			proxy, err := initProxy(args)
			if err != nil {
				return err
			}
			proxyConfig, err := config.ConstructProxyConfig(meshConfigFile, serviceCluster, options.ProxyConfigEnv, concurrency, proxy)
			if err != nil {
				return fmt.Errorf("failed to get proxy config: %v", err)
			}
			if out, err := gogoprotomarshal.ToYAML(proxyConfig); err != nil {
				log.Infof("Failed to serialize to YAML: %v", err)
			} else {
				log.Infof("Effective config: %s", out)
			}

			secOpts, err := options.NewSecurityOptions(proxyConfig, stsPort, tokenManagerPlugin)
			if err != nil {
				return err
			}

			// If security token service (STS) port is not zero, start STS server and
			// listen on STS port for STS requests. For STS, see
			// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16.
			// STS is used for stackdriver or other Envoy services using google gRPC.
			if stsPort > 0 {
				stsServer, err := initStsServer(proxy, secOpts.TokenManager)
				if err != nil {
					return err
				}
				defer stsServer.Stop()
			}

			agentOptions := options.NewAgentOptions(proxy)
			var pilotSAN []string
			if proxyConfig.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS {
				// Obtain Pilot SAN, using DNS.
				pilotSAN = []string{config.GetPilotSan(proxyConfig.DiscoveryAddress)}
			}
			log.Infof("Pilot SAN: %v", pilotSAN)

			agent := istio_agent.NewAgent(proxyConfig, agentOptions, secOpts)
			// Start in process SDS.
			if err := agent.Start(); err != nil {
				log.Fatala("Failed to start in-process SDS", err)
			}

			// If we are using a custom template file (for control plane proxy, for example), configure this.
			if templateFile != "" && proxyConfig.CustomConfigFile == "" {
				proxyConfig.ProxyBootstrapTemplatePath = templateFile
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// If a status port was provided, start handling status probes.
			if proxyConfig.StatusPort > 0 {
				if err := initStatusServer(ctx, proxy, proxyConfig); err != nil {
					return err
				}
			}

			provCert := agent.FindRootCAForXDS()
			if provCert == "" {
				// Envoy only supports load from file. If we want to use system certs, use best guess
				// To be more correct this could lookup all the "well known" paths but this is extremely \
				// unlikely to run on a non-debian based machine, and if it is it can be explicitly configured
				provCert = "/etc/ssl/certs/ca-certificates.crt"
			}
			envoyProxy := envoy.NewProxy(envoy.ProxyConfig{
				Config:              proxyConfig,
				Node:                proxy.ServiceNode(),
				LogLevel:            proxyLogLevel,
				ComponentLogLevel:   proxyComponentLogLevel,
				LogAsJSON:           loggingOptions.JSONEncoding,
				PilotSubjectAltName: pilotSAN,
				NodeIPs:             proxy.IPAddresses,
				STSPort:             stsPort,
				OutlierLogPath:      outlierLogPath,
				PilotCertProvider:   secOpts.PilotCertProvider,
				ProvCert:            provCert,
				Sidecar:             proxy.Type == model.SidecarProxy,
				ProxyViaAgent:       agentOptions.ProxyXDSViaAgent,
			})

			drainDuration, _ := types.DurationFromProto(proxyConfig.TerminationDrainDuration)
			envoyAgent := envoy.NewAgent(envoyProxy, drainDuration)
			// On SIGINT or SIGTERM, cancel the context, triggering a graceful shutdown
			go cmd.WaitSignalFunc(cancel)

			return envoyAgent.Run(ctx)
		},
	}
)

func init() {
	proxyCmd.PersistentFlags().StringVar(&dnsDomain, "domain", "",
		"DNS domain suffix. If not provided uses ${POD_NAMESPACE}.svc.cluster.local")
	proxyCmd.PersistentFlags().StringVar(&meshConfigFile, "meshConfig", "./etc/istio/config/mesh",
		"File name for Istio mesh configuration. If not specified, a default mesh will be used. This may be overridden by "+
			"PROXY_CONFIG environment variable or proxy.istio.io/config annotation.")
	proxyCmd.PersistentFlags().IntVar(&stsPort, "stsPort", 0,
		"HTTP Port on which to serve Security Token Service (STS). If zero, STS service will not be provided.")
	proxyCmd.PersistentFlags().StringVar(&tokenManagerPlugin, "tokenManagerPlugin", tokenmanager.GoogleTokenExchange,
		"Token provider specific plugin name.")
	// Flags for proxy configuration
	proxyCmd.PersistentFlags().StringVar(&serviceCluster, "serviceCluster", constants.ServiceClusterName, "Service cluster")
	// Log levels are provided by the library https://github.com/gabime/spdlog, used by Envoy.
	proxyCmd.PersistentFlags().StringVar(&proxyLogLevel, "proxyLogLevel", "warning",
		fmt.Sprintf("The log level used to start the Envoy proxy (choose from {%s, %s, %s, %s, %s, %s, %s})",
			"trace", "debug", "info", "warning", "error", "critical", "off"))
	proxyCmd.PersistentFlags().IntVar(&concurrency, "concurrency", 0, "number of worker threads to run")
	// See https://www.envoyproxy.io/docs/envoy/latest/operations/cli#cmdoption-component-log-level
	proxyCmd.PersistentFlags().StringVar(&proxyComponentLogLevel, "proxyComponentLogLevel", "misc:error",
		"The component log level used to start the Envoy proxy")
	proxyCmd.PersistentFlags().StringVar(&templateFile, "templateFile", "",
		"Go template bootstrap config")
	proxyCmd.PersistentFlags().StringVar(&outlierLogPath, "outlierLogPath", "",
		"The log path for outlier detection")

	// Attach the Istio logging options to the command.
	loggingOptions.AttachCobraFlags(rootCmd)

	cmd.AddFlags(rootCmd)

	rootCmd.AddCommand(proxyCmd)
	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(iptables.GetCommand())
	rootCmd.AddCommand(cleaniptables.GetCommand())

	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio Pilot Agent",
		Section: "pilot-agent CLI",
		Manual:  "Istio Pilot Agent",
	}))
}

func initStatusServer(ctx context.Context, proxy *model.Proxy, proxyConfig *meshconfig.ProxyConfig) error {
	o := options.NewStatusServerOptions(proxy, proxyConfig)
	statusServer, err := status.NewServer(*o)
	if err != nil {
		return err
	}
	go statusServer.Run(ctx)
	return nil
}

func initStsServer(proxy *model.Proxy, tokenManager security.TokenManager) (*stsserver.Server, error) {
	localHostAddr := localHostIPv4
	if options.IsIPv6Proxy(proxy.IPAddresses) {
		localHostAddr = localHostIPv6
	}
	stsServer, err := stsserver.NewServer(stsserver.Config{
		LocalHostAddr: localHostAddr,
		LocalPort:     stsPort,
	}, tokenManager)
	if err != nil {
		return nil, err
	}
	return stsServer, nil
}

func getDNSDomain(podNamespace, domain string) string {
	if len(domain) == 0 {
		domain = podNamespace + ".svc." + constants.DefaultKubernetesDomain
	}
	return domain
}

func configureLogging(_ *cobra.Command, _ []string) error {
	if err := log.Configure(loggingOptions); err != nil {
		return err
	}
	return nil
}

func initProxy(args []string) (*model.Proxy, error) {
	proxy := &model.Proxy{
		Type: model.SidecarProxy,
	}
	if len(args) > 0 {
		proxy.Type = model.NodeType(args[0])
		if !model.IsApplicationNodeType(proxy.Type) {
			log.Errorf("Invalid proxy Type: %#v", proxy.Type)
			return nil, fmt.Errorf("Invalid proxy Type: " + string(proxy.Type))
		}
	}

	podIP := net.ParseIP(options.InstanceIPVar.Get()) // protobuf encoding of IP_ADDRESS type
	if podIP != nil {
		proxy.IPAddresses = []string{podIP.String()}
	}

	// Obtain all the IPs from the node
	if ipAddrs, ok := network.GetPrivateIPs(context.Background()); ok {
		if len(proxy.IPAddresses) == 1 {
			for _, ip := range ipAddrs {
				// prevent duplicate ips, the first one must be the pod ip
				// as we pick the first ip as pod ip in istiod
				if proxy.IPAddresses[0] != ip {
					proxy.IPAddresses = append(proxy.IPAddresses, ip)
				}
			}
		} else {
			proxy.IPAddresses = append(proxy.IPAddresses, ipAddrs...)
		}
	}

	// No IP addresses provided, append 127.0.0.1 for ipv4 and ::1 for ipv6
	if len(proxy.IPAddresses) == 0 {
		proxy.IPAddresses = append(proxy.IPAddresses, localHostIPv4, localHostIPv6)
	}

	// Extract pod variables.
	podName := options.PodNameVar.Get()
	podNamespace := options.PodNamespaceVar.Get()
	proxy.ID = podName + "." + podNamespace

	// If not set, set a default based on platform - podNamespace.svc.cluster.local for
	// K8S
	proxy.DNSDomain = getDNSDomain(podNamespace, dnsDomain)
	log.WithLabels("ips", proxy.IPAddresses, "type", proxy.Type, "id", proxy.ID, "domain", proxy.DNSDomain).Info("Proxy role")

	return proxy, nil
}

// TODO: get the config and bootstrap from istiod, by passing the env

// Use env variables - from injection, k8s and local namespace config map.
// No CLI parameters.
func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Error(err)
		os.Exit(-1)
	}
}
