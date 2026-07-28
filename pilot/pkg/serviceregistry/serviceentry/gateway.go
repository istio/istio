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

package serviceentry

import (
	gatewayx "sigs.k8s.io/gateway-api/apisx/v1alpha1"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/krt"
)

func backendToServiceEntry(ctx krt.HandlerContext, cfg config.Config) *config.Config {
	gvk := gvk.ServiceEntry
	backend, ok := cfg.Spec.(*gatewayx.BackendSpec)
	if !ok || backend.Type != gatewayx.BackendTypeExternalHostname || backend.ExternalHostname == nil {
		return nil
	}

	protocol := "TCP"
	if backend.Protocol != nil {
		protocol = backendProtocolToServiceEntryProtocol(*backend.Protocol)
	}

	se := &networking.ServiceEntry{
		Hosts: []string{string(backend.ExternalHostname.Hostname)},
		Ports: []*networking.ServicePort{{
			Number:   uint32(backend.Port.Port),
			Protocol: protocol,
			Name:     protocol,
		}},
		Location:   networking.ServiceEntry_MESH_EXTERNAL,
		Resolution: networking.ServiceEntry_DNS,
		// TODO(ericdbishop): map TLS
	}

	return &config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk,
			Name:             "backend-" + cfg.Name,
			Namespace:        cfg.Namespace,
		},
		Spec: se,
	}
}

func backendProtocolToServiceEntryProtocol(p gatewayx.BackendProtocol) string {
	switch p {
	case gatewayx.BackendProtocolHTTP:
		return "HTTP"
	case gatewayx.BackendProtocolHTTP2, gatewayx.BackendProtocolH2C:
		return "HTTP2"
	case gatewayx.BackendProtocolHTTP11:
		return "HTTP"
	case gatewayx.BackendProtocolGRPC:
		return "GRPC"
	default:
		// Any unrecognized protocols fall back to plain TCP.
		return "TCP"
	}
}
