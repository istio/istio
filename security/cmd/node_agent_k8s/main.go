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

	_ "k8s.io/client-go/plugin/pkg/client/auth"
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

	// The workload SDS mode allows node agent to provision credentials to workload proxy by sending
	// CSR to CA.
	enableWorkloadSDS = "ENABLE_WORKLOAD_SDS"

	// The ingress gateway SDS mode allows node agent to provision credentials to ingress gateway
	// proxy by watching kubernetes secrets.
	enableIngressGatewaySDS = "ENABLE_INGRESS_GATEWAY_SDS"

	// The environmental variable name for Vault CA address.
	vaultAddress = "VAULT_ADDR"

	// The environmental variable name for Vault auth path.
	vaultAuthPath = "VAULT_AUTH_PATH"

	// The environmental variable name for Vault role.
	vaultRole = "VAULT_ROLE"

	// The environmental variable name for Vault sign CSR path.
	vaultSignCsrPath = "VAULT_SIGN_CSR_PATH"

	// The environmental variable name for Vault TLS root certificate.
	vaultTLSRootCert = "VAULT_TLS_ROOT_CERT"

	// The environmental variable name for the flag which is used to indicate the token passed
	// from envoy is always valid(ex, normal 8ks JWT).
	alwaysValidTokenFlag = "VALID_TOKEN"

	// The environmental variable name for secret TTL, node agent decides whether a secret
	// is expired if time.now - secret.createtime >= secretTTL.
	// example value format like "90m"
	secretTTL = "SECRET_TTL"

	// The environmental variable name for grace duration that secret is re-generated
	// before it's expired time.
	// example value format like "10m"
	SecretRefreshGraceDuration = "SECRET_GRACE_DURATION"

	// The environmental variable name for key rotation job running interval.
	// example value format like "20m"
	SecretRotationJobRunInterval = "SECRET_JOB_RUN_INTERVAL"
)

var (
	workloadSdsCacheOptions cache.Options
	gatewaySdsCacheOptions  cache.Options
	serverOptions           sds.Options
	gatewaySecretChan       chan struct{}
	loggingOptions          = log.DefaultOptions()

	// rootCmd defines the command for node agent.
	rootCmd = &cobra.Command{
		Use:   "nodeagent",
		Short: "Node agent",
		RunE: func(c *cobra.Command, args []string) error {
			if err := log.Configure(loggingOptions); err != nil {
				return err
			}
			gatewaySdsCacheOptions = workloadSdsCacheOptions

			if serverOptions.EnableIngressGatewaySDS && serverOptions.EnableWorkloadSDS &&
				serverOptions.IngressGatewayUDSPath == serverOptions.WorkloadUDSPath {
				log.Error("UDS paths for ingress gateway and workload are the same")
				os.Exit(1)
			}
			if serverOptions.CAProviderName == "" && serverOptions.EnableWorkloadSDS {
				log.Error("CA Provider is missing")
				os.Exit(1)
			}
			if serverOptions.CAEndpoint == "" && serverOptions.EnableWorkloadSDS {
				log.Error("CA Endpoint is missing")
				os.Exit(1)
			}

			stop := make(chan struct{})

			workloadSecretCache, gatewaySecretCache := newSecretCache(serverOptions)
			if workloadSecretCache != nil {
				defer workloadSecretCache.Close()
			}
			if gatewaySecretCache != nil {
				defer gatewaySecretCache.Close()
			}

			server, err := sds.NewServer(serverOptions, workloadSecretCache, gatewaySecretCache)
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

func newSecretCache(serverOptions sds.Options) (workloadSecretCache, gatewaySecretCache *cache.SecretCache) {
	if serverOptions.EnableWorkloadSDS {
		wSecretFetcher, err := secretfetcher.NewSecretFetcher(false, serverOptions.CAEndpoint,
			serverOptions.CAProviderName, true, []byte(serverOptions.VaultTLSRootCert),
			serverOptions.VaultAddress, serverOptions.VaultRole, serverOptions.VaultAuthPath,
			serverOptions.VaultSignCsrPath)
		if err != nil {
			log.Errorf("failed to create secretFetcher for workload proxy: %v", err)
			os.Exit(1)
		}
		workloadSdsCacheOptions.TrustDomain = serverOptions.TrustDomain
		workloadSdsCacheOptions.Plugins = sds.NewPlugins(serverOptions.PluginNames)
		workloadSecretCache = cache.NewSecretCache(wSecretFetcher, sds.NotifyProxy, workloadSdsCacheOptions)
	} else {
		workloadSecretCache = nil
	}

	if serverOptions.EnableIngressGatewaySDS {
		gSecretFetcher, err := secretfetcher.NewSecretFetcher(true, "", "", false, nil, "", "", "", "")
		if err != nil {
			log.Errorf("failed to create secretFetcher for gateway proxy: %v", err)
			os.Exit(1)
		}
		gatewaySecretChan = make(chan struct{})
		gSecretFetcher.Run(gatewaySecretChan)
		gatewaySecretCache = cache.NewSecretCache(gSecretFetcher, sds.NotifyProxy, gatewaySdsCacheOptions)
	} else {
		gatewaySecretCache = nil
	}
	return workloadSecretCache, gatewaySecretCache
}

func init() {
	pluginNames := os.Getenv(pluginNames)
	pns := []string{}
	if pluginNames != "" {
		pns = strings.Split(pluginNames, ",")
	}

	enableWorkloadSdsEnv := true
	val := os.Getenv(enableWorkloadSDS)
	if env, err := strconv.ParseBool(val); err == nil {
		enableWorkloadSdsEnv = env
	}
	enableIngressGatewaySdsEnv := false
	val = os.Getenv(enableIngressGatewaySDS)
	if env, err := strconv.ParseBool(val); err == nil {
		enableIngressGatewaySdsEnv = env
	}

	alwaysValidTokenFlagEnv := false
	val = os.Getenv(alwaysValidTokenFlag)
	if env, err := strconv.ParseBool(val); err == nil {
		alwaysValidTokenFlagEnv = env
	}

	rootCmd.PersistentFlags().BoolVar(&serverOptions.EnableWorkloadSDS, "enableWorkloadSDS",
		enableWorkloadSdsEnv,
		"If true, node agent works as SDS server and provisions key/certificate to workload proxies.")
	rootCmd.PersistentFlags().StringVar(&serverOptions.WorkloadUDSPath, "workloadUDSPath",
		"/var/run/sds/uds_path", "Unix domain socket through which SDS server communicates with workload proxies")

	rootCmd.PersistentFlags().BoolVar(&serverOptions.EnableIngressGatewaySDS, "enableIngressGatewaySDS",
		enableIngressGatewaySdsEnv,
		"If true, node agent works as SDS server and watches kubernetes secrets for ingress gateway.")
	rootCmd.PersistentFlags().StringVar(&serverOptions.IngressGatewayUDSPath, "gatewayUdsPath",
		"/var/run/ingress_gateway/sds", "Unix domain socket through which SDS server communicates with ingress gateway proxies.")

	rootCmd.PersistentFlags().StringVar(&serverOptions.CAProviderName, "caProvider", os.Getenv(caProvider), "CA provider")
	rootCmd.PersistentFlags().StringVar(&serverOptions.CAEndpoint, "caEndpoint", os.Getenv(caAddress), "CA endpoint")

	rootCmd.PersistentFlags().StringVar(&serverOptions.TrustDomain, "trustDomain",
		os.Getenv(trustDomain), "The trust domain this node agent run in")
	rootCmd.PersistentFlags().StringArrayVar(&serverOptions.PluginNames, "pluginNames",
		pns, "authentication provider specific plugin names")

	rootCmd.PersistentFlags().StringVar(&serverOptions.CertFile, "sdsCertFile", "", "SDS gRPC TLS server-side certificate")
	rootCmd.PersistentFlags().StringVar(&serverOptions.KeyFile, "sdsKeyFile", "", "SDS gRPC TLS server-side key")

	setWorkloadCacheTimeParams()

	rootCmd.PersistentFlags().BoolVar(&workloadSdsCacheOptions.AlwaysValidTokenFlag, "alwaysValidTokenFlag",
		alwaysValidTokenFlagEnv,
		"If true, node agent assume token passed from envoy is always valid.")

	rootCmd.PersistentFlags().StringVar(&serverOptions.VaultAddress, "vaultAddress", os.Getenv(vaultAddress),
		"Vault address")
	rootCmd.PersistentFlags().StringVar(&serverOptions.VaultRole, "vaultRole", os.Getenv(vaultRole),
		"Vault role")
	rootCmd.PersistentFlags().StringVar(&serverOptions.VaultAuthPath, "vaultAuthPath", os.Getenv(vaultAuthPath),
		"Vault auth path")
	rootCmd.PersistentFlags().StringVar(&serverOptions.VaultSignCsrPath, "vaultSignCsrPath", os.Getenv(vaultSignCsrPath),
		"Vault sign CSR path")
	rootCmd.PersistentFlags().StringVar(&serverOptions.VaultTLSRootCert, "vaultTLSRootCert", os.Getenv(vaultTLSRootCert),
		"Vault TLS root certificate")

	// Attach the Istio logging options to the command.
	loggingOptions.AttachCobraFlags(rootCmd)
}

func setWorkloadCacheTimeParams() {
	secretTTLDuration := 24 * time.Hour
	if env, err := time.ParseDuration(os.Getenv(secretTTL)); err == nil {
		secretTTLDuration = env
	}

	secretRefreshGraceDuration := time.Hour
	if env, err := time.ParseDuration(os.Getenv(SecretRefreshGraceDuration)); err == nil {
		secretRefreshGraceDuration = env
	}

	secretRotationJobRunInterval := 10 * time.Minute
	if env, err := time.ParseDuration(os.Getenv(SecretRotationJobRunInterval)); err == nil {
		secretRotationJobRunInterval = env
	}

	// The initial backoff time (in millisec) is a random number between 0 and initBackoff.
	// Default to 10, a valid range is [10, 120000].
	var initBackoff int64 = 10
	env := os.Getenv("INITIAL_BACKOFF_MSEC")
	if len(env) > 0 {
		initialBackoff, err := strconv.ParseInt(env, 0, 32)
		if err != nil {
			log.Errorf("Failed to parse INITIAL_BACKOFF to integer with error: %v", err)
			os.Exit(1)
		} else if initialBackoff < 10 || initialBackoff > 120000 {
			log.Errorf("INITIAL_BACKOFF should be within range 10 to 120000")
			os.Exit(1)
		} else {
			initBackoff = initialBackoff
		}
	}

	rootCmd.PersistentFlags().DurationVar(&workloadSdsCacheOptions.SecretTTL, "secretTtl",
		secretTTLDuration, "Secret's TTL")
	rootCmd.PersistentFlags().DurationVar(&workloadSdsCacheOptions.SecretRefreshGraceDuration, "secretRefreshGraceDuration",
		secretRefreshGraceDuration, "Secret's Refresh Grace Duration")
	rootCmd.PersistentFlags().DurationVar(&workloadSdsCacheOptions.RotationInterval, "secretRotationInterval",
		secretRotationJobRunInterval, "Secret rotation job running interval")

	rootCmd.PersistentFlags().Int64Var(&workloadSdsCacheOptions.InitialBackoff, "initialBackoff",
		initBackoff, "The initial backoff interval in milliseconds")

	rootCmd.PersistentFlags().DurationVar(&workloadSdsCacheOptions.EvictionDuration, "secretEvictionDuration",
		24*time.Hour, "Secret eviction time duration")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Errora(err)
		os.Exit(1)
	}
}
