// Copyright 2020 Istio Authors
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
	"io/ioutil"
	"strconv"
	"strings"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/api/annotation"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/proxy"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/bootstrap"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/validation"
	"istio.io/pkg/log"
)

// getTlsCerts returns all file based certificates from mesh config
// TODO(https://github.com/istio/istio/issues/21834) serve over SDS instead of files
func getTlsCerts(pc meshconfig.ProxyConfig) []string {
	certs := []string{}
	appendTLSCerts := func(rs *meshconfig.RemoteService) {
		if rs.TlsSettings == nil {
			return
		}
		if rs.TlsSettings.Mode == networking.TLSSettings_DISABLE {
			return
		}
		certs = append(certs, rs.TlsSettings.CaCertificates, rs.TlsSettings.ClientCertificate,
			rs.TlsSettings.PrivateKey)
	}
	if pc.EnvoyMetricsService != nil {
		appendTLSCerts(pc.EnvoyMetricsService)
	}
	if pc.EnvoyAccessLogService != nil {
		appendTLSCerts(pc.EnvoyAccessLogService)
	}
	return certs
}

func constructProxyConfig() (meshconfig.ProxyConfig, error) {
	meshConfig, err := getMeshConfig()
	if err != nil {
		return meshconfig.ProxyConfig{}, err
	}
	proxyConfig := mesh.DefaultProxyConfig()
	if meshConfig.DefaultConfig != nil {
		proxyConfig = *meshConfig.DefaultConfig
	}

	// TODO(https://github.com/istio/istio/issues/21222) remove all of these flag overrides
	proxyConfig.CustomConfigFile = customConfigFile
	proxyConfig.ProxyBootstrapTemplatePath = templateFile
	proxyConfig.ConfigPath = configPath
	proxyConfig.BinaryPath = binaryPath
	proxyConfig.ServiceCluster = serviceCluster
	proxyConfig.ProxyAdminPort = int32(proxyAdminPort)
	proxyConfig.Concurrency = int32(concurrency)

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
	if err := validation.ValidateProxyConfig(&proxyConfig); err != nil {
		return meshconfig.ProxyConfig{}, err
	}
	annotations, err := readPodAnnotations()
	if err != nil {
		log.Warnf("failed to read pod annotations: %v", err)
	}
	return applyAnnotations(proxyConfig, annotations), nil
}

func readPodAnnotations() (map[string]string, error) {
	b, err := ioutil.ReadFile(constants.PodInfoAnnotationsPath)
	if err != nil {
		return nil, err
	}
	return bootstrap.ParseDownwardAPI(string(b))
}

// Apply any overrides to proxy config from annotations
func applyAnnotations(config meshconfig.ProxyConfig, annos map[string]string) meshconfig.ProxyConfig {
	if v, f := annos[annotation.SidecarDiscoveryAddress.Name]; f {
		config.DiscoveryAddress = v
	}
	if v, f := annos[annotation.SidecarStatusPort.Name]; f {
		p, err := strconv.Atoi(v)
		if err != nil {
			log.Errorf("Invalid annotation %v=%v: %v", annotation.SidecarStatusPort, p, err)
		}
		config.StatusPort = int32(p)
	}
	return config
}

func getControlPlaneNamespace(podNamespace string, discoveryAddress string) string {
	ns := ""
	if registryID == serviceregistry.Kubernetes {
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
	return ns
}
