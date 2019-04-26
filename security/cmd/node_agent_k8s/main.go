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
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/collateral"
	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/version"
	"istio.io/istio/security/pkg/nodeagent/cache"
	"istio.io/istio/security/pkg/nodeagent/sds"
	"istio.io/istio/security/pkg/nodeagent/secretfetcher"

	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

const (
	// name of authentication provider.
	caProvider     = "CA_PROVIDER"
	caProviderFlag = "caProvider"

	// CA endpoint.
	caEndpoint     = "CA_ADDR"
	caEndpointFlag = "caEndpoint"

	// names of authentication provider's plugins.
	pluginNames     = "Plugins"
	pluginNamesFlag = "pluginNames"

	// The trust domain corresponds to the trust root of a system.
	// Refer to https://github.com/spiffe/spiffe/blob/master/standards/SPIFFE-ID.md#21-trust-domain
	trustDomain     = "Trust_Domain"
	trustDomainFlag = "trustDomain"

	// The workload SDS mode allows node agent to provision credentials to workload proxy by sending
	// CSR to CA.
	enableWorkloadSDS     = "ENABLE_WORKLOAD_SDS"
	enableWorkloadSDSFlag = "enableWorkloadSDS"

	// The ingress gateway SDS mode allows node agent to provision credentials to ingress gateway
	// proxy by watching kubernetes secrets.
	enableIngressGatewaySDS     = "ENABLE_INGRESS_GATEWAY_SDS"
	enableIngressGatewaySDSFlag = "enableIngressGatewaySDS"

	// The environmental variable name for Vault CA address.
	vaultAddress     = "VAULT_ADDR"
	vaultAddressFlag = "vaultAddress"

	// The environmental variable name for Vault auth path.
	vaultAuthPath     = "VAULT_AUTH_PATH"
	vaultAuthPathFlag = "vaultAuthPath"

	// The environmental variable name for Vault role.
	vaultRole     = "VAULT_ROLE"
	vaultRoleFlag = "vaultRole"

	// The environmental variable name for Vault sign CSR path.
	vaultSignCsrPath     = "VAULT_SIGN_CSR_PATH"
	vaultSignCsrPathFlag = "vaultSignCsrPath"

	// The environmental variable name for Vault TLS root certificate.
	vaultTLSRootCert     = "VAULT_TLS_ROOT_CERT"
	vaultTLSRootCertFlag = "vaultTLSRootCert"

	// The environmental variable name for the flag which is used to indicate the token passed
	// from envoy is always valid(ex, normal 8ks JWT).
	alwaysValidTokenFlag     = "VALID_TOKEN"
	alwaysValidTokenFlagFlag = "alwaysValidTokenFlag"

	// The environmental variable name for the flag which is used to indicate whether to
	// validate the certificate's format which is returned by CA.
	skipValidateCertFlag = "SKIP_CERT_VALIDATION"

	// The environmental variable name for secret TTL, node agent decides whether a secret
	// is expired if time.now - secret.createtime >= secretTTL.
	// example value format like "90m"
	secretTTL     = "SECRET_TTL"
	secretTTLFlag = "secretTtl"

	// The environmental variable name for grace duration that secret is re-generated
	// before it's expired time.
	// example value format like "10m"
	SecretRefreshGraceDuration     = "SECRET_GRACE_DURATION"
	secretRefreshGraceDurationFlag = "secretRefreshGraceDuration"

	// The environmental variable name for key rotation job running interval.
	// example value format like "20m"
	SecretRotationInterval     = "SECRET_JOB_RUN_INTERVAL"
	secretRotationIntervalFlag = "secretRotationInterval"
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

			applyEnvVars(c)

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

// newSecretCache creates the cache for workload secrets and/or gateway secrets.
// Although currently not used, Citadel Agent can serve both workload and gateway secrets at the same time.
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

var (
	pluginNamesEnv                = env.RegisterStringVar(pluginNames, "", "").Get()
	enableWorkloadSDSEnv          = env.RegisterBoolVar(enableWorkloadSDS, true, "").Get()
	enableIngressGatewaySDSEnv    = env.RegisterBoolVar(enableIngressGatewaySDS, false, "").Get()
	alwaysValidTokenFlagEnv       = env.RegisterBoolVar(alwaysValidTokenFlag, false, "").Get()
	skipValidateCertFlagEnv       = env.RegisterBoolVar(skipValidateCertFlag, false, "").Get()
	caProviderEnv                 = env.RegisterStringVar(caProvider, "", "").Get()
	caEndpointEnv                 = env.RegisterStringVar(caEndpoint, "", "").Get()
	trustDomainEnv                = env.RegisterStringVar(trustDomain, "", "").Get()
	vaultAddressEnv               = env.RegisterStringVar(vaultAddress, "", "").Get()
	vaultRoleEnv                  = env.RegisterStringVar(vaultRole, "", "").Get()
	vaultAuthPathEnv              = env.RegisterStringVar(vaultAuthPath, "", "").Get()
	vaultSignCsrPathEnv           = env.RegisterStringVar(vaultSignCsrPath, "", "").Get()
	vaultTLSRootCertEnv           = env.RegisterStringVar(vaultTLSRootCert, "", "").Get()
	secretTTLEnv                  = env.RegisterDurationVar(secretTTL, 24*time.Hour, "").Get()
	secretRefreshGraceDurationEnv = env.RegisterDurationVar(SecretRefreshGraceDuration, 1*time.Hour, "").Get()
	secretRotationIntervalEnv     = env.RegisterDurationVar(SecretRotationInterval, 10*time.Minute, "").Get()
)

func applyEnvVars(cmd *cobra.Command) {
	if !cmd.Flag(pluginNamesFlag).Changed {
		serverOptions.PluginNames = strings.Split(pluginNamesEnv, ",")
	}

	if !cmd.Flag(enableWorkloadSDSFlag).Changed {
		serverOptions.EnableWorkloadSDS = enableWorkloadSDSEnv
	}

	if !cmd.Flag(enableIngressGatewaySDSFlag).Changed {
		serverOptions.EnableIngressGatewaySDS = enableIngressGatewaySDSEnv
	}

	if !cmd.Flag(alwaysValidTokenFlagFlag).Changed {
		serverOptions.AlwaysValidTokenFlag = alwaysValidTokenFlagEnv
	}

	if !cmd.Flag(caProviderFlag).Changed {
		serverOptions.CAProviderName = caProviderEnv
	}

	if !cmd.Flag(caEndpointFlag).Changed {
		serverOptions.CAEndpoint = caEndpointEnv
	}

	if !cmd.Flag(trustDomainFlag).Changed {
		serverOptions.TrustDomain = trustDomainEnv
	}

	if !cmd.Flag(vaultAddressFlag).Changed {
		serverOptions.VaultAddress = vaultAddressEnv
	}

	if !cmd.Flag(vaultRoleFlag).Changed {
		serverOptions.VaultRole = vaultRoleEnv
	}

	if !cmd.Flag(vaultAuthPathFlag).Changed {
		serverOptions.VaultAuthPath = vaultAuthPathEnv
	}

	if !cmd.Flag(vaultSignCsrPathFlag).Changed {
		serverOptions.VaultSignCsrPath = vaultSignCsrPathEnv
	}

	if !cmd.Flag(vaultTLSRootCertFlag).Changed {
		serverOptions.VaultTLSRootCert = vaultTLSRootCertEnv
	}

	if !cmd.Flag(secretTTLFlag).Changed {
		workloadSdsCacheOptions.SecretTTL = secretTTLEnv
	}

	if !cmd.Flag(secretRefreshGraceDurationFlag).Changed {
		workloadSdsCacheOptions.SecretRefreshGraceDuration = secretRefreshGraceDurationEnv
	}

	if !cmd.Flag(secretRotationIntervalFlag).Changed {
		workloadSdsCacheOptions.RotationInterval = secretRotationIntervalEnv
	}

	if !cmd.Flag(skipValidateCertFlag).Changed {
		workloadSdsCacheOptions.SkipValidateCert = skipValidateCertFlagEnv
	}
}

var defaultInitialBackoff = 10
var initialBackoffEnvVar = env.RegisterIntVar("INITIAL_BACKOFF_MSEC", defaultInitialBackoff, "")

func main() {
	rootCmd.PersistentFlags().BoolVar(&serverOptions.EnableWorkloadSDS, enableWorkloadSDSFlag,
		true,
		"If true, node agent works as SDS server and provisions key/certificate to workload proxies.")
	rootCmd.PersistentFlags().StringVar(&serverOptions.WorkloadUDSPath, "workloadUDSPath",
		"/var/run/sds/uds_path", "Unix domain socket through which SDS server communicates with workload proxies")

	rootCmd.PersistentFlags().BoolVar(&serverOptions.EnableIngressGatewaySDS, enableIngressGatewaySDSFlag,
		false,
		"If true, node agent works as SDS server and watches kubernetes secrets for ingress gateway.")
	rootCmd.PersistentFlags().StringVar(&serverOptions.IngressGatewayUDSPath, "gatewayUdsPath",
		"/var/run/ingress_gateway/sds", "Unix domain socket through which SDS server communicates with ingress gateway proxies.")

	rootCmd.PersistentFlags().StringVar(&serverOptions.CAProviderName, caProviderFlag, "", "CA provider")
	rootCmd.PersistentFlags().StringVar(&serverOptions.CAEndpoint, caEndpointFlag, "", "CA endpoint")

	rootCmd.PersistentFlags().StringVar(&serverOptions.TrustDomain, trustDomainFlag,
		"", "The trust domain this node agent run in")
	rootCmd.PersistentFlags().StringArrayVar(&serverOptions.PluginNames, pluginNamesFlag,
		[]string{}, "authentication provider specific plugin names")

	rootCmd.PersistentFlags().StringVar(&serverOptions.CertFile, "sdsCertFile", "", "SDS gRPC TLS server-side certificate")
	rootCmd.PersistentFlags().StringVar(&serverOptions.KeyFile, "sdsKeyFile", "", "SDS gRPC TLS server-side key")

	rootCmd.PersistentFlags().DurationVar(&workloadSdsCacheOptions.SecretTTL, secretTTLFlag,
		24*time.Hour, "Secret's TTL")
	rootCmd.PersistentFlags().DurationVar(&workloadSdsCacheOptions.SecretRefreshGraceDuration, secretRefreshGraceDurationFlag,
		time.Hour, "Secret's Refresh Grace Duration")
	rootCmd.PersistentFlags().DurationVar(&workloadSdsCacheOptions.RotationInterval, secretRotationIntervalFlag,
		10*time.Minute, "Secret rotation job running interval")

	// The initial backoff time (in millisec) is a random number between 0 and initBackoff.
	// Default to 10, a valid range is [10, 120000].
	initBackoff := int64(initialBackoffEnvVar.Get())
	if initBackoff < 10 || initBackoff > 120000 {
		log.Errorf("INITIAL_BACKOFF_MSEC should be within range 10 to 120000")
		os.Exit(1)
	}
	rootCmd.PersistentFlags().Int64Var(&workloadSdsCacheOptions.InitialBackoff, "initialBackoff",
		initBackoff, "The initial backoff interval in milliseconds")

	rootCmd.PersistentFlags().DurationVar(&workloadSdsCacheOptions.EvictionDuration, "secretEvictionDuration",
		24*time.Hour, "Secret eviction time duration")

	rootCmd.PersistentFlags().BoolVar(&workloadSdsCacheOptions.AlwaysValidTokenFlag, alwaysValidTokenFlagFlag,
		false,
		"If true, node agent assume token passed from envoy is always valid.")

	rootCmd.PersistentFlags().BoolVar(&workloadSdsCacheOptions.SkipValidateCert, skipValidateCertFlag,
		false,
		"If true, node agent skip validating format of certificate returned from CA.")

	rootCmd.PersistentFlags().StringVar(&serverOptions.VaultAddress, vaultAddressFlag, "",
		"Vault address")
	rootCmd.PersistentFlags().StringVar(&serverOptions.VaultRole, vaultRoleFlag, "",
		"Vault role")
	rootCmd.PersistentFlags().StringVar(&serverOptions.VaultAuthPath, vaultAuthPathFlag, "",
		"Vault auth path")
	rootCmd.PersistentFlags().StringVar(&serverOptions.VaultSignCsrPath, vaultSignCsrPathFlag, "",
		"Vault sign CSR path")
	rootCmd.PersistentFlags().StringVar(&serverOptions.VaultTLSRootCert, vaultTLSRootCertFlag, "",
		"Vault TLS root certificate")

	// Attach the Istio logging options to the command.
	loggingOptions.AttachCobraFlags(rootCmd)

	rootCmd.AddCommand(version.CobraCommand())
	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio Node Agent K8s",
		Section: "node_agent_k8s CLI",
		Manual:  "Istio Node K8s Agent",
	}))

	// TODO: need integration with ctrlz?

	if err := rootCmd.Execute(); err != nil {
		log.Errora(err)
		os.Exit(1)
	}
}
