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
	"strings"

	gatewayx "sigs.k8s.io/gateway-api/apisx/v1alpha1"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/krt"
)

func backendToServiceEntry(domainSuffix string) krt.TransformationSingle[config.Config, config.Config] {
	// We must enforce that custom trust domains being used for Service FQDNs
	// MUST also enforce that hostnames ending with those trust domains (e.g.
	// .cluster.local) are not allowed.
	// The CRD's CEL rule only covers the default `.cluster.local` suffix.
	reserved := []string{
		".svc." + domainSuffix,
		".svc." + constants.DefaultClusterSetLocalDomain,
	}
	return func(ctx krt.HandlerContext, cfg config.Config) *config.Config {
		backend, ok := cfg.Spec.(*gatewayx.BackendSpec)
		if !ok || backend.Type != gatewayx.BackendTypeExternalHostname || backend.ExternalHostname == nil {
			return nil
		}

		host := string(backend.ExternalHostname.Hostname)
		for _, suffix := range reserved {
			if strings.HasSuffix(host, suffix) {
				return nil
			}
		}

		protocol := "HTTP"
		if backend.Protocol != nil {
			protocol = backendProtocolToServiceEntryProtocol(*backend.Protocol)
		}

		se := &networking.ServiceEntry{
			Hosts: []string{host},
			Ports: []*networking.ServicePort{{
				Number:   uint32(backend.Port.Port),
				Protocol: protocol,
				Name:     protocol,
			}},
			Location:   networking.ServiceEntry_MESH_EXTERNAL,
			Resolution: networking.ServiceEntry_DNS,
			// ExportTo is left unset, so it defaults to public, as it does for a user-authored
			// ServiceEntry. `meshConfig.defaultServiceExportTo` applies here as well.
		}

		return &config.Config{
			Meta: config.Meta{
				GroupVersionKind: gvk.ServiceEntry,
				// `~` cannot appear in a Kubernetes object name, so this cannot collide with a
				// user-authored ServiceEntry.
				Name:              cfg.Name + "~" + constants.KubernetesGatewayName,
				Namespace:         cfg.Namespace,
				CreationTimestamp: cfg.CreationTimestamp,
			},
			Spec: se,
		}
	}
}

func backendProtocolToServiceEntryProtocol(p gatewayx.BackendProtocol) string {
	switch p {
	case gatewayx.BackendProtocolHTTP, gatewayx.BackendProtocolHTTP11:
		return "HTTP"
	case gatewayx.BackendProtocolHTTP2, gatewayx.BackendProtocolH2C:
		return "HTTP2"
	case gatewayx.BackendProtocolGRPC:
		return "GRPC"
	case gatewayx.BackendProtocolMCP:
		// ServicePort protocol cannot explicitly be MCP.
		return "HTTP"
	case gatewayx.BackendProtocolTCP:
		return "TCP"
	default:
		// Any unrecognized protocol falls back to plain TCP.
		return "TCP"
	}
}
