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
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"google.golang.org/grpc/grpclog"

	"istio.io/istio/pkg/dns"

	meshconfig "istio.io/api/mesh/v1alpha1"
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
	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/envoy"
	istio_agent "istio.io/istio/pkg/istio-agent"
	"istio.io/istio/pkg/jwt"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/gogoprotomarshal"
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
	outputKeyCertToDir = env.RegisterStringVar("OUTPUT_CERTS", "",
		"The output directory for the key and certificate. If empty, key and certificate will not be saved. "+
			"Must be set for VMs using provisioning certificates.").Get()
	proxyConfigEnv = env.RegisterStringVar(
		"PROXY_CONFIG",
		"",
		"The proxy configuration. This will be set by the injection - gateways will use file mounts.",
	).Get()

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
			if ipAddrs, ok := proxy.GetPrivateIPs(context.Background()); ok {
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
			sa := istio_agent.NewSDSAgent(proxyConfig.DiscoveryAddress, proxyConfig.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS,
				pilotCertProvider, jwtPath, outputKeyCertToDir, clusterIDVar.Get())

			// Connection to Istiod secure port
			if sa.RequireCerts {
				proxyConfig.ControlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS
			}

			var pilotSAN, mixerSAN []string
			if proxyConfig.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS {
				setSpiffeTrustDomain(podNamespace, role.DNSDomain)
				// Obtain the Mixer SAN, which uses SPIFFE certs. Used below to create a Envoy proxy.
				mixerSAN = getSAN(getControlPlaneNamespace(podNamespace, proxyConfig.DiscoveryAddress), envoyDiscovery.MixerSvcAccName, mixerIdentity)
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

			// dedupe cert paths so we don't set up 2 watchers for the same file
			tlsCerts := dedupeStrings(getTLSCerts(proxyConfig))

			// Since Envoy needs the file-mounted certs for mTLS, we wait for them to become available
			// before starting it.
			if len(tlsCerts) > 0 {
				log.Infof("Monitored certs: %#v", tlsCerts)
				for _, cert := range tlsCerts {
					waitForFile(cert, 2*time.Minute)
				}
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
			})

			agent := envoy.NewAgent(envoyProxy, features.TerminationDrainDuration())

			// Watcher is also kicking envoy start.
			watcher := envoy.NewWatcher(tlsCerts, agent.Restart)
			go watcher.Run(ctx)

			// On SIGINT or SIGTERM, cancel the context, triggering a graceful shutdown
			go cmd.WaitSignalFunc(cancel)

			return agent.Run(ctx)
		},
	}
)

// dedupes the string array and also ignores the empty string.
func dedupeStrings(in []string) []string {
	set := sets.NewSet(in...)
	return set.UnsortedList()
}

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
		san = append(san, envoyDiscovery.GetSAN(ns, defaultSA))
	} else {
		san = append(san, envoyDiscovery.GetSAN("", overrideIdentity))

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
