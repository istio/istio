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

package converter

import (
	"sort"
	"strconv"
	"strings"

	"github.com/gogo/protobuf/types"

	mcp "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry/annotations"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry/pod"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"

	coreV1 "k8s.io/api/core/v1"
)

// Instance of the converter.
type Instance struct {
	domain string
	pods   pod.Cache
}

// New creates a new instance of the converter.
func New(domain string, pods pod.Cache) *Instance {
	return &Instance{
		domain: domain,
		pods:   pods,
	}
}

// Convert applies the conversion function from k8s Service and Endpoints to ServiceEntry. The
// ServiceEntry is passed as an argument (out) in order to enable object reuse in the future.
func (i *Instance) Convert(service *resource.Entry, endpoints *resource.Entry, outMeta *mcp.Metadata,
	out *networking.ServiceEntry) error {
	if err := i.convertService(service, outMeta, out); err != nil {
		return err
	}
	i.convertEndpoints(endpoints, outMeta, out)
	return nil
}

// convertService applies the k8s Service to the output.
func (i *Instance) convertService(service *resource.Entry, outMeta *mcp.Metadata, out *networking.ServiceEntry) error {
	if service == nil {
		// For testing only. Production code will always provide a non-nil service.
		return nil
	}

	spec := service.Item.(*coreV1.ServiceSpec)

	resolution := networking.ServiceEntry_STATIC
	location := networking.ServiceEntry_MESH_INTERNAL
	endpoints := convertExternalServiceEndpoints(spec, service.Metadata)

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

	host := serviceHostname(service.ID.FullName, i.domain)

	// Store everything in the ServiceEntry.
	out.Hosts = []string{host}
	out.Addresses = []string{addr}
	out.Resolution = resolution
	out.Location = location
	out.Ports = ports
	out.Endpoints = endpoints
	out.ExportTo = convertExportTo(service.Metadata.Annotations)

	// Convert Metadata
	outMeta.Name = service.ID.FullName.String()
	outMeta.Labels = service.Metadata.Labels

	// Convert the creation time.
	createTime, err := types.TimestampProto(service.Metadata.CreateTime)
	if err != nil {
		return err
	}
	outMeta.CreateTime = createTime

	// Update the annotations.
	outMeta.Annotations = getOrCreateStringMap(outMeta.Annotations)
	for k, v := range service.Metadata.Annotations {
		outMeta.Annotations[k] = v
	}
	// Add an annotation for the version of the service resource.
	outMeta.Annotations[annotations.ServiceVersion] = string(service.ID.Version)
	return nil
}

func getOrCreateStringMap(in map[string]string) map[string]string {
	if in != nil {
		return in
	}
	return make(map[string]string)
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

// convertEndpoints applies the k8s Endpoints to the output.
func (i *Instance) convertEndpoints(endpoints *resource.Entry, outMeta *mcp.Metadata, out *networking.ServiceEntry) {
	if endpoints == nil {
		return
	}

	spec := endpoints.Item.(*coreV1.Endpoints)
	// Store the subject alternate names in a set to avoid duplicates.
	subjectAltNameSet := make(map[string]struct{})
	eps := make([]*networking.ServiceEntry_Endpoint, 0)

	// A builder for the annotation for not-ready addresses. The ServiceEntry does not support not ready addresses,
	// so we send them as an annotation instead.
	var notReadyBuilder strings.Builder

	for _, subset := range spec.Subsets {
		// Convert the ports for this subset. They will be re-used for each endpoint in the same subset.
		ports := make(map[string]uint32)
		for _, port := range subset.Ports {
			ports[port.Name] = uint32(port.Port)

			// Process the not ready addresses.
			portString := strconv.Itoa(int(port.Port))
			for _, address := range subset.NotReadyAddresses {
				if notReadyBuilder.Len() > 0 {
					// Add a separator between the addresses.
					notReadyBuilder.WriteByte(',')
				}
				notReadyBuilder.WriteString(address.IP)
				notReadyBuilder.WriteByte(':')
				notReadyBuilder.WriteString(portString)
			}
		}

		// Convert the endpoints in this subset.
		for _, address := range subset.Addresses {
			locality := ""
			var labels map[string]string

			ip := address.IP
			p, hasPod := i.pods.GetPodByIP(ip)
			if hasPod {
				labels = p.Labels
				locality = p.Locality
				if p.ServiceAccountName != "" {
					subjectAltNameSet[p.ServiceAccountName] = struct{}{}
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

	// Add an annotation for the version of the Endpoints resource.
	outMeta.Annotations = getOrCreateStringMap(outMeta.Annotations)
	outMeta.Annotations[annotations.EndpointsVersion] = string(endpoints.ID.Version)

	// Add an annotation for any "not ready" endpoints.
	if notReadyBuilder.Len() > 0 {
		outMeta.Annotations[annotations.NotReadyEndpoints] = notReadyBuilder.String()
	}
}

func convertExternalServiceEndpoints(
	svc *coreV1.ServiceSpec,
	serviceMeta resource.Metadata) []*networking.ServiceEntry_Endpoint {

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
