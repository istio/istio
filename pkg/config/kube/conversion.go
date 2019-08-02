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

package kube

import (
	"strings"

	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ConvertLabels(obj metaV1.ObjectMeta) labels.Instance {
	out := make(labels.Instance, len(obj.Labels))
	for k, v := range obj.Labels {
		out[k] = v
	}
	return out
}

var grpcWeb = string(protocol.GRPCWeb)
var grpcWebLen = len(grpcWeb)

// ConvertProtocol from k8s protocol and port name
func ConvertProtocol(name string, proto coreV1.Protocol) protocol.Instance {
	out := protocol.TCP
	switch proto {
	case coreV1.ProtocolUDP:
		out = protocol.UDP
	case coreV1.ProtocolTCP:
		if len(name) >= grpcWebLen && strings.EqualFold(name[:grpcWebLen], grpcWeb) {
			out = protocol.GRPCWeb
			break
		}
		i := strings.IndexByte(name, '-')
		if i >= 0 {
			name = name[:i]
		}
		p := protocol.Parse(name)
		if p != protocol.UDP && p != protocol.Unsupported {
			out = p
		}
	}
	return out
}
