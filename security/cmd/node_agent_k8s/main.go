// Copyright 2018 Istio Authors
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
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/nodeagent/cache"
	"istio.io/istio/security/pkg/nodeagent/sds"
	"istio.io/istio/security/pkg/nodeagent/secretfetcher"
)

const (
	// name of authentication provider.
	caProvider = "CA_PROVIDER"

	// CA endpoint.
	caAddress = "CA_ADDR"

	// names of authentication provider's plugins.
	pluginNames = "Plugins"

	// The trust domain corresponds to the trust root of a system.
	// Refer to https://github.com/spiffe/spiffe/blob/master/standards/SPIFFE-ID.md#21-trust-domain
	trustDomain = "Trust_Domain"

	// Node agent could act as ingress gateway agent.
	ingressGatewayAgent = "Ingress_GateWay_Agent"
)

var (
	cacheOptions  cache.Options
	serverOptions sds.Options

	loggingOptions = log.DefaultOptions()

	// rootCmd defines the command for node agent.
	rootCmd = &cobra.Command{
		Use:   "nodeagent",
		Short: "Node agent",
		RunE: func(c *cobra.Command, args []string) error {
			if err := log.Configure(loggingOptions); err != nil {
				return err
			}

			stop := make(chan struct{})

			secretFetcher, err := secretfetcher.NewSecretFetcher(serverOptions.IngressGatewayAgent, serverOptions.CAEndpoint, serverOptions.CAProviderName, true)
			if err != nil {
				log.Errorf("failed to create secretFetcher: %v", err)
				return fmt.Errorf("failed to create secretFetcher")
			}
			cacheOptions.TrustDomain = serverOptions.TrustDomain
			cacheOptions.Plugins = sds.NewPlugins(serverOptions.PluginNames)
			sc := cache.NewSecretCache(secretFetcher, sds.NotifyProxy, cacheOptions)
			defer sc.Close()

			server, err := sds.NewServer(serverOptions, sc)
			defer server.Stop()
			if err != nil {
				log.Errorf("failed to create sds service: %v", err)
				return fmt.Errorf("failed to create sds service")
			}

			cmd.WaitSignal(stop)

			return nil
		},
	}
)

func init() {
	ingressGatewayAgentEnv := os.Getenv(ingressGatewayAgent)
	enableIngressGatewayAgentMode := false
	if ingressGatewayAgentEnv != "" {
		v, err := strconv.ParseBool(ingressGatewayAgentEnv)
		if err == nil {
			enableIngressGatewayAgentMode = v
		}
	}

	sdsUdsPath := "/var/run/sds/uds_path"
	if enableIngressGatewayAgentMode {
		sdsUdsPath = "/var/run/ingress_gateway_agent/uds_path"
	}

	caProvider := os.Getenv(caProvider)
	if caProvider == "" && !enableIngressGatewayAgentMode {
		log.Error("CA Provider is missing")
		os.Exit(1)
	}

	caAddr := os.Getenv(caAddress)
	if caAddr == "" && !enableIngressGatewayAgentMode {
		log.Error("CA Endpoint is missing")
		os.Exit(1)
	}

	pluginNames := os.Getenv(pluginNames)
	pns := []string{}
	if pluginNames != "" {
		pns = strings.Split(pluginNames, ",")
	}

	rootCmd.PersistentFlags().StringVar(&serverOptions.UDSPath, "sdsUdsPath",
		sdsUdsPath, "Unix domain socket through which SDS server communicates with proxies")

	rootCmd.PersistentFlags().BoolVar(&serverOptions.IngressGatewayAgent, "IngressGatewayAgent",
		enableIngressGatewayAgentMode,
		"If true, node agent works as ingress gateway agent and watches kubernetes secrets instead of sending CSR to CA.")

	rootCmd.PersistentFlags().StringVar(&serverOptions.CAProviderName, "caProvider", caProvider, "CA provider")
	rootCmd.PersistentFlags().StringVar(&serverOptions.CAEndpoint, "caEndpoint", caAddr, "CA endpoint")

	rootCmd.PersistentFlags().StringVar(&serverOptions.TrustDomain, "trustDomain",
		os.Getenv(trustDomain), "The trust domain this node agent run in")
	rootCmd.PersistentFlags().StringArrayVar(&serverOptions.PluginNames, "pluginNames",
		pns, "authentication provider specific plugin names")

	rootCmd.PersistentFlags().StringVar(&serverOptions.CertFile, "sdsCertFile", "", "SDS gRPC TLS server-side certificate")
	rootCmd.PersistentFlags().StringVar(&serverOptions.KeyFile, "sdsKeyFile", "", "SDS gRPC TLS server-side key")

	rootCmd.PersistentFlags().DurationVar(&cacheOptions.SecretTTL, "secretTtl",
		24*time.Hour, "Secret's TTL")
	rootCmd.PersistentFlags().DurationVar(&cacheOptions.SecretRefreshGraceDuration, "secretRefreshGraceDuration",
		time.Hour, "Secret's Refresh Grace Duration")
	rootCmd.PersistentFlags().DurationVar(&cacheOptions.RotationInterval, "secretRotationInterval",
		10*time.Minute, "Secret rotation job running interval")
	rootCmd.PersistentFlags().DurationVar(&cacheOptions.EvictionDuration, "secretEvictionDuration",
		24*time.Hour, "Secret eviction time duration")

	// Attach the Istio logging options to the command.
	loggingOptions.AttachCobraFlags(rootCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Errora(err)
		os.Exit(1)
	}
}
