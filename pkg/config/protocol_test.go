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

// This file describes the abstract model of services (and their instances) as
// represented in Istio. This model is independent of the underlying platform
// (Kubernetes, Mesos, etc.). Platform specific adapters found populate the
// model object with various fields, from the metadata found in the platform.
// The platform independent proxy code uses the representation in the model to
// generate the configuration files for the Layer 7 proxy sidecar. The proxy
// code is specific to individual proxy implementations

package config

import "testing"

func TestHTTPProtocol(t *testing.T) {
	if ProtocolUDP.IsHTTP() {
		t.Errorf("UDP is not HTTP protocol")
	}
	if !ProtocolGRPC.IsHTTP() {
		t.Errorf("gRPC is HTTP protocol")
	}
}

func TestParseProtocol(t *testing.T) {
	var testPairs = []struct {
		name string
		out  Protocol
	}{
		{"tcp", ProtocolTCP},
		{"http", ProtocolHTTP},
		{"HTTP", ProtocolHTTP},
		{"Http", ProtocolHTTP},
		{"https", ProtocolHTTPS},
		{"http2", ProtocolHTTP2},
		{"grpc", ProtocolGRPC},
		{"grpc-web", ProtocolGRPCWeb},
		{"gRPC-Web", ProtocolGRPCWeb},
		{"grpc-Web", ProtocolGRPCWeb},
		{"udp", ProtocolUDP},
		{"Mongo", ProtocolMongo},
		{"mongo", ProtocolMongo},
		{"MONGO", ProtocolMongo},
		{"Redis", ProtocolRedis},
		{"redis", ProtocolRedis},
		{"REDIS", ProtocolRedis},
		{"Mysql", ProtocolMySQL},
		{"mysql", ProtocolMySQL},
		{"MYSQL", ProtocolMySQL},
		{"MySQL", ProtocolMySQL},
		{"", ProtocolUnsupported},
		{"SMTP", ProtocolUnsupported},
	}

	for _, testPair := range testPairs {
		out := ParseProtocol(testPair.name)
		if out != testPair.out {
			t.Errorf("ParseProtocol(%q) => %q, want %q", testPair.name, out, testPair.out)
		}
	}
}
