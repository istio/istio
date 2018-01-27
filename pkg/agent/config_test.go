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

package agent

import (
	"crypto/sha1"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/test/util"
)


type fileConfig struct {
	meta model.ConfigMeta
	file string
}

const (
	envoySidecarConfig     = "testdata/envoy-sidecar.json"
	envoySidecarAuthConfig = "testdata/envoy-sidecar-auth.json"
)

func makeProxyConfig() meshconfig.ProxyConfig {
	proxyConfig := model.DefaultProxyConfig()
	proxyConfig.ZipkinAddress = "localhost:6000"
	proxyConfig.StatsdUdpAddress = "10.1.1.10:9125"
	proxyConfig.DiscoveryAddress = "istio-pilot.istio-system:15003"
	proxyConfig.DiscoveryRefreshDelay = ptypes.DurationProto(10 * time.Millisecond)
	return proxyConfig
}

var (
	pilotSAN = []string{"spiffe://cluster.local/ns/istio-system/sa/istio-pilot-service-account"}
)

func makeProxyConfigControlPlaneAuth() meshconfig.ProxyConfig {
	proxyConfig := makeProxyConfig()
	proxyConfig.ControlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS
	return proxyConfig
}

func makeMeshConfig() meshconfig.MeshConfig {
	mesh := model.DefaultMeshConfig()
	mesh.MixerAddress = "istio-mixer.istio-system:9091"
	mesh.RdsRefreshDelay = ptypes.DurationProto(10 * time.Millisecond)
	return mesh
}

func TestProxyConfig(t *testing.T) {
	cases := []struct {
		envoyConfigFilename string
	}{
		{
			envoySidecarConfig,
		},
	}

	proxyConfig := makeProxyConfig()
	for _, c := range cases {
		config := buildConfig(proxyConfig, nil)
		if config == nil {
			t.Fatal("Failed to generate config")
		}

		err := config.WriteFile(c.envoyConfigFilename)
		if err != nil {
			t.Fatalf(err.Error())
		}

		util.CompareYAML(c.envoyConfigFilename, t)
	}
}

func TestProxyConfigControlPlaneAuth(t *testing.T) {
	cases := []struct {
		envoyConfigFilename string
	}{
		{
			envoySidecarAuthConfig,
		},
	}

	proxyConfig := makeProxyConfigControlPlaneAuth()
	for _, c := range cases {
		config := buildConfig(proxyConfig, pilotSAN)
		if config == nil {
			t.Fatal("Failed to generate config")
		}

		err := config.WriteFile(c.envoyConfigFilename)
		if err != nil {
			t.Fatalf(err.Error())
		}

		util.CompareYAML(c.envoyConfigFilename, t)
	}
}

func TestTruncateClusterName(t *testing.T) {
	data := make([]byte, MaxClusterNameLength+1)
	for i := range data {
		data[i] = byte('a' + i%26)
	}
	s := string(data) // the alphabet in lowercase, repeating...

	var trunc string
	less := s[:MaxClusterNameLength-1]
	trunc = truncateClusterName(less)
	if trunc != less {
		t.Errorf("Cluster name modified when truncating short cluster name:\nwant %s,\ngot %s", less, trunc)
	}
	eq := s[:MaxClusterNameLength]
	trunc = truncateClusterName(eq)
	if trunc != eq {
		t.Errorf("Cluster name modified when truncating cluster name:\nwant %s,\ngot %s", eq, trunc)
	}
	gt := s[:MaxClusterNameLength+1]
	trunc = truncateClusterName(gt)
	if len(trunc) != MaxClusterNameLength {
		t.Errorf("Cluster name length is not expected: want %d, got %d", MaxClusterNameLength, len(trunc))
	}
	prefixLen := MaxClusterNameLength - sha1.Size*2
	if gt[:prefixLen] != trunc[:prefixLen] {
		t.Errorf("Unexpected prefix:\nwant %s,\ngot %s", gt[:prefixLen], trunc[:prefixLen])
	}
}

