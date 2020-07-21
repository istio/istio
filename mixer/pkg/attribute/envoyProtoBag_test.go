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
package attribute

import (
	"istio.io/pkg/log"
	"net"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	authzGRPC "github.com/envoyproxy/go-control-plane/envoy/service/auth/v2"
	"github.com/golang/protobuf/ptypes/timestamp"

	attr "istio.io/pkg/attribute"
)

func TestBagEnvoy(t *testing.T) {
	attrs := &authzGRPC.CheckRequest{
		Attributes: &authzGRPC.AttributeContext{
			Source: &authzGRPC.AttributeContext_Peer{
				Address: &core.Address{
					Address: &core.Address_SocketAddress{
						SocketAddress: &core.SocketAddress{
							Address: "10.12.1.52",
							PortSpecifier: &core.SocketAddress_PortValue{
								PortValue: 52480,
							},
						},
					},
				},
				Principal: "spiffe://cluster.local/ns/default/sa/bookinfo-ratings",
			},
			Destination: &authzGRPC.AttributeContext_Peer{
				Address: &core.Address{
					Address: &core.Address_SocketAddress{
						SocketAddress: &core.SocketAddress{
							Address: "10.12.2.52",
							PortSpecifier: &core.SocketAddress_PortValue{
								PortValue: 9080,
							},
						},
					},
				},
				Principal: "spiffe://cluster.local/ns/default/sa/bookinfo-productpage",
			},
			Request: &authzGRPC.AttributeContext_Request{
				Time: &timestamp.Timestamp{
					Seconds: 1594395974,
					Nanos:   114093000,
				},
				Http: &authzGRPC.AttributeContext_HttpRequest{
					Id:     "4294822762638712056",
					Method: "POST",
					Headers: map[string]string{":authority": "productpage:9080", ":path": "/", ":method": "POST",
						":accept": "*/*", "content-length": "0"},
					Path:     "/",
					Host:     "productpage:9080",
					Protocol: "HTTP/1.1",
				},
			},
		},
	}
	ab := GetEnvoyProtoBagAuthz(attrs)

	results := []struct {
		name  string
		value interface{}
	}{
		{"context.protocol", "http"},
		{"context.reporter.kind", "inbound"},
		{"destination.ip", []byte(net.ParseIP("10.12.2.52").To16())},
		{"destination.port", int64(9080)},
		{"source.ip", []byte(net.ParseIP("10.12.1.52").To16())},
		{"source.port", int64(52480)},
		{"request.headers", attr.WrapStringMap(map[string]string{":authority": "productpage:9080", ":path": "/",
			":method": "POST",
			":accept": "*/*", "content-length": "0"})},
		{"request.host", "productpage:9080"},
		{"request.method", "POST"},
		{"request.path", "/"},
		{"request.time", time.Unix(1594395974, 114093000)},
	}

	for _, r := range results {
		t.Run(r.name, func(t *testing.T) {
			v, found := ab.Get(r.name)
			if !found {
				t.Error("Got false, expecting true")
			}

			if !attr.Equal(v, r.value) {
				t.Errorf("Got %v, expected %v for %s", v, r.value, r.name)
			}
		})
	}

	if _, found := ab.Get("XYZ"); found {
		t.Error("XYZ was found")
	}

	child := attr.GetMutableBag(ab)
	r, found := ab.Get("request.method")
	if !found || r.(string) != "POST" {
		t.Error("request.method has wrong value")
	}

	_ = child.String()
	child.Done()

}

func envoyMutableBagFromProtoForTesing() *attr.MutableBag {
	b := GetEnvoyProtoBagAuthz(envoyProtoAttrsForTesting())
	return attr.GetMutableBag(b)
}

func envoyProtoAttrsForTesting() *authzGRPC.CheckRequest {
	attrs := &authzGRPC.CheckRequest{
		Attributes: &authzGRPC.AttributeContext{
			Source: &authzGRPC.AttributeContext_Peer{
				Address: &core.Address{
					Address: &core.Address_SocketAddress{
						SocketAddress: &core.SocketAddress{
							Address: "10.12.1.52",
							PortSpecifier: &core.SocketAddress_PortValue{
								PortValue: 52480,
							},
						},
					},
				},
				Principal: "spiffe://cluster.local/ns/default/sa/bookinfo-ratings",
			},
			Destination: &authzGRPC.AttributeContext_Peer{
				Address: &core.Address{
					Address: &core.Address_SocketAddress{
						SocketAddress: &core.SocketAddress{
							Address: "10.12.2.52",
							PortSpecifier: &core.SocketAddress_PortValue{
								PortValue: 9080,
							},
						},
					},
				},
				Principal: "spiffe://cluster.local/ns/default/sa/bookinfo-productpage",
			},
			Request: &authzGRPC.AttributeContext_Request{
				Time: &timestamp.Timestamp{
					Seconds: 1594395974,
					Nanos:   114093000,
				},
				Http: &authzGRPC.AttributeContext_HttpRequest{
					Id:     "4294822762638712056",
					Method: "POST",
					Headers: map[string]string{":authority": "productpage:9080", ":path": "/", ":method": "POST",
						":accept": "*/*", "content-length": "0"},
					Path:     "/",
					Host:     "productpage:9080",
					Protocol: "HTTP/1.1",
				},
			},
		},
	}

	return attrs
}

func EnvoyTestProtoBagContains(t *testing.T) {
	mb := envoyMutableBagFromProtoForTesing()

	if mb.Contains("THIS_KEY_IS_NOT_IN_DICT") {
		t.Errorf("Found unexpected key")
	}

	if mb.Contains("BADKEY") {
		t.Errorf("Found unexpected key")
	}

}

func init() {
	// bump up the log level so log-only logic runs during the tests, for correctness and coverage.
	o := log.DefaultOptions()
	o.SetOutputLevel(log.DefaultScopeName, log.DebugLevel)
	o.SetOutputLevel(scope.Name(), log.DebugLevel)
	_ = log.Configure(o)
}
