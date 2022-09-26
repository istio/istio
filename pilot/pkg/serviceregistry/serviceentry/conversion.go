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
	"net"
	"strings"
	"time"

	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	labelutil "istio.io/istio/pilot/pkg/serviceregistry/util/label"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/kube/labels"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/spiffe"
)

func convertPort(port *networking.Port) *model.Port {
	return &model.Port{
		Name:     port.Name,
		Port:     int(port.Number),
		Protocol: protocol.Parse(port.Protocol),
	}
}

type HostAddress struct {
	host    string
	address string
}

// ServiceToServiceEntry converts from internal Service representation to ServiceEntry
// This does not include endpoints - they'll be represented as EndpointSlice or EDS.
//
// See convertServices() for the reverse conversion, used by Istio to handle ServiceEntry configs.
// See kube.ConvertService for the conversion from K8S to internal Service.
func ServiceToServiceEntry(svc *model.Service, proxy *model.Proxy) *config.Config {
	gvk := gvk.ServiceEntry
	se := &networking.ServiceEntry{
		// Host is fully qualified: name, namespace, domainSuffix
		Hosts: []string{string(svc.Hostname)},

		// Internal Service and K8S Service have a single Address.
		// ServiceEntry can represent multiple - but we are not using that. SE may be merged.
		// Will be 0.0.0.0 if not specified as ClusterIP or ClusterIP==None. In such case resolution is Passthrough.
		//
		Addresses: svc.GetAddresses(proxy),

		// Location:             0,

		// Internal resolution:
		//  - Passthrough - for ClusterIP=None and no ExternalName
		//  - ClientSideLB - regular ClusterIP clusters (VIP, resolved via EDS)
		//  - DNSLB - if ExternalName is specified. Also meshExternal is set.

		WorkloadSelector: &networking.WorkloadSelector{Labels: svc.Attributes.LabelSelectors},

		// This is based on alpha.istio.io/canonical-serviceaccounts and
		//  alpha.istio.io/kubernetes-serviceaccounts.
		SubjectAltNames: svc.ServiceAccounts,
	}

	// Based on networking.istio.io/exportTo annotation
	for k, v := range svc.Attributes.ExportTo {
		if v {
			// k is Private or Public
			se.ExportTo = append(se.ExportTo, string(k))
		}
	}

	if svc.MeshExternal {
		se.Location = networking.ServiceEntry_MESH_EXTERNAL // 0 - default
	} else {
		se.Location = networking.ServiceEntry_MESH_INTERNAL
	}

	// Reverse in convertServices. Note that enum values are different
	// TODO: make the enum match, should be safe (as long as they're used as enum)
	var resolution networking.ServiceEntry_Resolution
	switch svc.Resolution {
	case model.Passthrough: // 2
		resolution = networking.ServiceEntry_NONE // 0
	case model.DNSLB: // 1
		resolution = networking.ServiceEntry_DNS // 2
	case model.DNSRoundRobinLB: // 3
		resolution = networking.ServiceEntry_DNS_ROUND_ROBIN // 3
	case model.ClientSideLB: // 0
		resolution = networking.ServiceEntry_STATIC // 1
	}
	se.Resolution = resolution

	// Port is mapped from ServicePort
	for _, p := range svc.Ports {
		se.Ports = append(se.Ports, &networking.Port{
			Number: uint32(p.Port),
			Name:   p.Name,
			// Protocol is converted to protocol.Instance - reverse conversion will use the name.
			Protocol: string(p.Protocol),
			// TODO: target port
		})
	}

	cfg := &config.Config{
		Meta: config.Meta{
			GroupVersionKind:  gvk,
			Name:              "synthetic-" + svc.Attributes.Name,
			Namespace:         svc.Attributes.Namespace,
			CreationTimestamp: svc.CreationTime,
			ResourceVersion:   svc.ResourceVersion,
		},
		Spec: se,
	}

	// TODO: WorkloadSelector

	// TODO: preserve ServiceRegistry. The reverse conversion sets it to 'external'
	// TODO: preserve UID ? It seems MCP didn't preserve it - but that code path was not used much.

	// TODO: ClusterExternalPorts map - for NodePort services, with "traffic.istio.io/nodeSelector" ann
	// It's a per-cluster map

	// TODO: ClusterExternalAddresses - for LB types, per cluster. Populated from K8S, missing
	// in SE. Used for multi-network support.
	return cfg
}

// convertServices transforms a ServiceEntry config to a list of internal Service objects.
func convertServices(cfg config.Config) []*model.Service {
	serviceEntry := cfg.Spec.(*networking.ServiceEntry)
	creationTime := cfg.CreationTimestamp

	var resolution model.Resolution
	switch serviceEntry.Resolution {
	case networking.ServiceEntry_NONE:
		resolution = model.Passthrough
	case networking.ServiceEntry_DNS:
		resolution = model.DNSLB
	case networking.ServiceEntry_DNS_ROUND_ROBIN:
		resolution = model.DNSRoundRobinLB
	case networking.ServiceEntry_STATIC:
		resolution = model.ClientSideLB
	}

	svcPorts := make(model.PortList, 0, len(serviceEntry.Ports))
	for _, port := range serviceEntry.Ports {
		svcPorts = append(svcPorts, convertPort(port))
	}

	var exportTo map[visibility.Instance]bool
	if len(serviceEntry.ExportTo) > 0 {
		exportTo = make(map[visibility.Instance]bool)
		for _, e := range serviceEntry.ExportTo {
			exportTo[visibility.Instance(e)] = true
		}
	}

	var labelSelectors map[string]string
	if serviceEntry.WorkloadSelector != nil {
		labelSelectors = serviceEntry.WorkloadSelector.Labels
	}
	hostAddresses := []*HostAddress{}
	for _, hostname := range serviceEntry.Hosts {
		if len(serviceEntry.Addresses) > 0 {
			for _, address := range serviceEntry.Addresses {
				// Check if address is an IP first because that is the most common case.
				if net.ParseIP(address) != nil {
					hostAddresses = append(hostAddresses, &HostAddress{hostname, address})
				} else if ip, network, cidrErr := net.ParseCIDR(address); cidrErr == nil {
					newAddress := address
					ones, zeroes := network.Mask.Size()
					if ones == zeroes {
						// /32 mask. Remove the /32 and make it a normal IP address
						newAddress = ip.String()
					}
					hostAddresses = append(hostAddresses, &HostAddress{hostname, newAddress})
				}
			}
		} else {
			hostAddresses = append(hostAddresses, &HostAddress{hostname, constants.UnspecifiedIP})
		}
	}

	return buildServices(hostAddresses, cfg.Name, cfg.Namespace, svcPorts, serviceEntry.Location, resolution,
		exportTo, labelSelectors, serviceEntry.SubjectAltNames, creationTime, cfg.Labels)
}

func buildServices(hostAddresses []*HostAddress, name, namespace string, ports model.PortList, location networking.ServiceEntry_Location,
	resolution model.Resolution, exportTo map[visibility.Instance]bool, selectors map[string]string, saccounts []string,
	ctime time.Time, labels map[string]string) []*model.Service {
	out := make([]*model.Service, 0, len(hostAddresses))
	lbls := labels
	if features.CanonicalServiceForMeshExternalServiceEntry && location == networking.ServiceEntry_MESH_EXTERNAL {
		lbls = ensureCanonicalServiceLabels(name, labels)
	}
	for _, ha := range hostAddresses {
		out = append(out, &model.Service{
			CreationTime:   ctime,
			MeshExternal:   location == networking.ServiceEntry_MESH_EXTERNAL,
			Hostname:       host.Name(ha.host),
			DefaultAddress: ha.address,
			Ports:          ports,
			Resolution:     resolution,
			Attributes: model.ServiceAttributes{
				ServiceRegistry: provider.External,
				Name:            ha.host,
				Namespace:       namespace,
				Labels:          lbls,
				ExportTo:        exportTo,
				LabelSelectors:  selectors,
			},
			ServiceAccounts: saccounts,
		})
	}
	return out
}

func ensureCanonicalServiceLabels(name string, srcLabels map[string]string) map[string]string {
	if srcLabels == nil {
		srcLabels = make(map[string]string)
	}
	_, svcLabelFound := srcLabels[model.IstioCanonicalServiceLabelName]
	_, revLabelFound := srcLabels[model.IstioCanonicalServiceRevisionLabelName]
	if svcLabelFound && revLabelFound {
		return srcLabels
	}

	srcLabels[model.IstioCanonicalServiceLabelName], srcLabels[model.IstioCanonicalServiceRevisionLabelName] = labels.CanonicalService(srcLabels, name)
	return srcLabels
}

func (s *Controller) convertEndpoint(service *model.Service, servicePort *networking.Port,
	wle *networking.WorkloadEntry, configKey *configKey, clusterID cluster.ID) *model.ServiceInstance {
	var instancePort uint32
	addr := wle.GetAddress()
	// priority level: unixAddress > we.ports > se.port.targetPort > se.port.number
	if strings.HasPrefix(addr, model.UnixAddressPrefix) {
		instancePort = 0
		addr = strings.TrimPrefix(addr, model.UnixAddressPrefix)
	} else if port, ok := wle.Ports[servicePort.Name]; ok && port > 0 {
		instancePort = port
	} else if servicePort.TargetPort > 0 {
		instancePort = servicePort.TargetPort
	} else {
		// final fallback is to the service port value
		instancePort = servicePort.Number
	}

	tlsMode := getTLSModeFromWorkloadEntry(wle)
	sa := ""
	if wle.ServiceAccount != "" {
		sa = spiffe.MustGenSpiffeURI(service.Attributes.Namespace, wle.ServiceAccount)
	}
	networkID := s.workloadEntryNetwork(wle)
	labels := labelutil.AugmentLabels(wle.Labels, clusterID, wle.Locality, networkID)
	return &model.ServiceInstance{
		Endpoint: &model.IstioEndpoint{
			Address:         addr,
			EndpointPort:    instancePort,
			ServicePortName: servicePort.Name,
			Network:         network.ID(wle.Network),
			Locality: model.Locality{
				Label:     wle.Locality,
				ClusterID: clusterID,
			},
			LbWeight:       wle.Weight,
			Labels:         labels,
			TLSMode:        tlsMode,
			ServiceAccount: sa,
			// Workload entry config name is used as workload name, which will appear in metric label.
			// After VM auto registry is introduced, workload group annotation should be used for workload name.
			WorkloadName: configKey.name,
			Namespace:    configKey.namespace,
		},
		Service:     service,
		ServicePort: convertPort(servicePort),
	}
}

// convertWorkloadEntryToServiceInstances translates a WorkloadEntry into ServiceInstances. This logic is largely the
// same as the ServiceEntry convertServiceEntryToInstances.
func (s *Controller) convertWorkloadEntryToServiceInstances(wle *networking.WorkloadEntry, services []*model.Service,
	se *networking.ServiceEntry, configKey *configKey, clusterID cluster.ID) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)
	for _, service := range services {
		for _, port := range se.Ports {
			out = append(out, s.convertEndpoint(service, port, wle, configKey, clusterID))
		}
	}
	return out
}

func (s *Controller) convertServiceEntryToInstances(cfg config.Config, services []*model.Service) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)
	serviceEntry := cfg.Spec.(*networking.ServiceEntry)
	if serviceEntry == nil {
		return nil
	}
	if services == nil {
		services = convertServices(cfg)
	}
	for _, service := range services {
		for _, serviceEntryPort := range serviceEntry.Ports {
			if len(serviceEntry.Endpoints) == 0 && serviceEntry.WorkloadSelector == nil &&
				(serviceEntry.Resolution == networking.ServiceEntry_DNS || serviceEntry.Resolution == networking.ServiceEntry_DNS_ROUND_ROBIN) {
				// Note: only convert the hostname to service instance if WorkloadSelector is not set
				// when service entry has discovery type DNS and no endpoints
				// we create endpoints from service's host
				// Do not use serviceentry.hosts as a service entry is converted into
				// multiple services (one for each host)
				endpointPort := serviceEntryPort.Number
				if serviceEntryPort.TargetPort > 0 {
					endpointPort = serviceEntryPort.TargetPort
				}
				out = append(out, &model.ServiceInstance{
					Endpoint: &model.IstioEndpoint{
						Address:         string(service.Hostname),
						EndpointPort:    endpointPort,
						ServicePortName: serviceEntryPort.Name,
						Labels:          nil,
						TLSMode:         model.DisabledTLSModeLabel,
					},
					Service:     service,
					ServicePort: convertPort(serviceEntryPort),
				})
			} else {
				for _, endpoint := range serviceEntry.Endpoints {
					out = append(out, s.convertEndpoint(service, serviceEntryPort, endpoint, &configKey{}, s.clusterID))
				}
			}
		}
	}
	return out
}

func getTLSModeFromWorkloadEntry(wle *networking.WorkloadEntry) string {
	// * Use security.istio.io/tlsMode if its present
	// * If not, set TLS mode if ServiceAccount is specified
	tlsMode := model.DisabledTLSModeLabel
	if val, exists := wle.Labels[label.SecurityTlsMode.Name]; exists {
		tlsMode = val
	} else if wle.ServiceAccount != "" {
		tlsMode = model.IstioMutualTLSModeLabel
	}

	return tlsMode
}

// The workload instance has pointer to the service and its service port.
// We need to create our own but we can retain the endpoint already created.
func convertWorkloadInstanceToServiceInstance(workloadInstance *model.WorkloadInstance, serviceEntryServices []*model.Service,
	serviceEntry *networking.ServiceEntry) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0)
	for _, service := range serviceEntryServices {
		for _, serviceEntryPort := range serviceEntry.Ports {
			// note: this is same as workloadentry handler
			// endpoint port will first use the port defined in wle with same port name,
			// if not port name not match, use the targetPort specified in ServiceEntry
			// if both not matched, fallback to ServiceEntry port number.
			var targetPort uint32
			if port, ok := workloadInstance.PortMap[serviceEntryPort.Name]; ok && port > 0 {
				targetPort = port
			} else if serviceEntryPort.TargetPort > 0 {
				targetPort = serviceEntryPort.TargetPort
			} else {
				targetPort = serviceEntryPort.Number
			}
			ep := *workloadInstance.Endpoint
			ep.ServicePortName = serviceEntryPort.Name
			ep.EndpointPort = targetPort
			ep.EnvoyEndpoint = nil
			out = append(out, &model.ServiceInstance{
				Endpoint:    &ep,
				Service:     service,
				ServicePort: convertPort(serviceEntryPort),
			})
		}
	}
	return out
}

// Convenience function to convert a workloadEntry into a WorkloadInstance object encoding the endpoint (without service
// port names) and the namespace - k8s will consume this workload instance when selecting workload entries
func (s *Controller) convertWorkloadEntryToWorkloadInstance(cfg config.Config, clusterID cluster.ID) *model.WorkloadInstance {
	we := convertWorkloadEntry(cfg)
	addr := we.GetAddress()
	dnsServiceEntryOnly := false
	if strings.HasPrefix(addr, model.UnixAddressPrefix) {
		// k8s can't use uds for service objects
		dnsServiceEntryOnly = true
	}
	if net.ParseIP(addr) == nil {
		// k8s can't use workloads with hostnames in the address field.
		dnsServiceEntryOnly = true
	}
	tlsMode := getTLSModeFromWorkloadEntry(we)
	sa := ""
	if we.ServiceAccount != "" {
		sa = spiffe.MustGenSpiffeURI(cfg.Namespace, we.ServiceAccount)
	}
	networkID := s.workloadEntryNetwork(we)
	labels := labelutil.AugmentLabels(we.Labels, clusterID, we.Locality, networkID)
	return &model.WorkloadInstance{
		Endpoint: &model.IstioEndpoint{
			Address: addr,
			// Not setting ports here as its done by k8s controller
			Network: network.ID(we.Network),
			Locality: model.Locality{
				Label:     we.Locality,
				ClusterID: clusterID,
			},
			LbWeight:  we.Weight,
			Namespace: cfg.Namespace,
			// Workload entry config name is used as workload name, which will appear in metric label.
			// After VM auto registry is introduced, workload group annotation should be used for workload name.
			WorkloadName:   cfg.Name,
			Labels:         labels,
			TLSMode:        tlsMode,
			ServiceAccount: sa,
		},
		PortMap:             we.Ports,
		Namespace:           cfg.Namespace,
		Name:                cfg.Name,
		Kind:                model.WorkloadEntryKind,
		DNSServiceEntryOnly: dnsServiceEntryOnly,
	}
}
