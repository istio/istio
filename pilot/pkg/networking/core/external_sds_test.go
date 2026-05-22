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

package core

import (
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/host"
)

func TestBuildUpstreamClusterTLSContext_ExternalSDS(t *testing.T) {
	proxy := &model.Proxy{
		Type:         model.Router,
		Metadata:     &model.NodeMetadata{},
		IstioVersion: &model.IstioVersion{Major: 1, Minor: 24},
	}
	push := model.NewPushContext()
	push.Mesh = &meshconfig.MeshConfig{
		ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "my-sds",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_Sds{
					Sds: &meshconfig.MeshConfig_ExtensionProvider_SDSProvider{
						Service: "default/sds.default.svc.cluster.local",
						Port:    1234,
					},
				},
			},
		},
	}
	push.ServiceIndex.HostnameAndNamespace[host.Name("sds.default.svc.cluster.local")] = map[string]*model.Service{
		"default": {
			Hostname: host.Name("sds.default.svc.cluster.local"),
			Attributes: model.ServiceAttributes{
				Name:      "sds",
				Namespace: "default",
			},
		},
	}

	cb := NewClusterBuilder(proxy, &model.PushRequest{Push: push}, model.DisabledCache{})

	opts := &buildClusterOpts{
		mutable: newClusterWrapper(&cluster.Cluster{
			Name:                 "outbound|1234||foo.default.svc.cluster.local",
			ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
		}),
		mesh: push.Mesh,
		push: push,
	}

	tlsSettings := &networking.ClientTLSSettings{
		Mode:           networking.ClientTLSSettings_MUTUAL,
		CredentialName: "sds://my-cert",
	}

	t.Run("with push context", func(t *testing.T) {
		opts.push = push
		ctx, err := cb.buildUpstreamClusterTLSContext(opts, tlsSettings)
		if err != nil {
			t.Fatal(err)
		}

		if ctx == nil {
			t.Fatal("expected tls context")
		}

		sdsConfig := ctx.CommonTlsContext.TlsCertificateSdsSecretConfigs[0]
		if sdsConfig.Name != "my-cert" {
			t.Errorf("expected SDS name my-cert, got %s", sdsConfig.Name)
		}

		apiConfig := sdsConfig.SdsConfig.GetApiConfigSource()
		if apiConfig == nil {
			t.Fatal("expected api config source")
		}

		clusterName := apiConfig.GetGrpcServices()[0].GetEnvoyGrpc().GetClusterName()
		expectedClusterName := "outbound|1234||sds.default.svc.cluster.local"
		if clusterName != expectedClusterName {
			t.Errorf("expected cluster name %s, got %s", expectedClusterName, clusterName)
		}
	})

	t.Run("without push context", func(t *testing.T) {
		opts.push = nil
		ctx, err := cb.buildUpstreamClusterTLSContext(opts, tlsSettings)
		if err != nil {
			t.Fatal(err)
		}

		if ctx == nil {
			t.Fatal("expected tls context")
		}

		sdsConfig := ctx.CommonTlsContext.TlsCertificateSdsSecretConfigs[0]
		// Should fall back to kubernetes:// prefix (ADS)
		expectedName := "kubernetes://my-cert"
		if sdsConfig.Name != expectedName {
			t.Errorf("expected SDS name %s, got %s", expectedName, sdsConfig.Name)
		}

		if sdsConfig.SdsConfig.GetAds() == nil {
			t.Error("expected ADS config source")
		}
	})
}
