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

package protocol_test

import (
	"testing"

	"istio.io/istio/pkg/config/protocol"
)

func TestIsHTTP(t *testing.T) {
	if protocol.UDP.IsHTTP() {
		t.Errorf("UDP is not HTTP protocol")
	}
	if !protocol.GRPC.IsHTTP() {
		t.Errorf("gRPC is HTTP protocol")
	}
}

func TestParse(t *testing.T) {
	var testPairs = []struct {
		name string
		out  protocol.Instance
	}{
		{"tcp", protocol.TCP},
		{"http", protocol.HTTP},
		{"HTTP", protocol.HTTP},
		{"Http", protocol.HTTP},
		{"http_proxy", protocol.HTTP_PROXY},
		{"Http_Proxy", protocol.HTTP_PROXY},
		{"HTTP_PROXY", protocol.HTTP_PROXY},
		{"https", protocol.HTTPS},
		{"http2", protocol.HTTP2},
		{"grpc", protocol.GRPC},
		{"grpc-web", protocol.GRPCWeb},
		{"gRPC-Web", protocol.GRPCWeb},
		{"grpc-Web", protocol.GRPCWeb},
		{"udp", protocol.UDP},
		{"Mongo", protocol.Mongo},
		{"mongo", protocol.Mongo},
		{"MONGO", protocol.Mongo},
		{"Redis", protocol.Redis},
		{"redis", protocol.Redis},
		{"REDIS", protocol.Redis},
		{"Mysql", protocol.MySQL},
		{"mysql", protocol.MySQL},
		{"MYSQL", protocol.MySQL},
		{"MySQL", protocol.MySQL},
		{"", protocol.Unsupported},
		{"SMTP", protocol.Unsupported},
	}

	for _, testPair := range testPairs {
		testName := testPair.name
		if testName == "" {
			testName = "[empty]"
		}
		t.Run(testName, func(t *testing.T) {
			out := protocol.Parse(testPair.name)
			if out != testPair.out {
				t.Fatalf("Parse(%q) => %q, want %q", testPair.name, out, testPair.out)
			}
		})
	}
}
