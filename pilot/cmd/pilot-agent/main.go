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
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"istio.io/istio/pkg/spiffe"

	"github.com/gogo/protobuf/types"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/cmd/pilot-agent/status"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy"
	"istio.io/istio/pilot/pkg/proxy/envoy"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/collateral"
	"istio.io/istio/pkg/features/pilot"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/version"
)

var (
	role             = &model.Proxy{}
	proxyIP          string
	registry         serviceregistry.ServiceRegistry
	statusPort       uint16
	applicationPorts []string

	// proxy config flags (named identically)
	configPath                 string
	controlPlaneBootstrap      bool
	binaryPath                 string
	serviceCluster             string
	drainDuration              time.Duration
	parentShutdownDuration     time.Duration
	discoveryAddress           string
	zipkinAddress              string
	lightstepAddress           string
	lightstepAccessToken       string
	lightstepSecure            bool
	lightstepCacertPath        string
	datadogAgentAddress        string
	connectTimeout             time.Duration
	statsdUDPAddress           string
	envoyMetricsServiceAddress string
	proxyAdminPort             uint16
	controlPlaneAuthPolicy     string
	customConfigFile           string
	proxyLogLevel              string
	concurrency                int
	templateFile               string
	disableInternalTelemetry   bool
	tlsServerCertChain         string
	tlsServerKey               string
	tlsServerRootCert          string
	tlsClientCertChain         string
	tlsClientKey               string
	tlsClientRootCert          string
	tlsCertsToWatch            []string
	loggingOptions             = log.DefaultOptions()

	wg sync.WaitGroup

	rootCmd = &cobra.Command{
		Use:          "pilot-agent",
		Short:        "Istio Pilot agent.",
		Long:         "Istio Pilot agent runs in the sidecar or gateway container and bootstraps Envoy.",
		SilenceUsage: true,
	}

	proxyCmd = &cobra.Command{
		Use:   "proxy",
		Short: "Envoy proxy agent",
		RunE: func(c *cobra.Command, args []string) error {
			cmd.PrintFlags(c.Flags())
			if err := log.Configure(loggingOptions); err != nil {
				return err
			}
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
			} else {
				envIP := os.Getenv("INSTANCE_IP")
				if len(envIP) > 0 {
					role.IPAddresses = append(role.IPAddresses, envIP)
				}
			}

			// Obtain all the IPs from the node
			if ipAddr, ok := proxy.GetPrivateIPs(context.Background()); ok {
				log.Infof("Obtained private IP %v", ipAddr)
				role.IPAddresses = append(role.IPAddresses, ipAddr...)
			}

			// No IP addresses provided, append 127.0.0.1
			if len(role.IPAddresses) == 0 {
				role.IPAddresses = append(role.IPAddresses, "127.0.0.1")
			}

			if len(role.ID) == 0 {
				if registry == serviceregistry.KubernetesRegistry {
					role.ID = os.Getenv("POD_NAME") + "." + os.Getenv("POD_NAMESPACE")
				} else if registry == serviceregistry.ConsulRegistry {
					role.ID = role.IPAddresses[0] + ".service.consul"
				} else {
					role.ID = role.IPAddresses[0]
				}
			}

			spiffe.SetTrustDomain(spiffe.DetermineTrustDomain(role.TrustDomain, true))
			role.TrustDomain = spiffe.GetTrustDomain()
			log.Infof("Proxy role: %#v", role)

			// Add cert paths as node metadata only if they differ from defaults
			if tlsServerCertChain != model.DefaultCertChain {
				role.Metadata[model.NodeMetadataTLSServerCertChain] = tlsServerCertChain
			}
			if tlsServerKey != model.DefaultKey {
				role.Metadata[model.NodeMetadataTLSServerKey] = tlsServerKey
			}
			if tlsServerRootCert != model.DefaultRootCert {
				role.Metadata[model.NodeMetadataTLSServerRootCert] = tlsServerRootCert
			}

			if tlsClientCertChain != model.DefaultCertChain {
				role.Metadata[model.NodeMetadataTLSClientCertChain] = tlsClientCertChain
			}
			if tlsClientKey != model.DefaultKey {
				role.Metadata[model.NodeMetadataTLSClientKey] = tlsClientKey
			}
			if tlsClientRootCert != model.DefaultRootCert {
				role.Metadata[model.NodeMetadataTLSClientRootCert] = tlsClientRootCert
			}

			tlsCertsToWatch = []string{
				tlsServerCertChain, tlsServerKey, tlsServerRootCert,
				tlsClientCertChain, tlsClientKey, tlsClientCertChain,
			}

			if role.Type == model.Ingress {
				tlsCertsToWatch = append(tlsCertsToWatch, path.Join(model.IngressCertsPath, model.IngressCertFilename))
				tlsCertsToWatch = append(tlsCertsToWatch, path.Join(model.IngressCertsPath, model.IngressKeyFilename))
			}

			// dedupe cert paths so we don't set up 2 watchers for the same file:
			tlsCertsToWatch = dedupeStrings(tlsCertsToWatch)

			proxyConfig := model.DefaultProxyConfig()

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
			proxyConfig.EnvoyMetricsServiceAddress = envoyMetricsServiceAddress
			proxyConfig.ProxyAdminPort = int32(proxyAdminPort)
			proxyConfig.Concurrency = int32(concurrency)

			var pilotSAN []string
			ns := ""
			switch controlPlaneAuthPolicy {
			case meshconfig.AuthenticationPolicy_NONE.String():
				proxyConfig.ControlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_NONE
			case meshconfig.AuthenticationPolicy_MUTUAL_TLS.String():
				proxyConfig.ControlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS
				if registry == serviceregistry.KubernetesRegistry {
					partDiscoveryAddress := strings.Split(discoveryAddress, ":")
					discoveryHostname := partDiscoveryAddress[0]
					parts := strings.Split(discoveryHostname, ".")
					if len(parts) == 1 {
						// namespace of pilot is not part of discovery address use
						// pod namespace e.g. istio-pilot:15005
						ns = os.Getenv("POD_NAMESPACE")
					} else if len(parts) == 2 {
						// namespace is found in the discovery address
						// e.g. istio-pilot.istio-system:15005
						ns = parts[1]
					} else {
						// discovery address is a remote address. For remote clusters
						// only support the default config, or env variable
						ns = os.Getenv("ISTIO_NAMESPACE")
						if ns == "" {
							ns = model.IstioSystemNamespace
						}
					}
				}
			}
			role.DNSDomain = getDNSDomain(role.DNSDomain)
			pilotSAN = getPilotSAN(role.DNSDomain, ns)

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
			}

			if err := model.ValidateProxyConfig(&proxyConfig); err != nil {
				return err
			}

			if out, err := model.ToYAML(&proxyConfig); err != nil {
				log.Infof("Failed to serialize to YAML: %v", err)
			} else {
				log.Infof("Effective config: %s", out)
			}

			log.Infof("Monitored certs: %#v", tlsCertsToWatch)
			// since Envoy needs the certs for mTLS, we wait for them to become available before starting it
			if controlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS.String() {
				for _, cert := range tlsCertsToWatch {
					waitForCerts(cert, 2*time.Minute)
				}
			}

			// TODO: change Mixer and Pilot to use standard template and deprecate this custom bootstrap parser
			if controlPlaneBootstrap {
				if templateFile != "" && proxyConfig.CustomConfigFile == "" {
					opts := make(map[string]string)
					opts["PodName"] = os.Getenv("POD_NAME")
					opts["PodNamespace"] = os.Getenv("POD_NAMESPACE")
					opts["MixerSubjectAltName"] = envoy.GetMixerSAN(opts["PodNamespace"])

					// protobuf encoding of IP_ADDRESS type
					opts["PodIP"] = base64.StdEncoding.EncodeToString(net.ParseIP(os.Getenv("INSTANCE_IP")))

					if proxyConfig.ControlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS {
						opts["ControlPlaneAuth"] = "enable"
					}
					if disableInternalTelemetry {
						opts["DisableReportCalls"] = "true"
					}
					tmpl, err := template.ParseFiles(templateFile)
					if err != nil {
						return err
					}
					var buffer bytes.Buffer
					err = tmpl.Execute(&buffer, opts)
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
			defer func() {
				log.Info("pilot-agent is terminating")
				cancel()
				wg.Wait()
			}()
			// If a status port was provided, start handling status probes.
			if statusPort > 0 {
				parsedPorts, err := parseApplicationPorts()
				if err != nil {
					return err
				}

				prober := os.Getenv(status.KubeAppProberEnvName)
				statusServer, err := status.NewServer(status.Config{
					AdminPort:          proxyAdminPort,
					StatusPort:         statusPort,
					ApplicationPorts:   parsedPorts,
					KubeAppHTTPProbers: prober,
				})
				if err != nil {
					return err
				}
				go waitForCompletion(ctx, statusServer.Run)
			}

			log.Infof("PilotSAN %#v", pilotSAN)

			envoyProxy := envoy.NewProxy(proxyConfig, role.ServiceNode(), proxyLogLevel, pilotSAN, role.IPAddresses)
			agent := proxy.NewAgent(envoyProxy, proxy.DefaultRetry, pilot.TerminationDrainDuration())
			watcher := envoy.NewWatcher(tlsCertsToWatch, agent.ConfigCh())

			go waitForCompletion(ctx, agent.Run)
			go waitForCompletion(ctx, watcher.Run)

			cmd.WaitSignal(make(chan struct{}))
			return nil
		},
	}
)

func dedupeStrings(in []string) []string {
	stringMap := map[string]bool{}
	for _, c := range in {
		stringMap[c] = true
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

func getPilotSAN(domain string, ns string) []string {
	var pilotSAN []string
	if controlPlaneAuthPolicy == meshconfig.AuthenticationPolicy_MUTUAL_TLS.String() {
		pilotTrustDomain := role.TrustDomain
		if len(pilotTrustDomain) == 0 {
			if registry == serviceregistry.KubernetesRegistry &&
				(domain == os.Getenv("POD_NAMESPACE")+".svc.cluster.local" || domain == "") {
				pilotTrustDomain = "cluster.local"
			} else if registry == serviceregistry.ConsulRegistry &&
				(domain == "service.consul" || domain == "") {
				pilotTrustDomain = ""
			} else {
				pilotTrustDomain = domain
			}
		}
		spiffe.SetTrustDomain(pilotTrustDomain)
		pilotSAN = append(pilotSAN, envoy.GetPilotSAN(ns))
	}
	log.Infof("PilotSAN %#v", pilotSAN)
	return pilotSAN
}

func getDNSDomain(domain string) string {
	if len(domain) == 0 {
		if registry == serviceregistry.KubernetesRegistry {
			domain = os.Getenv("POD_NAMESPACE") + ".svc.cluster.local"
		} else if registry == serviceregistry.ConsulRegistry {
			domain = "service.consul"
		} else {
			domain = ""
		}
	}
	return domain
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

func init() {
	proxyCmd.PersistentFlags().StringVar((*string)(&registry), "serviceregistry",
		string(serviceregistry.KubernetesRegistry),
		fmt.Sprintf("Select the platform for service registry, options are {%s, %s, %s}",
			serviceregistry.KubernetesRegistry, serviceregistry.ConsulRegistry, serviceregistry.MockRegistry))
	proxyCmd.PersistentFlags().StringVar(&proxyIP, "ip", "",
		"Proxy IP address. If not provided uses ${INSTANCE_IP} environment variable.")
	proxyCmd.PersistentFlags().StringVar(&role.ID, "id", "",
		"Proxy unique ID. If not provided uses ${POD_NAME}.${POD_NAMESPACE} from environment variables")
	proxyCmd.PersistentFlags().StringVar(&role.DNSDomain, "domain", "",
		"DNS domain suffix. If not provided uses ${POD_NAMESPACE}.svc.cluster.local")
	proxyCmd.PersistentFlags().StringVar(&role.TrustDomain, "trust-domain", "",
		"The domain to use for identities")
	proxyCmd.PersistentFlags().Uint16Var(&statusPort, "statusPort", 0,
		"HTTP Port on which to serve pilot agent status. If zero, agent status will not be provided.")
	proxyCmd.PersistentFlags().StringSliceVar(&applicationPorts, "applicationPorts", []string{},
		"Ports exposed by the application. Used to determine that Envoy is configured and ready to receive traffic.")

	// Flags for proxy configuration
	values := model.DefaultProxyConfig()
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
	proxyCmd.PersistentFlags().StringVar(&envoyMetricsServiceAddress, "envoyMetricsServiceAddress", values.EnvoyMetricsServiceAddress,
		"Host and Port of an Envoy Metrics Service API implementation (e.g. metrics-service:15000)")
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
	proxyCmd.PersistentFlags().IntVar(&concurrency, "concurrency", int(values.Concurrency),
		"number of worker threads to run")
	proxyCmd.PersistentFlags().StringVar(&templateFile, "templateFile", "",
		"Go template bootstrap config")
	proxyCmd.PersistentFlags().BoolVar(&disableInternalTelemetry, "disableInternalTelemetry", false,
		"Disable internal telemetry")
	proxyCmd.PersistentFlags().BoolVar(&controlPlaneBootstrap, "controlPlaneBootstrap", true,
		"Process bootstrap provided via templateFile to be used by control plane components.")

	// server certs
	proxyCmd.PersistentFlags().StringVar(&tlsServerCertChain, "tlsServerCertChain",
		model.DefaultCertChain, "Absolute path to server cert-chain file used for istio mTLS")
	proxyCmd.PersistentFlags().StringVar(&tlsServerKey, "tlsServerKey",
		model.DefaultKey, "Absolute path to server private key file used for istio mTLS")
	proxyCmd.PersistentFlags().StringVar(&tlsServerRootCert, "tlsServerRootCert",
		model.DefaultRootCert, "Absolute path to server root cert file used for istio mTLS")

	// client certs
	proxyCmd.PersistentFlags().StringVar(&tlsClientCertChain, "tlsClientCertChain",
		model.DefaultCertChain, "Absolute path to client cert-chain file used for istio mTLS")
	proxyCmd.PersistentFlags().StringVar(&tlsClientKey, "tlsSClientKey",
		model.DefaultKey, "Absolute path to client key file used for istio mTLS")
	proxyCmd.PersistentFlags().StringVar(&tlsClientRootCert, "tlsClientRootCert",
		model.DefaultRootCert, "Absolute path to client root cert file used for istio mTLS")

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

func waitForCerts(fname string, maxWait time.Duration) {
	log.Infof("waiting %v for %s", maxWait, fname)

	logDelay := 1 * time.Second
	nextLog := time.Now().Add(logDelay)
	endWait := time.Now().Add(maxWait)

	for {
		_, err := os.Stat(fname)
		if err == nil {
			return
		}
		if !os.IsNotExist(err) { // another error (e.g., permission) - likely no point in waiting longer
			log.Errora("error while waiting for certificates", err.Error())
			return
		}

		now := time.Now()
		if now.After(endWait) {
			log.Warna("certificates still not available after", maxWait)
			break
		}
		if now.After(nextLog) {
			log.Infof("waiting for certificates")
			logDelay = logDelay * 2
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
