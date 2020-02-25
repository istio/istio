// Copyright 2017 Istio Authors
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
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	stsserver "istio.io/istio/security/pkg/stsservice/server"
	"istio.io/istio/security/pkg/stsservice/tokenmanager"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/jwt"
	"istio.io/pkg/collateral"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
	"istio.io/pkg/version"

	"istio.io/istio/pilot/cmd/pilot-agent/status"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy"
	envoyDiscovery "istio.io/istio/pilot/pkg/proxy/envoy"
	securityModel "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/bootstrap/option"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/envoy"
	istio_agent "istio.io/istio/pkg/istio-agent"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/gogoprotomarshal"
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
	pilotIdentity      string
	mixerIdentity      string
	statusPort         uint16
	stsPort            int
	tokenManagerPlugin string

	meshConfigFile string

	// proxy config flags (named identically)
	configPath               string
	controlPlaneBootstrap    bool
	binaryPath               string
	serviceCluster           string
	proxyAdminPort           uint16
	customConfigFile         string
	proxyLogLevel            string
	proxyComponentLogLevel   string
	concurrency              int
	templateFile             string
	disableInternalTelemetry bool
	tlsCertsToWatch          []string
	loggingOptions           = log.DefaultOptions()
	outlierLogPath           string

	wg sync.WaitGroup

	instanceIPVar        = env.RegisterStringVar("INSTANCE_IP", "", "")
	podNameVar           = env.RegisterStringVar("POD_NAME", "", "")
	podNamespaceVar      = env.RegisterStringVar("POD_NAMESPACE", "", "")
	istioNamespaceVar    = env.RegisterStringVar("ISTIO_NAMESPACE", "", "")
	kubeAppProberNameVar = env.RegisterStringVar(status.KubeAppProberEnvName, "", "")
	sdsEnabledVar        = env.RegisterBoolVar("SDS_ENABLED", false, "")
	autoMTLSEnabled      = env.RegisterBoolVar("ISTIO_AUTO_MTLS_ENABLED", false, "If true, auto mTLS is enabled, "+
		"sidecar checks key/cert if SDS is not enabled.")
	sdsUdsPathVar = env.RegisterStringVar("SDS_UDS_PATH", "unix:/var/run/sds/uds_path", "SDS address")

	pilotCertProvider = env.RegisterStringVar("PILOT_CERT_PROVIDER", "istiod",
		"the provider of Pilot DNS certificate.").Get()
	jwtPolicy = env.RegisterStringVar("JWT_POLICY", jwt.JWTPolicyThirdPartyJWT,
		"The JWT validation policy.")
	outputKeyCertToDir = env.RegisterStringVar("OUTPUT_KEY_CERT_TO_DIRECTORY", "",
		"The output directory for the key and certificate. If empty, no output of key and certificate.").Get()
	meshConfig = env.RegisterStringVar("MESH_CONFIG", "", "The mesh configuration").Get()

	sdsUdsWaitTimeout = time.Minute

	// Indicates if any the remote services like AccessLogService, MetricsService have enabled tls.
	rsTLSEnabled bool

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

			//Do we need to get IP from the command line or environment?
			if len(proxyIP) != 0 {
				role.IPAddresses = append(role.IPAddresses, proxyIP)
			} else if podIP != nil {
				role.IPAddresses = append(role.IPAddresses, podIP.String())
			}

			// Obtain all the IPs from the node
			if ipAddrs, ok := proxy.GetPrivateIPs(context.Background()); ok {
				log.Infof("Obtained private IP %v", ipAddrs)
				role.IPAddresses = append(role.IPAddresses, ipAddrs...)
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

			tlsCertsToWatch = []string{
				tlsServerCertChain, tlsServerKey, tlsServerRootCert,
				tlsClientCertChain, tlsClientKey, tlsClientRootCert,
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

			role.DNSDomain = getDNSDomain(podNamespace, role.DNSDomain)
			log.Infof("Proxy role: %#v", role)

			var pilotSAN, mixerSAN []string
			if proxyConfig.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS {
				setSpiffeTrustDomain(podNamespace, role.DNSDomain)
				// Obtain the Pilot and Mixer SANs. Used below to create a Envoy proxy.
				pilotSAN = getSAN(getControlPlaneNamespace(podNamespace, proxyConfig.DiscoveryAddress), envoyDiscovery.PilotSvcAccName, pilotIdentity)
				mixerSAN = getSAN(getControlPlaneNamespace(podNamespace, proxyConfig.DiscoveryAddress), envoyDiscovery.MixerSvcAccName, mixerIdentity)
			}
			log.Infof("PilotSAN %#v", pilotSAN)
			log.Infof("MixerSAN %#v", mixerSAN)

			// Legacy - so pilot-agent can be used with citadel node agent.
			// Main will be replaced by istio-agent when we clean up - this code can stay here and be removed with the rest.
			sdsUDSPath := sdsUdsPathVar.Get()
			var jwtPath string
			if jwtPolicy.Get() == jwt.JWTPolicyThirdPartyJWT {
				log.Info("JWT policy is third-party-jwt")
				jwtPath = trustworthyJWTPath
			} else if jwtPolicy.Get() == jwt.JWTPolicyFirstPartyJWT {
				log.Info("JWT policy is first-party-jwt")
				jwtPath = securityModel.K8sSAJwtFileName
			} else {
				err := fmt.Errorf("invalid JWT policy %v", jwtPolicy.Get())
				log.Errorf("%v", err)
				return err
			}
			nodeAgentSDSEnabled, sdsTokenPath := detectSds(controlPlaneBootstrap, sdsUDSPath, jwtPath)

			if !nodeAgentSDSEnabled { // Not using citadel agent - this is either Pilot or Istiod.

				// Istiod and new SDS-only mode doesn't use sdsUdsPathVar - sdsEnabled will be false.
				sa := istio_agent.NewSDSAgent(proxyConfig.DiscoveryAddress, proxyConfig.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS,
					pilotCertProvider, jwtPath, outputKeyCertToDir)

				if sa.JWTPath != "" {
					// If user injected a JWT token for SDS - use SDS.
					nodeAgentSDSEnabled = true
					sdsTokenPath = sa.JWTPath
					sdsUDSPath = sa.SDSAddress

					// Connection to Istiod secure port
					if sa.RequireCerts {
						proxyConfig.ControlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS
					}

					// For normal Istio - start in process SDS.

					// citadel node-agent not found, but we have a K8S JWT available. Start an in-process SDS.
					_, err := sa.Start(role.Type == model.SidecarProxy, podNamespaceVar.Get())
					if err != nil {
						log.Fatala("Failed to start in-process SDS", err)
					}

					if sa.RequireCerts {
						proxyConfig.ControlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS
					}
					if sa.SAN != "" {
						pilotSAN = append(pilotSAN, sa.SAN)
					}
				}
			}

			// dedupe cert paths so we don't set up 2 watchers for the same file:
			tlsCertsToWatch = dedupeStrings(tlsCertsToWatch)

			// Since Envoy needs the file-mounted certs for mTLS, we wait for them to become available
			// before starting it. Skip waiting cert if sds is enabled, otherwise it takes long time for
			// pod to start.
			if (proxyConfig.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS || rsTLSEnabled || autoMTLSEnabled.Get()) && !nodeAgentSDSEnabled {
				log.Infof("Monitored certs: %#v", tlsCertsToWatch)
				for _, cert := range tlsCertsToWatch {
					waitForFile(cert, 2*time.Minute)
				}
			}

			// If control plane auth is not mTLS or global SDS flag is turned off, unset UDS path and token path
			// for control plane SDS.
			if !nodeAgentSDSEnabled {
				sdsUDSPath = ""
				sdsTokenPath = ""
			}

			// If the token and path are present - use SDS.

			// TODO: change Mixer and Pilot to use standard template and deprecate this custom bootstrap parser
			if controlPlaneBootstrap {
				if templateFile != "" && proxyConfig.CustomConfigFile == "" {
					// Generate a custom bootstrap template.
					opts := []option.Instance{
						option.PodName(podName),
						option.PodNamespace(podNamespace),
						option.MixerSubjectAltName(mixerSAN),
						option.PodIP(podIP),
						option.ControlPlaneAuth(proxyConfig.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS),
						option.DisableReportCalls(disableInternalTelemetry),
						option.SDSTokenPath(sdsTokenPath),
						option.SDSUDSPath(sdsUDSPath),
					}

					if stsPort > 0 {
						opts = append(opts, option.STSEnabled(true),
							option.STSPort(stsPort))
					}

					// Check if nodeIP carries IPv4 or IPv6 and set up proxy accordingly
					if proxyIPv6 {
						opts = append(opts, option.Localhost(option.LocalhostIPv6),
							option.Wildcard(option.WildcardIPv6),
							option.DNSLookupFamily(option.DNSLookupFamilyIPv6))
					} else {
						opts = append(opts, option.Localhost(option.LocalhostIPv4),
							option.Wildcard(option.WildcardIPv4),
							option.DNSLookupFamily(option.DNSLookupFamilyIPv4))
					}

					params, err := option.NewTemplateParams(opts...)
					if err != nil {
						return err
					}

					tmpl, err := template.ParseFiles(templateFile)
					if err != nil {
						return err
					}
					var buffer bytes.Buffer
					err = tmpl.Execute(&buffer, params)
					if err != nil {
						return err
					}
					content := buffer.Bytes()
					log.Infof("Static config:\n%s", string(content))
					proxyConfig.CustomConfigFile = proxyConfig.ConfigPath + "/envoy.yaml"
					err = ioutil.WriteFile(proxyConfig.CustomConfigFile, content, 0644)
					if err != nil {
						return err
					}
				}
			} else if templateFile != "" && proxyConfig.CustomConfigFile == "" {
				proxyConfig.ProxyBootstrapTemplatePath = templateFile
			}
			ctx, cancel := context.WithCancel(context.Background())
			// If a status port was provided, start handling status probes.
			if statusPort > 0 {
				localHostAddr := localHostIPv4
				if proxyIPv6 {
					localHostAddr = localHostIPv6
				}
				prober := kubeAppProberNameVar.Get()
				statusServer, err := status.NewServer(status.Config{
					LocalHostAddr:  localHostAddr,
					AdminPort:      proxyAdminPort,
					StatusPort:     statusPort,
					KubeAppProbers: prober,
					NodeType:       role.Type,
				})
				if err != nil {
					cancel()
					return err
				}
				go waitForCompletion(ctx, statusServer.Run)
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
					tokenmanager.Config{TrustDomain: trustDomain})
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
				SDSUDSPath:          sdsUDSPath,
				SDSTokenPath:        sdsTokenPath,
				STSPort:             stsPort,
				ControlPlaneAuth:    proxyConfig.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS,
				DisableReportCalls:  disableInternalTelemetry,
				OutlierLogPath:      outlierLogPath,
				PilotCertProvider:   pilotCertProvider,
			})

			agent := envoy.NewAgent(envoyProxy, features.TerminationDrainDuration())

			if nodeAgentSDSEnabled {
				tlsCertsToWatch = []string{}
			}

			// Watcher is also kicking envoy start.
			watcher := envoy.NewWatcher(tlsCertsToWatch, agent.Restart)
			go watcher.Run(ctx)

			// On SIGINT or SIGTERM, cancel the context, triggering a graceful shutdown
			go cmd.WaitSignalFunc(cancel)

			return agent.Run(ctx)
		},
	}
)

// getMeshConfig gets the mesh config to use for proxy configuration
// 1. Search for MESH_CONFIG env var. This is set in the injection template
// 2. Attempt to read --meshConfigFile. This is used for gateways
// 3. If neither is found, we can fallback to default settings
func getMeshConfig() (meshconfig.MeshConfig, error) {
	defaultConfig := mesh.DefaultMeshConfig()
	if meshConfig != "" {
		mc, err := mesh.ApplyMeshConfig(meshConfig, defaultConfig)
		if err != nil || mc == nil {
			return meshconfig.MeshConfig{}, fmt.Errorf("failed to unmarshal mesh config config [%v]: %v", meshConfig, err)
		}
		return *mc, nil
	}
	b, err := ioutil.ReadFile(meshConfigFile)
	if err != nil {
		log.Warnf("Failed to read mesh config file from %v or MESH_CONFIG. Falling back to defaults: %v", meshConfigFile, err)
		return defaultConfig, nil
	}
	mc, err := mesh.ApplyMeshConfig(string(b), defaultConfig)
	if err != nil || mc == nil {
		return meshconfig.MeshConfig{}, fmt.Errorf("failed to unmarshal mesh config config [%v]: %v", string(b), err)
	}
	return *mc, nil
}

// dedupes the string array and also ignores the empty string.
func dedupeStrings(in []string) []string {
	stringMap := map[string]bool{}
	for _, c := range in {
		if len(c) > 0 {
			stringMap[c] = true
		}
	}
	unique := make([]string, 0)
	for c := range stringMap {
		unique = append(unique, c)
	}
	return unique
}

func waitForCompletion(ctx context.Context, fn func(context.Context)) {
	wg.Add(1)
	fn(ctx)
	wg.Done()
}

//explicitly setting the trustdomain so the pilot and mixer SAN will have same trustdomain
//and the initialization of the spiffe pkg isn't linked to generating pilot's SAN first
func setSpiffeTrustDomain(podNamespace string, domain string) {
	pilotTrustDomain := trustDomain
	if len(pilotTrustDomain) == 0 {
		if registryID == serviceregistry.Kubernetes &&
			(domain == podNamespace+".svc.cluster.local" || domain == "") {
			pilotTrustDomain = "cluster.local"
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
		san = append(san, envoyDiscovery.GetSAN(ns, defaultSA))
	} else {
		san = append(san, envoyDiscovery.GetSAN("", overrideIdentity))

	}
	return san
}

func getDNSDomain(podNamespace, domain string) string {
	if len(domain) == 0 {
		if registryID == serviceregistry.Kubernetes {
			domain = podNamespace + ".svc.cluster.local"
		} else if registryID == serviceregistry.Consul {
			domain = "service.consul"
		} else {
			domain = ""
		}
	}
	return domain
}

// detectSds checks if the SDS address (when it is UDS) and JWT paths are present.
func detectSds(controlPlaneBootstrap bool, sdsAddress, jwtPath string) (bool, string) {
	if !sdsEnabledVar.Get() {
		return false, ""
	}

	if len(sdsAddress) == 0 {
		return false, ""
	}

	if _, err := os.Stat(jwtPath); err != nil {
		return false, ""
	}

	// sdsAddress will not be empty when sdsAddress is a UDS address.
	udsPath := ""
	if strings.HasPrefix(sdsAddress, "unix:") {
		udsPath = strings.TrimPrefix(sdsAddress, "unix:")
		if len(udsPath) == 0 {
			// If sdsAddress is "unix:", it is invalid, return false.
			return false, ""
		}
	} else {
		return true, jwtPath
	}

	if !controlPlaneBootstrap {
		// workload sidecar
		// treat sds as disabled if uds path isn't set.
		if _, err := os.Stat(udsPath); err != nil {
			return false, ""
		}

		return true, jwtPath
	}

	// controlplane components like pilot/mixer/galley have sidecar
	// they start almost same time as sds server; wait since there is a chance
	// when pilot-agent start, the uds file doesn't exist.
	if !waitForFile(udsPath, sdsUdsWaitTimeout) {
		return false, ""
	}

	return true, jwtPath
}

func fromJSON(j string) *meshconfig.RemoteService {
	var m meshconfig.RemoteService
	err := jsonpb.UnmarshalString(j, &m)
	if err != nil {
		log.Warnf("Unable to unmarshal %s: %v", j, err)
		return nil
	}

	return &m
}

func appendTLSCerts(rs *meshconfig.RemoteService) {
	if rs.TlsSettings == nil {
		return
	}
	if rs.TlsSettings.Mode == networking.TLSSettings_DISABLE {
		return
	}
	rsTLSEnabled = true
	tlsCertsToWatch = append(tlsCertsToWatch, rs.TlsSettings.CaCertificates, rs.TlsSettings.ClientCertificate,
		rs.TlsSettings.PrivateKey)
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
	proxyCmd.PersistentFlags().StringVar(&pilotIdentity, "pilotIdentity", "",
		"The identity used as the suffix for pilot's spiffe SAN ")
	proxyCmd.PersistentFlags().StringVar(&mixerIdentity, "mixerIdentity", "",
		"The identity used as the suffix for mixer's spiffe SAN. This would only be used by pilot all other proxy would get this value from pilot")

	proxyCmd.PersistentFlags().StringVar(&meshConfigFile, "meshConfig", "/etc/istio/config/mesh",
		"File name for Istio mesh configuration. If not specified, a default mesh will be used. MESH_CONFIG environment variable takes precedence.")
	proxyCmd.PersistentFlags().Uint16Var(&statusPort, "statusPort", 0,
		"HTTP Port on which to serve pilot agent status. If zero, agent status will not be provided.")
	proxyCmd.PersistentFlags().IntVar(&stsPort, "stsPort", 0,
		"HTTP Port on which to serve Security Token Service (STS). If zero, STS service will not be provided.")
	proxyCmd.PersistentFlags().StringVar(&tokenManagerPlugin, "tokenManagerPlugin", tokenmanager.GoogleTokenExchange,
		"Token provider specific plugin name.")
	// Flags for proxy configuration
	values := mesh.DefaultProxyConfig()
	proxyCmd.PersistentFlags().StringVar(&configPath, "configPath", values.ConfigPath,
		"Path to the generated configuration file directory")
	proxyCmd.PersistentFlags().StringVar(&binaryPath, "binaryPath", values.BinaryPath,
		"Path to the proxy binary")
	proxyCmd.PersistentFlags().StringVar(&serviceCluster, "serviceCluster", values.ServiceCluster,
		"Service cluster")
	proxyCmd.PersistentFlags().Uint16Var(&proxyAdminPort, "proxyAdminPort", uint16(values.ProxyAdminPort),
		"Port on which Envoy should listen for administrative commands")
	proxyCmd.PersistentFlags().StringVar(&customConfigFile, "customConfigFile", values.CustomConfigFile,
		"Path to the custom configuration file")
	// Log levels are provided by the library https://github.com/gabime/spdlog, used by Envoy.
	proxyCmd.PersistentFlags().StringVar(&proxyLogLevel, "proxyLogLevel", "warning",
		fmt.Sprintf("The log level used to start the Envoy proxy (choose from {%s, %s, %s, %s, %s, %s, %s})",
			"trace", "debug", "info", "warning", "error", "critical", "off"))
	// See https://www.envoyproxy.io/docs/envoy/latest/operations/cli#cmdoption-component-log-level
	proxyCmd.PersistentFlags().StringVar(&proxyComponentLogLevel, "proxyComponentLogLevel", "misc:error",
		"The component log level used to start the Envoy proxy")
	proxyCmd.PersistentFlags().IntVar(&concurrency, "concurrency", int(values.Concurrency),
		"number of worker threads to run")
	proxyCmd.PersistentFlags().StringVar(&templateFile, "templateFile", "",
		"Go template bootstrap config")
	proxyCmd.PersistentFlags().BoolVar(&disableInternalTelemetry, "disableInternalTelemetry", false,
		"Disable internal telemetry")
	proxyCmd.PersistentFlags().BoolVar(&controlPlaneBootstrap, "controlPlaneBootstrap", true,
		"Process bootstrap provided via templateFile to be used by control plane components.")
	proxyCmd.PersistentFlags().StringVar(&outlierLogPath, "outlierLogPath", "",
		"The log path for outlier detection")

	// Attach the Istio logging options to the command.
	loggingOptions.AttachCobraFlags(rootCmd)

	cmd.AddFlags(rootCmd)

	rootCmd.AddCommand(proxyCmd)
	rootCmd.AddCommand(version.CobraCommand())

	rootCmd.AddCommand(collateral.CobraCommand(rootCmd, &doc.GenManHeader{
		Title:   "Istio Pilot Agent",
		Section: "pilot-agent CLI",
		Manual:  "Istio Pilot Agent",
	}))
}

func waitForFile(fname string, maxWait time.Duration) bool {
	log.Infof("waiting %v for %s", maxWait, fname)

	logDelay := 1 * time.Second
	nextLog := time.Now().Add(logDelay)
	endWait := time.Now().Add(maxWait)

	for {
		_, err := os.Stat(fname)
		if err == nil {
			return true
		}
		if !os.IsNotExist(err) { // another error (e.g., permission) - likely no point in waiting longer
			log.Errora("error while waiting for file", err.Error())
			return false
		}

		now := time.Now()
		if now.After(endWait) {
			log.Warna("file still not available after", maxWait)
			return false
		}
		if now.After(nextLog) {
			log.Infof("waiting for file")
			logDelay *= 2
			nextLog.Add(logDelay)
		}
		time.Sleep(100 * time.Millisecond)
	}
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
