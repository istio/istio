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

package kube

import (
	"testing"

	coreV1 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/config/protocol"
)

func TestConvertProtocol(t *testing.T) {
	https := "https"
	cases := []struct {
		name          string
		port          int32
		portName      string
		proto         coreV1.Protocol
		appProto      *string
		expectedProto protocol.Instance
	}{
		{
			name:          "resolves empty",
			expectedProto: protocol.Unsupported,
		},
		{
			name:          "resolves from protocol directly",
			proto:         coreV1.ProtocolUDP,
			expectedProto: protocol.UDP,
		},
		{
			name:          "resolves from port name",
			portName:      "http-something",
			expectedProto: protocol.HTTP,
		},
		{
			name:          "prefers appProto over portName",
			portName:      "http-something",
			appProto:      &https,
			expectedProto: protocol.HTTPS,
		},
		{
			name:          "resolves from appProto",
			portName:      "something-httpx",
			appProto:      &https,
			expectedProto: protocol.HTTPS,
		},
		{
			name:          "resolves grpc-web",
			portName:      "grpc-web-x",
			expectedProto: protocol.GRPCWeb,
		},
		{
			name:          "makes sure grpc-web is not resolved incorrectly",
			portName:      "grpcweb-x",
			expectedProto: protocol.Unsupported,
		},
		{
			name:          "resolves based on known ports",
			port:          3306, // mysql
			portName:      "random-name",
			expectedProto: protocol.TCP,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actualProto := ConvertProtocol(tc.port, tc.portName, tc.proto, tc.appProto)
			if actualProto != tc.expectedProto {
				t.Errorf("ConvertProtocol(%d, %s, %s, %v) => %s, want %s",
					tc.port, tc.portName, tc.proto, tc.appProto, actualProto, tc.expectedProto)
			}
		})
	}
}
