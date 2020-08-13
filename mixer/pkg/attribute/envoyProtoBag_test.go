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
	"net"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	v2 "github.com/envoyproxy/go-control-plane/envoy/data/accesslog/v2"
	accesslog "github.com/envoyproxy/go-control-plane/envoy/service/accesslog/v2"
	authz "github.com/envoyproxy/go-control-plane/envoy/service/auth/v2"
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/golang/protobuf/ptypes/wrappers"

	attr "istio.io/pkg/attribute"
)

func TestBagEnvoyAuthzHttp(t *testing.T) {
	attrs := envoyProtoAttrsForTestingAuthzHTTP()
	ab := AuthzProtoBag(attrs)

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

func TestBagEnvoyAuthzGrpc(t *testing.T) {
	attrs := envoyProtoAttrsForTestingAuthzGRPC()
	ab := AuthzProtoBag(attrs)

	results := []struct {
		name  string
		value interface{}
	}{
		{"context.protocol", "grpc"},
		{"context.reporter.kind", "inbound"},
		{"destination.ip", []byte(net.ParseIP("10.12.0.254").To16())},
		{"destination.port", int64(8079)},
		{"source.ip", []byte(net.ParseIP("10.12.1.121").To16())},
		{"source.port", int64(46346)},
		{"request.headers", attr.WrapStringMap(map[string]string{":authority": "fortio:8079",
			":path": "/fgrpc.PingServer/Ping", ":method": "POST", "content-type": "application/grpc"})},
		{"request.host", "fortio:8079"},
		{"request.method", "POST"},
		{"request.path", "/fgrpc.PingServer/Ping"},
		{"request.time", time.Unix(1595981863, 977598000)},
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

func TestBagEnvoyAlsGrpc(t *testing.T) {
	msg := envoyProtoAttrsForTestingALSGRPC()
	ab := AccessLogProtoBag(msg, 0)
	results := []struct {
		name  string
		value interface{}
	}{
		{"context.protocol", "grpc"},
		{"context.reporter.kind", "inbound"},
		{"destination.ip", []byte(net.ParseIP("10.12.0.254").To16())},
		{"destination.port", int64(8079)},
		{"source.ip", []byte(net.ParseIP("10.12.1.121").To16())},
		{"source.port", int64(46346)},
		{"request.headers", attr.WrapStringMap(map[string]string{":authority": "fortio:8079", "content-type": "application/grpc"})},
		{"request.host", "fortio:8079"},
		{"request.method", "POST"},
		{"request.time", time.Unix(1595981863, 977598000)},
		{"response.size", int64(17)},
		{"response.total_size", int64(17 + 118)},
		{"response.code", int64(200)},
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

func TestBagEnvoyAlsTcp(t *testing.T) {
	msg := envoyProtoAttrsForTestingALSTCP()
	ab := AccessLogProtoBag(msg, 0)
	results := []struct {
		name  string
		value interface{}
	}{
		{"context.protocol", "tcp"},
		{"context.reporter.kind", "inbound"},
		{"destination.ip", []byte(net.ParseIP("10.12.0.254").To16())},
		{"destination.port", int64(8079)},
		{"source.ip", []byte(net.ParseIP("10.12.1.121").To16())},
		{"source.port", int64(46346)},
		{"connection.received.bytes", int64(uint64(334))},
		{"connection.sent.bytes", int64(uint64(439))},
		{"destination.principal", "cluster.local/ns/default/sa/default"},
		{"source.principal", "cluster.local/ns/default/sa/bookinfo-productpage"},
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

	_ = child.String()
	child.Done()

}

func envoyMutableBagFromProtoForTesing() *attr.MutableBag {
	b := AuthzProtoBag(envoyProtoAttrsForTestingAuthzHTTP())
	return attr.GetMutableBag(b)
}

func envoyProtoAttrsForTestingAuthzHTTP() *authz.CheckRequest {
	return &authz.CheckRequest{
		Attributes: &authz.AttributeContext{
			Source: &authz.AttributeContext_Peer{
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
			Destination: &authz.AttributeContext_Peer{
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
			Request: &authz.AttributeContext_Request{
				Time: &timestamp.Timestamp{
					Seconds: 1594395974,
					Nanos:   114093000,
				},
				Http: &authz.AttributeContext_HttpRequest{
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

}

func envoyProtoAttrsForTestingAuthzGRPC() *authz.CheckRequest {
	return &authz.CheckRequest{
		Attributes: &authz.AttributeContext{
			Source: &authz.AttributeContext_Peer{
				Address: &core.Address{
					Address: &core.Address_SocketAddress{
						SocketAddress: &core.SocketAddress{
							Address: "10.12.1.121",
							PortSpecifier: &core.SocketAddress_PortValue{
								PortValue: 46346,
							},
						},
					},
				},
				Principal: "spiffe://cluster.local/ns/default/sa/default",
			},
			Destination: &authz.AttributeContext_Peer{
				Address: &core.Address{
					Address: &core.Address_SocketAddress{
						SocketAddress: &core.SocketAddress{
							Address: "10.12.0.254",
							PortSpecifier: &core.SocketAddress_PortValue{
								PortValue: 8079,
							},
						},
					},
				},
				Principal: "spiffe://cluster.local/ns/default/sa/bookinfo-productpage",
			},
			Request: &authz.AttributeContext_Request{
				Time: &timestamp.Timestamp{
					Seconds: 1595981863,
					Nanos:   977598000,
				},
				Http: &authz.AttributeContext_HttpRequest{
					Id:       "4294822762638712056",
					Method:   "POST",
					Headers:  map[string]string{":authority": "fortio:8079", ":path": "/fgrpc.PingServer/Ping", ":method": "POST", "content-type": "application/grpc"},
					Path:     "/fgrpc.PingServer/Ping",
					Host:     "fortio:8079",
					Protocol: "HTTP/2",
				},
			},
		},
	}

}

func envoyProtoAttrsForTestingALSGRPC() *accesslog.StreamAccessLogsMessage {

	entry := &v2.HTTPAccessLogEntry{
		CommonProperties: &v2.AccessLogCommon{
			DownstreamDirectRemoteAddress: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address: "10.12.1.121",
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 46346,
						},
					},
				},
			},
			DownstreamLocalAddress: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address: "10.12.0.254",
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 8079,
						},
					},
				},
			},
			ResponseFlags: &v2.ResponseFlags{},
			StartTime: &timestamp.Timestamp{
				Seconds: 1595981863,
				Nanos:   977598000,
			},
			TimeToFirstUpstreamRxByte: &duration.Duration{
				Nanos: 28810555,
			},
			TlsProperties: &v2.TLSProperties{
				TlsSniHostname: "outbound_.8079_._.fortio.default.svc.cluster.local",
				LocalCertificateProperties: &v2.TLSProperties_CertificateProperties{
					SubjectAltName: []*v2.TLSProperties_CertificateProperties_SubjectAltName{
						{
							San: &v2.TLSProperties_CertificateProperties_SubjectAltName_Uri{
								Uri: "spiffe://cluster.local/ns/default/sa/default",
							},
						},
					},
				},
				PeerCertificateProperties: &v2.TLSProperties_CertificateProperties{
					SubjectAltName: []*v2.TLSProperties_CertificateProperties_SubjectAltName{
						{
							San: &v2.TLSProperties_CertificateProperties_SubjectAltName_Uri{
								Uri: "spiffe://cluster.local/ns/default/sa/default",
							},
						},
					},
				},
			},
			UpstreamCluster: "inbound|8079|grpc-ping|fortio.default.svc.cluster.local",
		},
		Request: &v2.HTTPRequestProperties{
			RequestMethod:       core.RequestMethod(3),
			Scheme:              "http",
			Authority:           "fortio:8079",
			UserAgent:           "grpc-go/1.15.0",
			RequestHeadersBytes: uint64(540),
			RequestBodyBytes:    uint64(5),
			RequestHeaders:      map[string]string{":authority": "fortio:8079", "content-type": "application/grpc"},
		},
		Response: &v2.HTTPResponseProperties{
			ResponseCode:         &wrappers.UInt32Value{Value: uint32(200)},
			ResponseHeadersBytes: uint64(118),
			ResponseBodyBytes:    uint64(17),
			ResponseHeaders:      map[string]string{},
		},
	}

	return &accesslog.StreamAccessLogsMessage{
		LogEntries: &accesslog.StreamAccessLogsMessage_HttpLogs{
			HttpLogs: &accesslog.StreamAccessLogsMessage_HTTPAccessLogEntries{
				LogEntry: []*v2.HTTPAccessLogEntry{entry},
			},
		},
	}

}

func envoyProtoAttrsForTestingALSTCP() *accesslog.StreamAccessLogsMessage {
	entry := &v2.TCPAccessLogEntry{
		CommonProperties: &v2.AccessLogCommon{
			DownstreamDirectRemoteAddress: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address: "10.12.1.121",
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 46346,
						},
					},
				},
			},
			DownstreamLocalAddress: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Address: "10.12.0.254",
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: 8079,
						},
					},
				},
			},
			ResponseFlags: &v2.ResponseFlags{},
			StartTime: &timestamp.Timestamp{
				Seconds: 1595981863,
				Nanos:   977598000,
			},
			TimeToFirstUpstreamRxByte: &duration.Duration{
				Nanos: 28810555,
			},
			TlsProperties: &v2.TLSProperties{
				TlsSniHostname: "outbound_.8079_._.fortio.default.svc.cluster.local",
				LocalCertificateProperties: &v2.TLSProperties_CertificateProperties{
					SubjectAltName: []*v2.TLSProperties_CertificateProperties_SubjectAltName{
						{
							San: &v2.TLSProperties_CertificateProperties_SubjectAltName_Uri{
								Uri: "spiffe://cluster.local/ns/default/sa/default",
							},
						},
					},
				},
				PeerCertificateProperties: &v2.TLSProperties_CertificateProperties{
					SubjectAltName: []*v2.TLSProperties_CertificateProperties_SubjectAltName{
						{
							San: &v2.TLSProperties_CertificateProperties_SubjectAltName_Uri{
								Uri: "spiffe://cluster.local/ns/default/sa/bookinfo-productpage",
							},
						},
					},
				},
			},
			UpstreamCluster: "inbound|8079|grpc-ping|fortio.default.svc.cluster.local",
		},
		ConnectionProperties: &v2.ConnectionProperties{
			ReceivedBytes: uint64(334),
			SentBytes:     uint64(439),
		},
	}
	return &accesslog.StreamAccessLogsMessage{
		LogEntries: &accesslog.StreamAccessLogsMessage_TcpLogs{
			TcpLogs: &accesslog.StreamAccessLogsMessage_TCPAccessLogEntries{
				LogEntry: []*v2.TCPAccessLogEntry{entry},
			},
		},
	}
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
