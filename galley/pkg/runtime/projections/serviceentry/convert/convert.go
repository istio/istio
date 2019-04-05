// Copyright 2019 Istio Authors
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

package convert

import (
	"sort"
	"strings"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/runtime/log"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry/annotations"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry/node"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry/pod"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"

	coreV1 "k8s.io/api/core/v1"
)

// Annotations augments the annotations for the k8s Service.
func Annotations(service resource.Entry, endpoints *resource.Entry) resource.Annotations {
	out := make(resource.Annotations)
	for k, v := range service.Metadata.Annotations {
		out[k] = v
	}

	out[annotations.ServiceVersion] = string(service.ID.Version)
	if endpoints != nil {
		out[annotations.EndpointsVersion] = string(endpoints.ID.Version)
	}
	return out
}

// Service converts a k8s Service to a networking.ServiceEntry. The target ServiceEntry is passed as an argument (out) in order to
// enable object reuse in the future.
func Service(spec *coreV1.ServiceSpec, metadata resource.Metadata, fullName resource.FullName, domainSuffix string, out *networking.ServiceEntry) {
	resolution := networking.ServiceEntry_STATIC
	location := networking.ServiceEntry_MESH_INTERNAL
	endpoints := convertExternalServiceEndpoints(spec, metadata)

	// Check for an external service
	externalName := ""
	if spec.Type == coreV1.ServiceTypeExternalName && spec.ExternalName != "" {
		externalName = spec.ExternalName
		resolution = networking.ServiceEntry_DNS
		location = networking.ServiceEntry_MESH_EXTERNAL
	}

	// Check for unspecified Cluster IP
	addr := model.UnspecifiedIP
	if spec.ClusterIP != "" && spec.ClusterIP != coreV1.ClusterIPNone {
		addr = spec.ClusterIP
	}
	if addr == model.UnspecifiedIP && externalName == "" {
		// Headless services should not be load balanced
		resolution = networking.ServiceEntry_NONE
	}

	ports := make([]*networking.Port, 0, len(spec.Ports))
	for _, port := range spec.Ports {
		ports = append(ports, convertPort(port))
	}

	host := serviceHostname(fullName, domainSuffix)

	// Store everything in the ServiceEntry.
	out.Hosts = []string{host}
	out.Addresses = []string{addr}
	out.Resolution = resolution
	out.Location = location
	out.Ports = ports
	out.Endpoints = endpoints
	out.ExportTo = convertExportTo(metadata.Annotations)
}

func convertExportTo(annotations resource.Annotations) []string {
	var exportTo map[string]struct{}
	if annotations[kube.ServiceExportAnnotation] != "" {
		exportTo = make(map[string]struct{})
		for _, e := range strings.Split(annotations[kube.ServiceExportAnnotation], ",") {
			exportTo[strings.TrimSpace(e)] = struct{}{}
		}
	}
	if exportTo == nil {
		return nil
	}

	out := make([]string, 0, len(exportTo))
	for k := range exportTo {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Endpoints converts k8s Endpoints to networking.ServiceEntry_Endpoint resources and extracts the service accounts for the endpoints.
// The target ServiceEntry is passed as an argument (out) in order to enable object reuse in the future.
func Endpoints(endpoints *coreV1.Endpoints, pods pod.Cache, nodes node.Cache, out *networking.ServiceEntry) {
	// Store the subject alternate names in a set to avoid duplicates.
	subjectAltNameSet := make(map[string]struct{})
	eps := make([]*networking.ServiceEntry_Endpoint, 0)

	for _, subset := range endpoints.Subsets {
		// Convert the ports for this subset. They will be re-used for each endpoint in the same subset.
		ports := make(map[string]uint32)
		for _, port := range subset.Ports {
			ports[port.Name] = uint32(port.Port)
		}

		// Convert the endpoints in this subset.
		for _, address := range subset.Addresses {
			locality := ""

			ip := address.IP
			p, hasPod := pods.GetPodByIP(ip)
			var labels map[string]string
			if hasPod {
				labels = p.Labels
				if p.ServiceAccountName != "" {
					subjectAltNameSet[p.ServiceAccountName] = struct{}{}
				}

				n, hasNode := nodes.GetNodeByName(p.NodeName)
				if hasNode {
					locality = n.Locality
				} else {
					log.Scope.Warnf("unable to get node %q for pod %q", p.NodeName, p.FullName)
				}
			}

			ep := &networking.ServiceEntry_Endpoint{
				Labels:   labels,
				Address:  ip,
				Ports:    ports,
				Locality: locality,
				// TODO(nmittler): Network: "",
			}
			eps = append(eps, ep)
		}
	}

	// Convert the subject alternate names to an array.
	subjectAltNames := make([]string, 0, len(subjectAltNameSet))
	for k := range subjectAltNameSet {
		subjectAltNames = append(subjectAltNames, k)
	}
	sort.Strings(subjectAltNames)

	out.Endpoints = eps
	out.SubjectAltNames = subjectAltNames
}

func convertExternalServiceEndpoints(svc *coreV1.ServiceSpec, serviceMeta resource.Metadata) []*networking.ServiceEntry_Endpoint {
	endpoints := make([]*networking.ServiceEntry_Endpoint, 0)
	if svc.Type == coreV1.ServiceTypeExternalName && svc.ExternalName != "" {
		// Generate endpoints for the external service.
		ports := make(map[string]uint32)
		for _, port := range svc.Ports {
			ports[port.Name] = uint32(port.Port)
		}
		addr := svc.ExternalName
		endpoints = append(endpoints, &networking.ServiceEntry_Endpoint{
			Address: addr,
			Ports:   ports,
			Labels:  serviceMeta.Labels,
		})
	}
	return endpoints
}

// serviceHostname produces FQDN for a k8s service
func serviceHostname(fullName resource.FullName, domainSuffix string) string {
	namespace, name := fullName.InterpretAsNamespaceAndName()
	if namespace == "" {
		namespace = coreV1.NamespaceDefault
	}
	return name + "." + namespace + ".svc." + domainSuffix
}

func convertPort(port coreV1.ServicePort) *networking.Port {
	return &networking.Port{
		Name:     port.Name,
		Number:   uint32(port.Port),
		Protocol: string(kube.ConvertProtocol(port.Name, port.Protocol)),
	}
}
