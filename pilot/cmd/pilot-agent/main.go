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
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/types"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/pkg/collateral"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
	"istio.io/pkg/version"

	"istio.io/istio/pilot/cmd/pilot-agent/status"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy"
	envoyDiscovery "istio.io/istio/pilot/pkg/proxy/envoy"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/bootstrap/option"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/validation"
	"istio.io/istio/pkg/envoy"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/gogoprotomarshal"
)

const trustworthyJWTPath = "/var/run/secrets/tokens/istio-token"

var (
	role             = &model.Proxy{}
	proxyIP          string
	registry         serviceregistry.ServiceRegistry
	trustDomain      string
	pilotIdentity    string
	mixerIdentity    string
	statusPort       uint16
	applicationPorts []string

	// proxy config flags (named identically)
	configPath               string
	controlPlaneBootstrap    bool
	binaryPath               string
	serviceCluster           string
	drainDuration            time.Duration
	parentShutdownDuration   time.Duration
	discoveryAddress         string
	zipkinAddress            string
	lightstepAddress         string
	lightstepAccessToken     string
	lightstepSecure          bool
	lightstepCacertPath      string
	datadogAgentAddress      string
	connectTimeout           time.Duration
	statsdUDPAddress         string
	envoyMetricsService      string
	envoyAccessLogService    string
	proxyAdminPort           uint16
	controlPlaneAuthPolicy   string
	customConfigFile         string
	proxyLogLevel            string
	proxyComponentLogLevel   string
	dnsRefreshRate           string
	concurrency              int
	templateFile             string
	disableInternalTelemetry bool
	tlsCertsToWatch          []string
	loggingOptions           = log.DefaultOptions()

	wg sync.WaitGroup

	instanceIPVar             = env.RegisterStringVar("INSTANCE_IP", "", "")
	podNameVar                = env.RegisterStringVar("POD_NAME", "", "")
	podNamespaceVar           = env.RegisterStringVar("POD_NAMESPACE", "", "")
	istioNamespaceVar         = env.RegisterStringVar("ISTIO_NAMESPACE", "", "")
	kubeAppProberNameVar      = env.RegisterStringVar(status.KubeAppProberEnvName, "", "")
	sdsEnabledVar             = env.RegisterBoolVar("SDS_ENABLED", false, "")
	sdsUdsPathVar             = env.RegisterStringVar("SDS_UDS_PATH", "unix:/var/run/sds/uds_path", "SDS address")
	stackdriverTracingEnabled = env.RegisterBoolVar("STACKDRIVER_TRACING_ENABLED", false, "If enabled, stackdriver will"+
		" get configured as the tracer.")
	stackdriverTracingDebug = env.RegisterBoolVar("STACKDRIVER_TRACING_DEBUG", false, "If set to true, "+
		"enables trace output to stdout")
	stackdriverTracingMaxNumberOfAnnotations = env.RegisterIntVar("STACKDRIVER_TRACING_MAX_NUMBER_OF_ANNOTATIONS", 200, "Sets the max"+
		" number of annotations for stackdriver")
	stackdriverTracingMaxNumberOfAttributes = env.RegisterIntVar("STACKDRIVER_TRACING_MAX_NUMBER_OF_ATTRIBUTES", 200, "Sets the max "+
		"number of attributes for stackdriver")
	stackdriverTracingMaxNumberOfMessageEvents = env.RegisterIntVar("STACKDRIVER_TRACING_MAX_NUMBER_OF_MESSAGE_EVENTS", 200, "Sets the "+
		"max number of message events for stackdriver")

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
				if registry == serviceregistry.KubernetesRegistry {
					role.ID = podName + "." + podNamespace
				} else if registry == serviceregistry.ConsulRegistry {
					role.ID = role.IPAddresses[0] + ".service.consul"
				} else {
					role.ID = role.IPAddresses[0]
				}
			}

			trustDomain = spiffe.DetermineTrustDomain(trustDomain, true)
			spiffe.SetTrustDomain(trustDomain)
			log.Infof("Proxy role: %#v", role)

			tlsCertsToWatch = []string{
				tlsServerCertChain, tlsServerKey, tlsServerRootCert,
				tlsClientCertChain, tlsClientKey, tlsClientRootCert,
			}

			proxyConfig := mesh.DefaultProxyConfig()

			// set all flags
			proxyConfig.CustomConfigFile = customConfigFile
			proxyConfig.ConfigPath = configPath
			proxyConfig.BinaryPath = binaryPath
			proxyConfig.ServiceCluster = serviceCluster
			proxyConfig.DrainDuration = types.DurationProto(drainDuration)
			proxyConfig.ParentShutdownDuration = types.DurationProto(parentShutdownDuration)
			proxyConfig.DiscoveryAddress = discoveryAddress
			proxyConfig.ConnectTimeout = types.DurationProto(connectTimeout)
			proxyConfig.StatsdUdpAddress = statsdUDPAddress
			if envoyMetricsService != "" {
				if ms := fromJSON(envoyMetricsService); ms != nil {
					proxyConfig.EnvoyMetricsService = ms
					appendTLSCerts(ms)
				}
			}
			if envoyAccessLogService != "" {
				if rs := fromJSON(envoyAccessLogService); rs != nil {
					proxyConfig.EnvoyAccessLogService = rs
					appendTLSCerts(rs)
				}
			}
			proxyConfig.ProxyAdminPort = int32(proxyAdminPort)
			proxyConfig.Concurrency = int32(concurrency)

			var pilotSAN []string
			controlPlaneAuthEnabled := false
			ns := ""
			switch controlPlaneAuthPolicy {
			case meshconfig.AuthenticationPolicy_NONE.String():
				proxyConfig.ControlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_NONE
			case meshconfig.AuthenticationPolicy_MUTUAL_TLS.String():
				controlPlaneAuthEnabled = true
				proxyConfig.ControlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS
				if registry == serviceregistry.KubernetesRegistry {
					partDiscoveryAddress := strings.Split(discoveryAddress, ":")
					discoveryHostname := partDiscoveryAddress[0]
					parts := strings.Split(discoveryHostname, ".")
					if len(parts) == 1 {
						// namespace of pilot is not part of discovery address use
						// pod namespace e.g. istio-pilot:15005
						ns = podNamespace
					} else if len(parts) == 2 {
						// namespace is found in the discovery address
						// e.g. istio-pilot.istio-system:15005
						ns = parts[1]
					} else {
						// discovery address is a remote address. For remote clusters
						// only support the default config, or env variable
						ns = istioNamespaceVar.Get()
						if ns == "" {
							ns = constants.IstioSystemNamespace
						}
					}
				}
			}
			role.DNSDomain = getDNSDomain(podNamespace, role.DNSDomain)
			setSpiffeTrustDomain(podNamespace, role.DNSDomain)

			// Obtain the Pilot and Mixer SANs. Used below to create a Envoy proxy.
			pilotSAN = getSAN(ns, envoyDiscovery.PilotSvcAccName, pilotIdentity)
			log.Infof("PilotSAN %#v", pilotSAN)
			mixerSAN := getSAN(ns, envoyDiscovery.MixerSvcAccName, mixerIdentity)
			log.Infof("MixerSAN %#v", mixerSAN)

			// resolve statsd address
			if proxyConfig.StatsdUdpAddress != "" {
				addr, err := proxy.ResolveAddr(proxyConfig.StatsdUdpAddress)
				if err != nil {
					// If istio-mixer.istio-system can't be resolved, skip generating the statsd config.
					// (instead of crashing). Mixer is optional.
					log.Warnf("resolve StatsdUdpAddress failed: %v", err)
					proxyConfig.StatsdUdpAddress = ""
				} else {
					proxyConfig.StatsdUdpAddress = addr
				}
			}

			// set tracing config
			if lightstepAddress != "" {
				proxyConfig.Tracing = &meshconfig.Tracing{
					Tracer: &meshconfig.Tracing_Lightstep_{
						Lightstep: &meshconfig.Tracing_Lightstep{
							Address:     lightstepAddress,
							AccessToken: lightstepAccessToken,
							Secure:      lightstepSecure,
							CacertPath:  lightstepCacertPath,
						},
					},
				}
			} else if zipkinAddress != "" {
				proxyConfig.Tracing = &meshconfig.Tracing{
					Tracer: &meshconfig.Tracing_Zipkin_{
						Zipkin: &meshconfig.Tracing_Zipkin{
							Address: zipkinAddress,
						},
					},
				}
			} else if datadogAgentAddress != "" {
				proxyConfig.Tracing = &meshconfig.Tracing{
					Tracer: &meshconfig.Tracing_Datadog_{
						Datadog: &meshconfig.Tracing_Datadog{
							Address: datadogAgentAddress,
						},
					},
				}
			} else if stackdriverTracingEnabled.Get() {
				proxyConfig.Tracing = &meshconfig.Tracing{
					Tracer: &meshconfig.Tracing_Stackdriver_{
						Stackdriver: &meshconfig.Tracing_Stackdriver{
							Debug: stackdriverTracingDebug.Get(),
							MaxNumberOfAnnotations: &types.Int64Value{
								Value: int64(stackdriverTracingMaxNumberOfAnnotations.Get()),
							},
							MaxNumberOfAttributes: &types.Int64Value{
								Value: int64(stackdriverTracingMaxNumberOfAttributes.Get()),
							},
							MaxNumberOfMessageEvents: &types.Int64Value{
								Value: int64(stackdriverTracingMaxNumberOfMessageEvents.Get()),
							},
						},
					},
				}
			}

			if err := validation.ValidateProxyConfig(&proxyConfig); err != nil {
				return err
			}

			if out, err := gogoprotomarshal.ToYAML(&proxyConfig); err != nil {
				log.Infof("Failed to serialize to YAML: %v", err)
			} else {
				log.Infof("Effective config: %s", out)
			}

			sdsUDSPath := sdsUdsPathVar.Get()
			sdsEnabled, sdsTokenPath := detectSds(controlPlaneBootstrap, sdsUDSPath, trustworthyJWTPath)
			// dedupe cert paths so we don't set up 2 watchers for the same file:
			tlsCertsToWatch = dedupeStrings(tlsCertsToWatch)

			// Since Envoy needs the file-mounted certs for mTLS, we wait for them to become available
			// before starting it. Skip waiting cert if sds is enabled, otherwise it takes long time for
			// pod to start.
			if (controlPlaneAuthEnabled || rsTLSEnabled) && !sdsEnabled {
				log.Infof("Monitored certs: %#v", tlsCertsToWatch)
				for _, cert := range tlsCertsToWatch {
					waitForFile(cert, 2*time.Minute)
				}
			}

			// If control plane auth is not mTLS or global SDS flag is turned off, unset UDS path and token path
			// for control plane SDS.
			if !controlPlaneAuthEnabled || !sdsEnabled {
				sdsUDSPath = ""
				sdsTokenPath = ""
			}

			// TODO: change Mixer and Pilot to use standard template and deprecate this custom bootstrap parser
			if controlPlaneBootstrap {
				if templateFile != "" && proxyConfig.CustomConfigFile == "" {
					// Generate a custom bootstrap template.
					opts := []option.Instance{
						option.PodName(podName),
						option.PodNamespace(podNamespace),
						option.MixerSubjectAltName(mixerSAN),
						option.PodIP(podIP),
						option.ControlPlaneAuth(controlPlaneAuthEnabled),
						option.DisableReportCalls(disableInternalTelemetry),
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
				parsedPorts, err := parseApplicationPorts()
				if err != nil {
					cancel()
					return err
				}
				localHostAddr := "127.0.0.1"
				if proxyIPv6 {
					localHostAddr = "[::1]"
				}
				prober := kubeAppProberNameVar.Get()
				statusServer, err := status.NewServer(status.Config{
					LocalHostAddr:      localHostAddr,
					AdminPort:          proxyAdminPort,
					StatusPort:         statusPort,
					ApplicationPorts:   parsedPorts,
					KubeAppHTTPProbers: prober,
					NodeType:           role.Type,
				})
				if err != nil {
					cancel()
					return err
				}
				go waitForCompletion(ctx, statusServer.Run)
			}

			log.Infof("PilotSAN %#v", pilotSAN)

			envoyProxy := envoy.NewProxy(envoy.ProxyConfig{
				Config:              proxyConfig,
				Node:                role.ServiceNode(),
				LogLevel:            proxyLogLevel,
				ComponentLogLevel:   proxyComponentLogLevel,
				PilotSubjectAltName: pilotSAN,
				MixerSubjectAltName: mixerSAN,
				NodeIPs:             role.IPAddresses,
				DNSRefreshRate:      dnsRefreshRate,
				PodName:             podName,
				PodNamespace:        podNamespace,
				PodIP:               podIP,
				SDSUDSPath:          sdsUDSPath,
				SDSTokenPath:        sdsTokenPath,
				ControlPlaneAuth:    controlPlaneAuthEnabled,
				DisableReportCalls:  disableInternalTelemetry,
			})

			agent := envoy.NewAgent(envoyProxy, features.TerminationDrainDuration())

			watcher := envoy.NewWatcher(tlsCertsToWatch, agent.Restart)

			go watcher.Run(ctx)

			// On SIGINT or SIGTERM, cancel the context, triggering a graceful shutdown
			go cmd.WaitSignalFunc(cancel)

			return agent.Run(ctx)
		},
	}
)

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
	if controlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS.String() {
		pilotTrustDomain := trustDomain
		if len(pilotTrustDomain) == 0 {
			if registry == serviceregistry.KubernetesRegistry &&
				(domain == podNamespace+".svc.cluster.local" || domain == "") {
				pilotTrustDomain = "cluster.local"
			} else if registry == serviceregistry.ConsulRegistry &&
				(domain == "service.consul" || domain == "") {
				pilotTrustDomain = ""
			} else {
				pilotTrustDomain = domain
			}
		}
		spiffe.SetTrustDomain(pilotTrustDomain)
	}

}

func getSAN(ns string, defaultSA string, overrideIdentity string) []string {
	var san []string
	if controlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS.String() {

		if overrideIdentity == "" {
			san = append(san, envoyDiscovery.GetSAN(ns, defaultSA))
		} else {
			san = append(san, envoyDiscovery.GetSAN("", overrideIdentity))
		}
	}
	return san
}

func getDNSDomain(podNamespace, domain string) string {
	if len(domain) == 0 {
		if registry == serviceregistry.KubernetesRegistry {
			domain = podNamespace + ".svc.cluster.local"
		} else if registry == serviceregistry.ConsulRegistry {
			domain = "service.consul"
		} else {
			domain = ""
		}
	}
	return domain
}

// detectSds checks if the SDS address (when it is UDS) and JWT paths are present.
func detectSds(controlPlaneBootstrap bool, sdsAddress, trustworthyJWTPath string) (bool, string) {
	if !sdsEnabledVar.Get() {
		return false, ""
	}

	if len(sdsAddress) == 0 {
		return false, ""
	}

	if _, err := os.Stat(trustworthyJWTPath); err != nil {
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
		return true, trustworthyJWTPath
	}

	if !controlPlaneBootstrap {
		// workload sidecar
		// treat sds as disabled if uds path isn't set.
		if _, err := os.Stat(udsPath); err != nil {
			return false, ""
		}

		return true, trustworthyJWTPath
	}

	// controlplane components like pilot/mixer/galley have sidecar
	// they start almost same time as sds server; wait since there is a chance
	// when pilot-agent start, the uds file doesn't exist.
	if !waitForFile(udsPath, sdsUdsWaitTimeout) {
		return false, ""
	}

	return true, trustworthyJWTPath
}

func parseApplicationPorts() ([]uint16, error) {
	parsedPorts := make([]uint16, 0, len(applicationPorts))
	for _, port := range applicationPorts {
		port := strings.TrimSpace(port)
		if len(port) > 0 {
			parsedPort, err := strconv.ParseUint(port, 10, 16)
			if err != nil {
				return nil, err
			}
			parsedPorts = append(parsedPorts, uint16(parsedPort))
		}
	}
	return parsedPorts, nil
}

func timeDuration(dur *types.Duration) time.Duration {
	out, err := types.DurationFromProto(dur)
	if err != nil {
		log.Warna(err)
	}
	return out
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
	proxyCmd.PersistentFlags().StringVar((*string)(&registry), "serviceregistry",
		string(serviceregistry.KubernetesRegistry),
		fmt.Sprintf("Select the platform for service registry, options are {%s, %s, %s, %s}",
			serviceregistry.KubernetesRegistry, serviceregistry.ConsulRegistry, serviceregistry.MCPRegistry, serviceregistry.MockRegistry))
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

	proxyCmd.PersistentFlags().Uint16Var(&statusPort, "statusPort", 0,
		"HTTP Port on which to serve pilot agent status. If zero, agent status will not be provided.")
	proxyCmd.PersistentFlags().StringSliceVar(&applicationPorts, "applicationPorts", []string{},
		"Ports exposed by the application. Used to determine that Envoy is configured and ready to receive traffic.")

	// Flags for proxy configuration
	values := mesh.DefaultProxyConfig()
	proxyCmd.PersistentFlags().StringVar(&configPath, "configPath", values.ConfigPath,
		"Path to the generated configuration file directory")
	proxyCmd.PersistentFlags().StringVar(&binaryPath, "binaryPath", values.BinaryPath,
		"Path to the proxy binary")
	proxyCmd.PersistentFlags().StringVar(&serviceCluster, "serviceCluster", values.ServiceCluster,
		"Service cluster")
	proxyCmd.PersistentFlags().DurationVar(&drainDuration, "drainDuration",
		timeDuration(values.DrainDuration),
		"The time in seconds that Envoy will drain connections during a hot restart")
	proxyCmd.PersistentFlags().DurationVar(&parentShutdownDuration, "parentShutdownDuration",
		timeDuration(values.ParentShutdownDuration),
		"The time in seconds that Envoy will wait before shutting down the parent process during a hot restart")
	proxyCmd.PersistentFlags().StringVar(&discoveryAddress, "discoveryAddress", values.DiscoveryAddress,
		"Address of the discovery service exposing xDS (e.g. istio-pilot:8080)")
	proxyCmd.PersistentFlags().StringVar(&zipkinAddress, "zipkinAddress", "",
		"Address of the Zipkin service (e.g. zipkin:9411)")
	proxyCmd.PersistentFlags().StringVar(&lightstepAddress, "lightstepAddress", "",
		"Address of the LightStep Satellite pool")
	proxyCmd.PersistentFlags().StringVar(&lightstepAccessToken, "lightstepAccessToken", "",
		"Access Token for LightStep Satellite pool")
	proxyCmd.PersistentFlags().BoolVar(&lightstepSecure, "lightstepSecure", false,
		"Should connection to the LightStep Satellite pool be secure")
	proxyCmd.PersistentFlags().StringVar(&lightstepCacertPath, "lightstepCacertPath", "",
		"Path to the trusted cacert used to authenticate the pool")
	proxyCmd.PersistentFlags().StringVar(&datadogAgentAddress, "datadogAgentAddress", "",
		"Address of the Datadog Agent")
	proxyCmd.PersistentFlags().DurationVar(&connectTimeout, "connectTimeout",
		timeDuration(values.ConnectTimeout),
		"Connection timeout used by Envoy for supporting services")
	proxyCmd.PersistentFlags().StringVar(&statsdUDPAddress, "statsdUdpAddress", values.StatsdUdpAddress,
		"IP Address and Port of a statsd UDP listener (e.g. 10.75.241.127:9125)")
	proxyCmd.PersistentFlags().StringVar(&envoyMetricsService, "envoyMetricsService", "",
		"Settings of an Envoy gRPC Metrics Service API implementation")
	proxyCmd.PersistentFlags().StringVar(&envoyAccessLogService, "envoyAccessLogService", "",
		"Settings of an Envoy gRPC Access Log Service API implementation")
	proxyCmd.PersistentFlags().Uint16Var(&proxyAdminPort, "proxyAdminPort", uint16(values.ProxyAdminPort),
		"Port on which Envoy should listen for administrative commands")
	proxyCmd.PersistentFlags().StringVar(&controlPlaneAuthPolicy, "controlPlaneAuthPolicy",
		values.ControlPlaneAuthPolicy.String(), "Control Plane Authentication Policy")
	proxyCmd.PersistentFlags().StringVar(&customConfigFile, "customConfigFile", values.CustomConfigFile,
		"Path to the custom configuration file")
	// Log levels are provided by the library https://github.com/gabime/spdlog, used by Envoy.
	proxyCmd.PersistentFlags().StringVar(&proxyLogLevel, "proxyLogLevel", "warning",
		fmt.Sprintf("The log level used to start the Envoy proxy (choose from {%s, %s, %s, %s, %s, %s, %s})",
			"trace", "debug", "info", "warning", "error", "critical", "off"))
	// See https://www.envoyproxy.io/docs/envoy/latest/operations/cli#cmdoption-component-log-level
	proxyCmd.PersistentFlags().StringVar(&proxyComponentLogLevel, "proxyComponentLogLevel", "misc:error",
		"The component log level used to start the Envoy proxy")
	proxyCmd.PersistentFlags().StringVar(&dnsRefreshRate, "dnsRefreshRate", "300s",
		"The dns_refresh_rate for bootstrap STRICT_DNS clusters")
	proxyCmd.PersistentFlags().IntVar(&concurrency, "concurrency", int(values.Concurrency),
		"number of worker threads to run")
	proxyCmd.PersistentFlags().StringVar(&templateFile, "templateFile", "",
		"Go template bootstrap config")
	proxyCmd.PersistentFlags().BoolVar(&disableInternalTelemetry, "disableInternalTelemetry", false,
		"Disable internal telemetry")
	proxyCmd.PersistentFlags().BoolVar(&controlPlaneBootstrap, "controlPlaneBootstrap", true,
		"Process bootstrap provided via templateFile to be used by control plane components.")

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
