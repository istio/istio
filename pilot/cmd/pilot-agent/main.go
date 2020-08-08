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
	"io/ioutil"
	"net"
	"os"
	"strings"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"google.golang.org/grpc/grpclog"

	"istio.io/istio/pkg/security"

	"istio.io/istio/pkg/dns"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/pkg/collateral"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
	"istio.io/pkg/version"

	"istio.io/istio/pilot/cmd/pilot-agent/status"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	securityutil "istio.io/istio/pilot/pkg/security/authn/utils"
	securityModel "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/util/network"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/envoy"
	istio_agent "istio.io/istio/pkg/istio-agent"
	"istio.io/istio/pkg/jwt"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/gogoprotomarshal"
	"istio.io/istio/security/pkg/credentialfetcher"
	citadel "istio.io/istio/security/pkg/nodeagent/caclient/providers/citadel"
	stsserver "istio.io/istio/security/pkg/stsservice/server"
	"istio.io/istio/security/pkg/stsservice/tokenmanager"
	cleaniptables "istio.io/istio/tools/istio-clean-iptables/pkg/cmd"
	iptables "istio.io/istio/tools/istio-iptables/pkg/cmd"
)

const (
	trustworthyJWTPath = "./var/run/secrets/tokens/istio-token"
	localHostIPv4      = "127.0.0.1"
	localHostIPv6      = "[::1]"
)

// TODO: Move most of this to pkg.

var (
	role               = &model.Proxy{}
	proxyIP            string
	registryID         serviceregistry.ProviderID
	trustDomain        string
	mixerIdentity      string
	stsPort            int
	tokenManagerPlugin string

	meshConfigFile string

	// proxy config flags (named identically)
	serviceCluster           string
	proxyLogLevel            string
	proxyComponentLogLevel   string
	concurrency              int
	templateFile             string
	disableInternalTelemetry bool
	loggingOptions           = log.DefaultOptions()
	outlierLogPath           string

	instanceIPVar        = env.RegisterStringVar("INSTANCE_IP", "", "")
	podNameVar           = env.RegisterStringVar("POD_NAME", "", "")
	podNamespaceVar      = env.RegisterStringVar("POD_NAMESPACE", "", "")
	istioNamespaceVar    = env.RegisterStringVar("ISTIO_NAMESPACE", "", "")
	kubeAppProberNameVar = env.RegisterStringVar(status.KubeAppProberEnvName, "", "")
	clusterIDVar         = env.RegisterStringVar("ISTIO_META_CLUSTER_ID", "", "")

	pilotCertProvider = env.RegisterStringVar("PILOT_CERT_PROVIDER", "istiod",
		"the provider of Pilot DNS certificate.").Get()
	jwtPolicy = env.RegisterStringVar("JWT_POLICY", jwt.PolicyThirdParty,
		"The JWT validation policy.")
	// ProvCert is the environment controlling the use of pre-provisioned certs, for VMs.
	// May also be used in K8S to use a Secret to bootstrap (as a 'refresh key'), but use short-lived tokens
	// with extra SAN (labels, etc) in data path.
	provCert = env.RegisterStringVar("PROV_CERT", "",
		"Set to a directory containing provisioned certs, for VMs").Get()
	outputKeyCertToDir = env.RegisterStringVar("OUTPUT_CERTS", "",
		"The output directory for the key and certificate. If empty, key and certificate will not be saved. "+
			"Must be set for VMs using provisioning certificates.").Get()
	proxyConfigEnv = env.RegisterStringVar(
		"PROXY_CONFIG",
		"",
		"The proxy configuration. This will be set by the injection - gateways will use file mounts.",
	).Get()

	caProviderEnv = env.RegisterStringVar("CA_PROVIDER", "Citadel", "name of authentication provider").Get()
	// TODO: default to same as discovery address
	caEndpointEnv = env.RegisterStringVar("CA_ADDR", "", "Address of the spiffee certificate provider. Defaults to discoveryAddress").Get()

	// TODO: this is a horribly named env, it's really TOKEN_EXCHANGE_PLUGINS - but to avoid breaking
	// it's left unchanged. It may not be needed because we autodetect.
	pluginNamesEnv = env.RegisterStringVar("PLUGINS", "", "Token exchange plugins").Get()

	// This is also disabled by presence of the SDS socket directory
	enableGatewaySDSEnv = env.RegisterBoolVar("ENABLE_INGRESS_GATEWAY_SDS", false,
		"Enable provisioning gateway secrets. Requires Secret read permission").Get()

	// TODO: This is already present in ProxyConfig !!!
	trustDomainEnv = env.RegisterStringVar("TRUST_DOMAIN", "",
		"The trust domain for spiffe certificates").Get()

	secretTTLEnv = env.RegisterDurationVar("SECRET_TTL", 24*time.Hour,
		"The cert lifetime requested by istio agent").Get()

	secretRotationGracePeriodRatioEnv = env.RegisterFloatVar("SECRET_GRACE_PERIOD_RATIO", 0.5,
		"The grace period ratio for the cert rotation, by default 0.5.").Get()
	secretRotationIntervalEnv = env.RegisterDurationVar("SECRET_ROTATION_CHECK_INTERVAL", 5*time.Minute,
		"The ticker to detect and rotate the certificates, by default 5 minutes").Get()
	staledConnectionRecycleIntervalEnv = env.RegisterDurationVar("STALED_CONNECTION_RECYCLE_RUN_INTERVAL", 5*time.Minute,
		"The ticker to detect and close stale connections").Get()
	initialBackoffInMilliSecEnv = env.RegisterIntVar("INITIAL_BACKOFF_MSEC", 0, "").Get()
	pkcs8KeysEnv                = env.RegisterBoolVar("PKCS8_KEY", false,
		"Whether to generate PKCS#8 private keys").Get()
	eccSigAlgEnv        = env.RegisterStringVar("ECC_SIGNATURE_ALGORITHM", "", "The type of ECC signature algorithm to use when generating private keys").Get()
	fileMountedCertsEnv = env.RegisterBoolVar("FILE_MOUNTED_CERTS", false, "").Get()
	useTokenForCSREnv   = env.RegisterBoolVar("USE_TOKEN_FOR_CSR", false, "CSR requires a token").Get()
	credFetcherTypeEnv  = env.RegisterStringVar("CREDENTIAL_FETCHER_TYPE", "",
		"The type of the credential fetcher. Currently supported types include GoogleComputeEngine").Get()
	skipParseTokenEnv = env.RegisterBoolVar("SKIP_PARSE_TOKEN", false,
		"Skip Parse token to inspect information like expiration time in proxy. This may be possible "+
			"for example in vm we don't use token to rotate cert.").Get()

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
		RunE: func(c *cobra.Command, args []string) error {
			cmd.PrintFlags(c.Flags())
			if err := log.Configure(loggingOptions); err != nil {
				return err
			}
			grpclog.SetLoggerV2(grpclog.NewLoggerV2(ioutil.Discard, ioutil.Discard, ioutil.Discard))

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

			if len(proxyIP) != 0 {
				role.IPAddresses = []string{proxyIP}
			} else if podIP != nil {
				role.IPAddresses = []string{podIP.String()}
			}

			// Obtain all the IPs from the node
			if ipAddrs, ok := network.GetPrivateIPs(context.Background()); ok {
				log.Infof("Obtained private IP %v", ipAddrs)
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

			// Check if proxy runs in ipv4 or ipv6 environment to set Envoy's
			// operational parameters correctly.
			proxyIPv6 := isIPv6Proxy(role.IPAddresses)
			if len(role.ID) == 0 {
				if registryID == serviceregistry.Kubernetes {
					role.ID = podName + "." + podNamespace
				} else if registryID == serviceregistry.Consul {
					role.ID = role.IPAddresses[0] + ".service.consul"
				} else {
					role.ID = role.IPAddresses[0]
				}
			}

			proxyConfig, err := constructProxyConfig()
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
			log.Infof("Proxy role: %#v", role)

			var jwtPath string
			if jwtPolicy.Get() == jwt.PolicyThirdParty {
				log.Info("JWT policy is third-party-jwt")
				jwtPath = trustworthyJWTPath
			} else if jwtPolicy.Get() == jwt.PolicyFirstParty {
				log.Info("JWT policy is first-party-jwt")
				jwtPath = securityModel.K8sSAJwtFileName
			} else {
				log.Info("Using existing certs")
			}

			secOpts := &security.Options{
				PilotCertProvider:  pilotCertProvider,
				OutputKeyCertToDir: outputKeyCertToDir,
				ProvCert:           provCert,
				JWTPath:            jwtPath,
				ClusterID:          clusterIDVar.Get(),
				FileMountedCerts:   fileMountedCertsEnv,
				CAEndpoint:         caEndpointEnv,
				UseTokenForCSR:     useTokenForCSREnv,
				CredFetcher:        nil,
			}
			secOpts.PluginNames = strings.Split(pluginNamesEnv, ",")

			secOpts.EnableWorkloadSDS = true

			secOpts.EnableGatewaySDS = enableGatewaySDSEnv
			secOpts.CAProviderName = caProviderEnv

			// TODO: extract from ProxyConfig
			secOpts.TrustDomain = trustDomainEnv
			secOpts.Pkcs8Keys = pkcs8KeysEnv
			secOpts.ECCSigAlg = eccSigAlgEnv
			secOpts.RecycleInterval = staledConnectionRecycleIntervalEnv
			secOpts.ECCSigAlg = eccSigAlgEnv
			secOpts.SecretTTL = secretTTLEnv
			secOpts.SecretRotationGracePeriodRatio = secretRotationGracePeriodRatioEnv
			secOpts.RotationInterval = secretRotationIntervalEnv
			secOpts.InitialBackoffInMilliSec = int64(initialBackoffInMilliSecEnv)
			// Disable the secret eviction for istio agent.
			secOpts.EvictionDuration = 0
			secOpts.SkipParseToken = skipParseTokenEnv

			// TODO (liminw): CredFetcher is a general interface. In 1.7, we limit the use on GCE only because
			// GCE is the only supported plugin at the moment.
			if credFetcherTypeEnv == security.GCE {
				credFetcher, err := credentialfetcher.NewCredFetcher(credFetcherTypeEnv, secOpts.TrustDomain, jwtPath)
				if err != nil {
					return fmt.Errorf("failed to create credential fetcher: %v", err)
				}
				log.Infof("Start credential fetcher of %s type in %s trust domain", credFetcherTypeEnv, secOpts.TrustDomain)
				secOpts.CredFetcher = credFetcher
			}

			sa := istio_agent.NewAgent(&proxyConfig,
				&istio_agent.AgentConfig{}, secOpts)

			var pilotSAN, mixerSAN []string
			if proxyConfig.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS {
				setSpiffeTrustDomain(podNamespace, role.DNSDomain)
				// Obtain the Mixer SAN, which uses SPIFFE certs. Used below to create a Envoy proxy.
				mixerSAN = getSAN(getControlPlaneNamespace(podNamespace, proxyConfig.DiscoveryAddress), securityutil.MixerSvcAccName, mixerIdentity)
				// Obtain Pilot SAN, using DNS.
				pilotSAN = []string{getPilotSan(proxyConfig.DiscoveryAddress)}
			}
			log.Infof("PilotSAN %#v", pilotSAN)
			log.Infof("MixerSAN %#v", mixerSAN)

			// Start in process SDS.
			_, err = sa.Start(role.Type == model.SidecarProxy, podNamespaceVar.Get())
			if err != nil {
				log.Fatala("Failed to start in-process SDS", err)
			}

			// If we are using a custom template file (for control plane proxy, for example), configure this.
			if templateFile != "" && proxyConfig.CustomConfigFile == "" {
				proxyConfig.ProxyBootstrapTemplatePath = templateFile
			}

			ctx, cancel := context.WithCancel(context.Background())

			// If a status port was provided, start handling status probes.
			if proxyConfig.StatusPort > 0 {
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
					cancel()
					return err
				}
				go statusServer.Run(ctx)
			}

			// If security token service (STS) port is not zero, start STS server and
			// listen on STS port for STS requests. For STS, see
			// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16.
			if stsPort > 0 {
				localHostAddr := localHostIPv4
				if proxyIPv6 {
					localHostAddr = localHostIPv6
				}
				tokenManager := tokenmanager.CreateTokenManager(tokenManagerPlugin,
					tokenmanager.Config{CredFetcher: secOpts.CredFetcher, TrustDomain: secOpts.TrustDomain})
				stsServer, err := stsserver.NewServer(stsserver.Config{
					LocalHostAddr: localHostAddr,
					LocalPort:     stsPort,
				}, tokenManager)
				if err != nil {
					cancel()
					return err
				}
				defer stsServer.Stop()
			}

			// Start a local DNS server on 15053, forwarding to DNS-over-TLS server
			// This will not have any impact on app unless interception is enabled.
			// We can't start on 53 - istio-agent runs as user istio-proxy.
			// This is available to apps even if interception is not enabled.

			// TODO: replace hardcoded .global. Right now the ingress templates are
			// hardcoding it as well, so there is little benefit to do it only here.
			if dns.DNSTLSEnableAgent.Get() != "" {
				// In the injection template the only place where global.proxy.clusterDomain
				// is made available is in the --domain param.
				// Instead of introducing a new config, use that.

				dnsSrv := dns.InitDNSAgent(proxyConfig.DiscoveryAddress,
					role.DNSDomain, sa.RootCert,
					[]string{".global."})
				dnsSrv.StartDNS(dns.DNSAgentAddr, nil)
			}

			envoyProxy := envoy.NewProxy(envoy.ProxyConfig{
				Config:              proxyConfig,
				Node:                role.ServiceNode(),
				LogLevel:            proxyLogLevel,
				ComponentLogLevel:   proxyComponentLogLevel,
				PilotSubjectAltName: pilotSAN,
				MixerSubjectAltName: mixerSAN,
				NodeIPs:             role.IPAddresses,
				PodName:             podName,
				PodNamespace:        podNamespace,
				PodIP:               podIP,
				STSPort:             stsPort,
				ControlPlaneAuth:    proxyConfig.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS,
				DisableReportCalls:  disableInternalTelemetry,
				OutlierLogPath:      outlierLogPath,
				PilotCertProvider:   pilotCertProvider,
				ProvCert:            citadel.ProvCert,
				Sidecar:             role.Type == model.SidecarProxy,
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

// explicitly set the trustdomain so the pilot and mixer SAN will have same trustdomain
// and the initialization of the spiffe pkg isn't linked to generating pilot's SAN first
func setSpiffeTrustDomain(podNamespace string, domain string) {
	pilotTrustDomain := trustDomain
	if len(pilotTrustDomain) == 0 {
		if registryID == serviceregistry.Kubernetes &&
			(domain == podNamespace+".svc."+constants.DefaultKubernetesDomain || domain == "") {
			pilotTrustDomain = constants.DefaultKubernetesDomain
		} else if registryID == serviceregistry.Consul &&
			(domain == "service.consul" || domain == "") {
			pilotTrustDomain = ""
		} else {
			pilotTrustDomain = domain
		}
	}
	spiffe.SetTrustDomain(pilotTrustDomain)
}

func getSAN(ns string, defaultSA string, overrideIdentity string) []string {
	var san []string
	if overrideIdentity == "" {
		san = append(san, securityutil.GetSAN(ns, defaultSA))
	} else {
		san = append(san, securityutil.GetSAN("", overrideIdentity))

	}
	return san
}

func getDNSDomain(podNamespace, domain string) string {
	if len(domain) == 0 {
		if registryID == serviceregistry.Kubernetes {
			domain = podNamespace + ".svc." + constants.DefaultKubernetesDomain
		} else if registryID == serviceregistry.Consul {
			domain = "service.consul"
		} else {
			domain = ""
		}
	}
	return domain
}

func init() {
	proxyCmd.PersistentFlags().StringVar((*string)(&registryID), "serviceregistry",
		string(serviceregistry.Kubernetes),
		fmt.Sprintf("Select the platform for service registry, options are {%s, %s, %s}",
			serviceregistry.Kubernetes, serviceregistry.Consul, serviceregistry.Mock))
	proxyCmd.PersistentFlags().StringVar(&proxyIP, "ip", "",
		"Proxy IP address. If not provided uses ${INSTANCE_IP} environment variable.")
	proxyCmd.PersistentFlags().StringVar(&role.ID, "id", "",
		"Proxy unique ID. If not provided uses ${POD_NAME}.${POD_NAMESPACE} from environment variables")
	proxyCmd.PersistentFlags().StringVar(&role.DNSDomain, "domain", "",
		"DNS domain suffix. If not provided uses ${POD_NAMESPACE}.svc.cluster.local")
	proxyCmd.PersistentFlags().StringVar(&trustDomain, "trust-domain", "",
		"The domain to use for identities")
	proxyCmd.PersistentFlags().StringVar(&mixerIdentity, "mixerIdentity", "",
		"The identity used as the suffix for mixer's spiffe SAN. This would only be used by pilot all other proxy would get this value from pilot")

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
	proxyCmd.PersistentFlags().BoolVar(&disableInternalTelemetry, "disableInternalTelemetry", false,
		"Disable internal telemetry")
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
		log.Errora(err)
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
