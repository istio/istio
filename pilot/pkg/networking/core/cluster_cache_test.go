// Copyright Istio Authors. All Rights Reserved.
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
	"fmt"
	"testing"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"

	"istio.io/istio/pilot/pkg/model"
)

// Egress TLS origination: ServiceEntry (HTTP on 443) + DestinationRule tls SIMPLE,
// no caCertificates, so the validation context is the "file-root:system" SDS resource.
const fileCredReproConfig = `
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: httpbin
  namespace: default
spec:
  hosts:
  - httpbin.org
  location: MESH_EXTERNAL
  ports:
  - number: 443
    name: http-tls-origination
    protocol: HTTP
  resolution: DNS
---
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: httpbin
  namespace: default
spec:
  host: httpbin.org
  trafficPolicy:
    tls:
      mode: SIMPLE
      sni: httpbin.org
`

func fileCredReproSdsTarget(t *testing.T, clusters []*cluster.Cluster, name string) string {
	t.Helper()
	for _, c := range clusters {
		if c.Name != name {
			continue
		}
		ts := c.GetTransportSocket()
		if ts == nil {
			for _, m := range c.GetTransportSocketMatches() {
				if m.GetTransportSocket() != nil {
					ts = m.GetTransportSocket()
					break
				}
			}
		}
		if ts == nil {
			t.Fatalf("cluster %s has no transport socket", name)
		}
		utc := &tlsv3.UpstreamTlsContext{}
		if err := ts.GetTypedConfig().UnmarshalTo(utc); err != nil {
			t.Fatal(err)
		}
		vc := utc.GetCommonTlsContext().GetCombinedValidationContext().GetValidationContextSdsSecretConfig()
		var sdsCluster string
		if src, ok := vc.GetSdsConfig().GetConfigSourceSpecifier().(*core.ConfigSource_ApiConfigSource); ok {
			sdsCluster = src.ApiConfigSource.GetGrpcServices()[0].GetEnvoyGrpc().GetClusterName()
		}
		return fmt.Sprintf("%s via %s", vc.GetName(), sdsCluster)
	}
	t.Fatalf("cluster %s not found", name)
	return ""
}

// Same as ConfigGenTest.Clusters, but sets PushRequest.Start: the cache ignores requests
// with a zero Start time.
func buildClustersCached(t *testing.T, cg *ConfigGenTest, p *model.Proxy) ([]*cluster.Cluster, string) {
	t.Helper()
	raw, details := cg.ConfigGen.BuildClusters(p, &model.PushRequest{Push: cg.PushContext(), Start: time.Now()})
	res := make([]*cluster.Cluster, 0, len(raw))
	for _, r := range raw {
		c := &cluster.Cluster{}
		if err := r.Resource.UnmarshalTo(c); err != nil {
			t.Fatal(err)
		}
		res = append(res, c)
	}
	return res, details.AdditionalInfo
}

func TestFileCredentialCDSCacheKey(t *testing.T) {
	const clusterName = "outbound|443||httpbin.org"
	for _, cacheOn := range []bool{false, true} {
		t.Run(fmt.Sprintf("cds-cache=%v", cacheOn), func(t *testing.T) {
			cg := NewConfigGenTest(t, TestOptions{ConfigString: fileCredReproConfig})
			if cacheOn {
				cg.ConfigGen = NewConfigGenerator(model.NewXdsCache())
			}

			// Proxy A: no external SDS socket at startup => no file-credential metadata.
			a := cg.SetupProxy(&model.Proxy{
				ID:       "no-socket.default",
				Metadata: &model.NodeMetadata{Raw: map[string]any{}},
			})
			clA, infoA := buildClustersCached(t, cg, a)
			gotA := fileCredReproSdsTarget(t, clA, clusterName)

			// Proxy B: external SDS socket found => file-credential: "true".
			b := cg.SetupProxy(&model.Proxy{
				ID:       "with-socket.default",
				Metadata: &model.NodeMetadata{Raw: map[string]any{"file-credential": "true"}},
			})
			clB, infoB := buildClustersCached(t, cg, b)
			gotB := fileCredReproSdsTarget(t, clB, clusterName)

			t.Logf("proxy A (no file-credential)      -> %s   [%s]", gotA, infoA)
			t.Logf("proxy B (file-credential=\"true\") -> %s   [%s]", gotB, infoB)

			if gotA != "file-root:system via sds-grpc" {
				t.Errorf("proxy A: want file-root:system via sds-grpc, got %s", gotA)
			}
			if gotB != "file-root:system via sds-files-grpc" {
				t.Errorf("proxy B: want file-root:system via sds-files-grpc, got %s (stale CDS cache entry)", gotB)
			}
		})
	}
}

// Reverse order: the file-credential proxy populates the cache first; the other proxy is then
// handed a cluster that references sds-files-grpc, which its bootstrap doesn't contain.
func TestFileCredentialCDSCacheKeyReverse(t *testing.T) {
	const clusterName = "outbound|443||httpbin.org"
	cg := NewConfigGenTest(t, TestOptions{ConfigString: fileCredReproConfig})
	cg.ConfigGen = NewConfigGenerator(model.NewXdsCache())

	b := cg.SetupProxy(&model.Proxy{
		ID:       "with-socket.default",
		Metadata: &model.NodeMetadata{Raw: map[string]any{"file-credential": "true"}},
	})
	clB, infoB := buildClustersCached(t, cg, b)
	gotB := fileCredReproSdsTarget(t, clB, clusterName)

	a := cg.SetupProxy(&model.Proxy{
		ID:       "no-socket.default",
		Metadata: &model.NodeMetadata{Raw: map[string]any{}},
	})
	clA, infoA := buildClustersCached(t, cg, a)
	gotA := fileCredReproSdsTarget(t, clA, clusterName)

	t.Logf("proxy B (file-credential=\"true\") -> %s   [%s]", gotB, infoB)
	t.Logf("proxy A (no file-credential)      -> %s   [%s]", gotA, infoA)
	if gotA != "file-root:system via sds-grpc" {
		t.Errorf("proxy A: want file-root:system via sds-grpc, got %s (stale CDS cache entry)", gotA)
	}
}

// Proxies with the same file-credential value should still share cache entries.
func TestFileCredentialCDSCacheKeyStillCaches(t *testing.T) {
	const clusterName = "outbound|443||httpbin.org"
	cg := NewConfigGenTest(t, TestOptions{ConfigString: fileCredReproConfig})
	cg.ConfigGen = NewConfigGenerator(model.NewXdsCache())
	var infos []string
	for _, id := range []string{"with-socket-1.default", "with-socket-2.default"} {
		p := cg.SetupProxy(&model.Proxy{ID: id, Metadata: &model.NodeMetadata{Raw: map[string]any{"file-credential": "true"}}})
		cl, info := buildClustersCached(t, cg, p)
		got := fileCredReproSdsTarget(t, cl, clusterName)
		t.Logf("%s -> %s   [%s]", id, got, info)
		infos = append(infos, info)
	}
	if infos[1] != "cached:1/1" {
		t.Errorf("second identical proxy should be a cache hit, got %s", infos[1])
	}
}
