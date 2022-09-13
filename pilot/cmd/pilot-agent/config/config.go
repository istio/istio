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

package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/api/annotation"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/network"
	"istio.io/istio/pkg/bootstrap"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/validation"
	"istio.io/pkg/log"
)

// ConstructProxyConfig returns proxyConfig
func ConstructProxyConfig(meshConfigFile, serviceCluster, proxyConfigEnv string, concurrency int, role *model.Proxy) (*meshconfig.ProxyConfig, error) {
	annotations, err := bootstrap.ReadPodAnnotations("")
	if err != nil {
		if os.IsNotExist(err) {
			log.Debugf("failed to read pod annotations: %v", err)
		} else {
			log.Warnf("failed to read pod annotations: %v", err)
		}
	}
	var fileMeshContents string
	if fileExists(meshConfigFile) {
		contents, err := os.ReadFile(meshConfigFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read mesh config file %v: %v", meshConfigFile, err)
		}
		fileMeshContents = string(contents)
	}
	meshConfig, err := getMeshConfig(fileMeshContents, annotations[annotation.ProxyConfig.Name], proxyConfigEnv, role.Type == model.SidecarProxy)
	if err != nil {
		return nil, err
	}
	proxyConfig := mesh.DefaultProxyConfig()
	if meshConfig.DefaultConfig != nil {
		proxyConfig = meshConfig.DefaultConfig
	}

	if concurrency != 0 {
		// If --concurrency is explicitly set, we will use that. Otherwise, use source determined by
		// proxy config.
		proxyConfig.Concurrency = &wrapperspb.Int32Value{Value: int32(concurrency)}
	}
	if x, ok := proxyConfig.GetClusterName().(*meshconfig.ProxyConfig_ServiceCluster); ok {
		if x.ServiceCluster == "" {
			proxyConfig.ClusterName = &meshconfig.ProxyConfig_ServiceCluster{ServiceCluster: serviceCluster}
		}
	}
	// resolve statsd address
	if proxyConfig.StatsdUdpAddress != "" {
		addr, err := network.ResolveAddr(proxyConfig.StatsdUdpAddress)
		if err != nil {
			log.Warnf("resolve StatsdUdpAddress failed: %v", err)
			proxyConfig.StatsdUdpAddress = ""
		} else {
			proxyConfig.StatsdUdpAddress = addr
		}
	}
	if err := validation.ValidateMeshConfigProxyConfig(proxyConfig); err != nil {
		return nil, err
	}
	return applyAnnotations(proxyConfig, annotations), nil
}

// getMeshConfig gets the mesh config to use for proxy configuration
// 1. First we take the default config
// 2. Then we apply any settings from file (this comes from gateway mounting configmap)
// 3. Then we apply settings from environment variable (this comes from sidecar injection sticking meshconfig here)
// 4. Then we apply overrides from annotation (this comes from annotation on gateway, passed through downward API)
//
// Merging is done by replacement. Any fields present in the overlay will replace those existing fields, while
// untouched fields will remain untouched. This means lists will be replaced, not appended to, for example.
func getMeshConfig(fileOverride, annotationOverride, proxyConfigEnv string, isSidecar bool) (*meshconfig.MeshConfig, error) {
	mc := mesh.DefaultMeshConfig()
	// Gateway default should be concurrency unset (ie listen on all threads)
	if !isSidecar {
		mc.DefaultConfig.Concurrency = nil
	}

	if fileOverride != "" {
		log.Infof("Apply mesh config from file %v", fileOverride)
		fileMesh, err := mesh.ApplyMeshConfig(fileOverride, mc)
		if err != nil || fileMesh == nil {
			return nil, fmt.Errorf("failed to unmarshal mesh config from file [%v]: %v", fileOverride, err)
		}
		mc = fileMesh
	}

	if proxyConfigEnv != "" {
		log.Infof("Apply proxy config from env %v", proxyConfigEnv)
		envMesh, err := mesh.ApplyProxyConfig(proxyConfigEnv, mc)
		if err != nil || envMesh == nil {
			return nil, fmt.Errorf("failed to unmarshal mesh config from environment [%v]: %v", proxyConfigEnv, err)
		}
		mc = envMesh
	}

	if annotationOverride != "" {
		log.Infof("Apply proxy config from annotation %v", annotationOverride)
		annotationMesh, err := mesh.ApplyProxyConfig(annotationOverride, mc)
		if err != nil || annotationMesh == nil {
			return nil, fmt.Errorf("failed to unmarshal mesh config from annotation [%v]: %v", annotationOverride, err)
		}
		mc = annotationMesh
	}

	return mc, nil
}

func fileExists(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	}
	return true
}

// Apply any overrides to proxy config from annotations
func applyAnnotations(config *meshconfig.ProxyConfig, annos map[string]string) *meshconfig.ProxyConfig {
	if v, f := annos[annotation.SidecarDiscoveryAddress.Name]; f {
		config.DiscoveryAddress = v
	}
	if v, f := annos[annotation.SidecarStatusPort.Name]; f {
		p, err := strconv.Atoi(v)
		if err != nil {
			log.Errorf("Invalid annotation %v=%v: %v", annotation.SidecarStatusPort.Name, v, err)
		}
		config.StatusPort = int32(p)
	}
	return config
}

func GetPilotSan(discoveryAddress string) string {
	discHost := strings.Split(discoveryAddress, ":")[0]
	// For local debugging - the discoveryAddress is set to localhost, but the cert issued for normal SA.
	if discHost == "localhost" {
		discHost = "istiod.istio-system.svc"
	}
	return discHost
}
