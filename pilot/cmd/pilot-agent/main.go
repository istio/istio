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
	"strings"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/cmd/pilot-agent/config"
	secopt "istio.io/istio/pilot/cmd/pilot-agent/security"
	"istio.io/istio/pilot/cmd/pilot-agent/status"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/network"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/envoy"
	istio_agent "istio.io/istio/pkg/istio-agent"
	"istio.io/istio/pkg/jwt"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/util/gogoprotomarshal"
	stsserver "istio.io/istio/security/pkg/stsservice/server"
	"istio.io/istio/security/pkg/stsservice/tokenmanager"
	cleaniptables "istio.io/istio/tools/istio-clean-iptables/pkg/cmd"
	iptables "istio.io/istio/tools/istio-iptables/pkg/cmd"
	"istio.io/pkg/collateral"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

const (
	localHostIPv4 = "127.0.0.1"
	localHostIPv6 = "[::1]"

	// Similar with ISTIO_META_, which is used to customize the node metadata - this customizes extra header.
	xdsHeaderPrefix = "XDS_HEADER_"
)

// TODO: Move most of this to pkg.

var (
	role               = &model.Proxy{}
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

	instanceIPVar        = env.RegisterStringVar("INSTANCE_IP", "", "")
	podNameVar           = env.RegisterStringVar("POD_NAME", "", "")
	podNamespaceVar      = env.RegisterStringVar("POD_NAMESPACE", "", "")
	kubeAppProberNameVar = env.RegisterStringVar(status.KubeAppProberEnvName, "", "")
	serviceAccountVar    = env.RegisterStringVar("SERVICE_ACCOUNT", "", "Name of service account")
	clusterIDVar         = env.RegisterStringVar("ISTIO_META_CLUSTER_ID", "", "")
	// Provider for XDS auth, e.g., gcp. By default, it is empty, meaning no auth provider.
	xdsAuthProvider = env.RegisterStringVar("XDS_AUTH_PROVIDER", "", "Provider for XDS auth")

	pilotCertProvider = env.RegisterStringVar("PILOT_CERT_PROVIDER", "istiod",
		"The provider of Pilot DNS certificate.").Get()
	jwtPolicy = env.RegisterStringVar("JWT_POLICY", jwt.PolicyThirdParty,
		"The JWT validation policy.")
	// ProvCert is the environment controlling the use of pre-provisioned certs, for VMs.
	// May also be used in K8S to use a Secret to bootstrap (as a 'refresh key'), but use short-lived tokens
	// with extra SAN (labels, etc) in data path.
	provCert = env.RegisterStringVar("PROV_CERT", "",
		"Set to a directory containing provisioned certs, for VMs").Get()

	// set to "SYSTEM" for ACME/public signed XDS servers.
	xdsRootCA = env.RegisterStringVar("XDS_ROOT_CA", "",
		"Explicitly set the root CA to expect for the XDS connection.").Get()

	// set to "SYSTEM" for ACME/public signed CA servers.
	caRootCA = env.RegisterStringVar("CA_ROOT_CA", "",
		"Explicitly set the root CA to expect for the CA connection.").Get()

	outputKeyCertToDir = env.RegisterStringVar("OUTPUT_CERTS", "",
		"The output directory for the key and certificate. If empty, key and certificate will not be saved. "+
			"Must be set for VMs using provisioning certificates.").Get()
	proxyConfigEnv = env.RegisterStringVar(
		"PROXY_CONFIG",
		"",
		"The proxy configuration. This will be set by the injection - gateways will use file mounts.",
	).Get()

	caProviderEnv = env.RegisterStringVar("CA_PROVIDER", "Citadel", "name of authentication provider").Get()
	caEndpointEnv = env.RegisterStringVar("CA_ADDR", "", "Address of the spiffe certificate provider. Defaults to discoveryAddress").Get()

	trustDomainEnv = env.RegisterStringVar("TRUST_DOMAIN", "cluster.local",
		"The trust domain for spiffe certificates").Get()

	secretTTLEnv = env.RegisterDurationVar("SECRET_TTL", 24*time.Hour,
		"The cert lifetime requested by istio agent").Get()
	secretRotationGracePeriodRatioEnv = env.RegisterFloatVar("SECRET_GRACE_PERIOD_RATIO", 0.5,
		"The grace period ratio for the cert rotation, by default 0.5.").Get()
	pkcs8KeysEnv = env.RegisterBoolVar("PKCS8_KEY", false,
		"Whether to generate PKCS#8 private keys").Get()
	eccSigAlgEnv        = env.RegisterStringVar("ECC_SIGNATURE_ALGORITHM", "", "The type of ECC signature algorithm to use when generating private keys").Get()
	fileMountedCertsEnv = env.RegisterBoolVar("FILE_MOUNTED_CERTS", false, "").Get()
	credFetcherTypeEnv  = env.RegisterStringVar("CREDENTIAL_FETCHER_TYPE", "",
		"The type of the credential fetcher. Currently supported types include GoogleComputeEngine").Get()
	credIdentityProvider = env.RegisterStringVar("CREDENTIAL_IDENTITY_PROVIDER", "GoogleComputeEngine",
		"The identity provider for credential. Currently default supported identity provider is GoogleComputeEngine").Get()
	proxyXDSViaAgent = env.RegisterBoolVar("PROXY_XDS_VIA_AGENT", true,
		"If set to true, envoy will proxy XDS calls via the agent instead of directly connecting to istiod. This option "+
			"will be removed once the feature is stabilized.").Get()
	// This is a copy of the env var in the init code.
	dnsCaptureByAgent = env.RegisterBoolVar("ISTIO_META_DNS_CAPTURE", false,
		"If set to true, enable the capture of outgoing DNS packets on port 53, redirecting to istio-agent on :15053").Get()

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

			// Extract pod variables.
			podName := podNameVar.Get()
			podNamespace := podNamespaceVar.Get()
			podIP := net.ParseIP(instanceIPVar.Get()) // protobuf encoding of IP_ADDRESS type

			log.Infof("Version %s", version.Info.String())
			role.Type = model.SidecarProxy
			if len(args) > 0 {
				role.Type = model.NodeType(args[0])
				if !model.IsApplicationNodeType(role.Type) {
					log.Errorf("Invalid role Type: %#v", role.Type)
					return fmt.Errorf("Invalid role Type: " + string(role.Type))
				}
			}

			if podIP != nil {
				role.IPAddresses = []string{podIP.String()}
			}

			// Obtain all the IPs from the node
			if ipAddrs, ok := network.GetPrivateIPs(context.Background()); ok {
				if len(role.IPAddresses) == 1 {
					for _, ip := range ipAddrs {
						// prevent duplicate ips, the first one must be the pod ip
						// as we pick the first ip as pod ip in istiod
						if role.IPAddresses[0] != ip {
							role.IPAddresses = append(role.IPAddresses, ip)
						}
					}
				} else {
					role.IPAddresses = append(role.IPAddresses, ipAddrs...)
				}
			}

			// No IP addresses provided, append 127.0.0.1 for ipv4 and ::1 for ipv6
			if len(role.IPAddresses) == 0 {
				role.IPAddresses = append(role.IPAddresses, "127.0.0.1")
				role.IPAddresses = append(role.IPAddresses, "::1")
			}
			role.ID = podName + "." + podNamespace

			// Check if proxy runs in ipv4 or ipv6 environment to set Envoy's
			// operational parameters correctly.
			proxyIPv6 := isIPv6Proxy(role.IPAddresses)

			proxyConfig, err := config.ConstructProxyConfig(meshConfigFile, serviceCluster, proxyConfigEnv, concurrency, role)
			if err != nil {
				return fmt.Errorf("failed to get proxy config: %v", err)
			}
			if out, err := gogoprotomarshal.ToYAML(&proxyConfig); err != nil {
				log.Infof("Failed to serialize to YAML: %v", err)
			} else {
				log.Infof("Effective config: %s", out)
			}

			// If not set, set a default based on platform - podNamespace.svc.cluster.local for
			// K8S
			role.DNSDomain = getDNSDomain(podNamespace, role.DNSDomain)
			log.WithLabels("ips", role.IPAddresses, "type", role.Type, "id", role.ID, "domain", role.DNSDomain).Info("Proxy role")

			sop := security.Options{
				CAEndpoint:                     caEndpointEnv,
				CAProviderName:                 caProviderEnv,
				PilotCertProvider:              pilotCertProvider,
				OutputKeyCertToDir:             outputKeyCertToDir,
				ProvCert:                       provCert,
				WorkloadUDSPath:                security.DefaultLocalSDSPath,
				ClusterID:                      clusterIDVar.Get(),
				FileMountedCerts:               fileMountedCertsEnv,
				WorkloadNamespace:              podNamespaceVar.Get(),
				ServiceAccount:                 serviceAccountVar.Get(),
				XdsAuthProvider:                xdsAuthProvider.Get(),
				TrustDomain:                    trustDomainEnv,
				Pkcs8Keys:                      pkcs8KeysEnv,
				ECCSigAlg:                      eccSigAlgEnv,
				SecretTTL:                      secretTTLEnv,
				SecretRotationGracePeriodRatio: secretRotationGracePeriodRatioEnv,
			}
			secOpts, err := secopt.SetupSecurityOptions(proxyConfig, sop, jwtPolicy.Get(),
				credFetcherTypeEnv, credIdentityProvider)
			if err != nil {
				return err
			}
			var tokenManager security.TokenManager
			if stsPort > 0 || xdsAuthProvider.Get() != "" {
				// tokenManager is gcp token manager when using the default token manager plugin.
				tokenManager = tokenmanager.CreateTokenManager(tokenManagerPlugin,
					tokenmanager.Config{CredFetcher: secOpts.CredFetcher, TrustDomain: secOpts.TrustDomain})
			}
			secOpts.TokenManager = tokenManager

			// If security token service (STS) port is not zero, start STS server and
			// listen on STS port for STS requests. For STS, see
			// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16.
			// STS is used for stackdriver or other Envoy services using google gRPC.
			if stsPort > 0 {
				localHostAddr := localHostIPv4
				if proxyIPv6 {
					localHostAddr = localHostIPv6
				}
				stsServer, err := stsserver.NewServer(stsserver.Config{
					LocalHostAddr: localHostAddr,
					LocalPort:     stsPort,
				}, tokenManager)
				if err != nil {
					return err
				}
				defer stsServer.Stop()
			}

			agentConfig := &istio_agent.AgentConfig{
				XDSRootCerts: xdsRootCA,
				CARootCerts:  caRootCA,
				XDSHeaders:   map[string]string{},
				XdsUdsPath:   constants.DefaultXdsUdsPath,
				IsIPv6:       proxyIPv6,
				ProxyType:    role.Type,
			}
			extractXDSHeadersFromEnv(agentConfig)
			if proxyXDSViaAgent {
				agentConfig.ProxyXDSViaAgent = true
				agentConfig.DNSCapture = dnsCaptureByAgent
				agentConfig.ProxyNamespace = podNamespace
				agentConfig.ProxyDomain = role.DNSDomain
			}
			sa := istio_agent.NewAgent(&proxyConfig, agentConfig, secOpts)

			var pilotSAN []string
			if proxyConfig.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS {
				// Obtain Pilot SAN, using DNS.
				pilotSAN = []string{config.GetPilotSan(proxyConfig.DiscoveryAddress)}
			}
			log.Infof("Pilot SAN: %v", pilotSAN)

			// Start in process SDS.
			if err := sa.Start(); err != nil {
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
				if err := initStatusServer(ctx, proxyIPv6, proxyConfig); err != nil {
					return err
				}
			}

			provCert := sa.FindRootCAForXDS()
			if provCert == "" {
				// Envoy only supports load from file. If we want to use system certs, use best guess
				// To be more correct this could lookup all the "well known" paths but this is extremely \
				// unlikely to run on a non-debian based machine, and if it is it can be explicitly configured
				provCert = "/etc/ssl/certs/ca-certificates.crt"
			}
			envoyProxy := envoy.NewProxy(envoy.ProxyConfig{
				Config:              proxyConfig,
				Node:                role.ServiceNode(),
				LogLevel:            proxyLogLevel,
				ComponentLogLevel:   proxyComponentLogLevel,
				LogAsJSON:           loggingOptions.JSONEncoding,
				PilotSubjectAltName: pilotSAN,
				NodeIPs:             role.IPAddresses,
				STSPort:             stsPort,
				OutlierLogPath:      outlierLogPath,
				PilotCertProvider:   pilotCertProvider,
				ProvCert:            provCert,
				Sidecar:             role.Type == model.SidecarProxy,
				ProxyViaAgent:       agentConfig.ProxyXDSViaAgent,
			})

			drainDuration, _ := types.DurationFromProto(proxyConfig.TerminationDrainDuration)
			if ds, f := features.TerminationDrainDuration.Lookup(); f {
				// Legacy environment variable is set, us that instead
				drainDuration = time.Second * time.Duration(ds)
			}

			agent := envoy.NewAgent(envoyProxy, drainDuration)

			// Watcher is also kicking envoy start.
			watcher := envoy.NewWatcher(agent.Restart)
			go watcher.Run(ctx)

			// On SIGINT or SIGTERM, cancel the context, triggering a graceful shutdown
			go cmd.WaitSignalFunc(cancel)

			return agent.Run(ctx)
		},
	}
)

// Simplified extraction of gRPC headers from environment.
// Unlike ISTIO_META, where we need JSON and advanced features - this is just for small string headers.
func extractXDSHeadersFromEnv(config *istio_agent.AgentConfig) {
	envs := os.Environ()
	for _, e := range envs {
		if strings.HasPrefix(e, xdsHeaderPrefix) {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) != 2 {
				continue
			}
			config.XDSHeaders[parts[0][len(xdsHeaderPrefix):]] = parts[1]
		}
	}
}

func initStatusServer(ctx context.Context, proxyIPv6 bool, proxyConfig meshconfig.ProxyConfig) error {
	localHostAddr := localHostIPv4
	if proxyIPv6 {
		localHostAddr = localHostIPv6
	}
	prober := kubeAppProberNameVar.Get()
	statusServer, err := status.NewServer(status.Config{
		LocalHostAddr:  localHostAddr,
		AdminPort:      uint16(proxyConfig.ProxyAdminPort),
		StatusPort:     uint16(proxyConfig.StatusPort),
		KubeAppProbers: prober,
		NodeType:       role.Type,
	})
	if err != nil {
		return err
	}
	go statusServer.Run(ctx)
	return nil
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

func init() {
	proxyCmd.PersistentFlags().StringVar(&role.DNSDomain, "domain", "",
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

// TODO: get the config and bootstrap from istiod, by passing the env

// Use env variables - from injection, k8s and local namespace config map.
// No CLI parameters.
func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Error(err)
		os.Exit(-1)
	}
}

// isIPv6Proxy check the addresses slice and returns true for a valid IPv6 address
// for all other cases it returns false
func isIPv6Proxy(ipAddrs []string) bool {
	for i := 0; i < len(ipAddrs); i++ {
		addr := net.ParseIP(ipAddrs[i])
		if addr == nil {
			// Should not happen, invalid IP in proxy's IPAddresses slice should have been caught earlier,
			// skip it to prevent a panic.
			continue
		}
		if addr.To4() != nil {
			return false
		}
	}
	return true
}
