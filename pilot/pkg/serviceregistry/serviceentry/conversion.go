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
	"cmp"
	"net/netip"
	"strconv"
	"strings"

	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/serviceentry"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	labelutil "istio.io/istio/pilot/pkg/serviceregistry/util/label"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/labels"
	pm "istio.io/istio/pkg/model"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/spiffe"
	netutil "istio.io/istio/pkg/util/net"
	"istio.io/istio/pkg/util/sets"
)

func convertPort(port *networking.ServicePort) *model.Port {
	return &model.Port{
		Name:     port.Name,
		Port:     int(port.Number),
		Protocol: protocol.Parse(port.Protocol),
	}
}

type HostAddress struct {
	host           string
	address        string
	autoAssignedV4 string
	autoAssignedV6 string
}

// ServiceToServiceEntry converts from internal Service representation to ServiceEntry
// This does not include endpoints - they'll be represented as EndpointSlice or EDS.
//
// See convertServices() for the reverse conversion, used by Istio to handle ServiceEntry configs.
// See kube.ConvertService for the conversion from K8S to internal Service.
func ServiceToServiceEntry(svc *model.Service, proxy *model.Proxy) *config.Config {
	gvk := gvk.ServiceEntry

	getSvcAddresses := func(s *model.Service, node *model.Proxy) []string {
		if node.Metadata != nil && node.Metadata.ClusterID == "" {
			var addresses []string
			addressMap := s.ClusterVIPs.GetAddresses()
			for _, clusterAddresses := range addressMap {
				addresses = append(addresses, clusterAddresses...)
			}
			return addresses
		}

		return s.GetAllAddressesForProxy(proxy)
	}
	se := &networking.ServiceEntry{
		// Host is fully qualified: name, namespace, domainSuffix
		Hosts: []string{string(svc.Hostname)},

		// Internal Service and K8S Service have a single Address.
		// ServiceEntry can represent multiple - but we are not using that. SE may be merged.
		// Will be 0.0.0.0 if not specified as ClusterIP or ClusterIP==None. In such case resolution is Passthrough.
		Addresses: getSvcAddresses(svc, proxy),

		// This is based on alpha.istio.io/canonical-serviceaccounts and
		//  alpha.istio.io/kubernetes-serviceaccounts.
		SubjectAltNames: svc.ServiceAccounts,
	}

	if len(svc.Attributes.LabelSelectors) > 0 {
		se.WorkloadSelector = &networking.WorkloadSelector{Labels: svc.Attributes.LabelSelectors}
	}

	// Based on networking.istio.io/exportTo annotation
	for k := range svc.Attributes.ExportTo {
		// k is Private or Public
		se.ExportTo = append(se.ExportTo, string(k))
	}

	if svc.MeshExternal {
		se.Location = networking.ServiceEntry_MESH_EXTERNAL // 0 - default
	} else {
		se.Location = networking.ServiceEntry_MESH_INTERNAL
	}

	// Reverse in convertServices. Note that enum values are different
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
	case model.DynamicDNS:
		resolution = networking.ServiceEntry_DYNAMIC_DNS
	}
	se.Resolution = resolution

	// Port is mapped from ServicePort
	for _, p := range svc.Ports {
		se.Ports = append(se.Ports, &networking.ServicePort{
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
	// ShouldV2AutoAllocateIP already checks that there are no addresses in the spec however this is critical enough to likely be worth checking
	// explicitly as well in case the logic changes. We never want to overwrite addresses in the spec if there are any
	addresses := serviceEntry.Addresses
	addressLookup := map[string][]netip.Addr{}
	if serviceentry.ShouldV2AutoAllocateIPFromConfig(cfg) && len(addresses) == 0 {
		addressLookup = serviceentry.GetHostAddressesFromConfig(cfg)
	}

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
	case networking.ServiceEntry_DYNAMIC_DNS:
		resolution = model.DynamicDNS
	}

	trafficDistribution := model.GetTrafficDistribution(nil, cfg.Annotations)

	svcPorts := make(model.PortList, 0, len(serviceEntry.Ports))
	var portOverrides map[uint32]uint32
	for _, port := range serviceEntry.Ports {
		svcPorts = append(svcPorts, convertPort(port))
		if resolution == model.Passthrough && port.TargetPort != 0 {
			if portOverrides == nil {
				portOverrides = map[uint32]uint32{}
			}
			portOverrides[port.Number] = port.TargetPort
		}
	}

	var exportTo sets.Set[visibility.Instance]
	if len(serviceEntry.ExportTo) > 0 {
		exportTo = sets.NewWithLength[visibility.Instance](len(serviceEntry.ExportTo))
		for _, e := range serviceEntry.ExportTo {
			exportTo.Insert(visibility.Instance(e))
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
				if netutil.IsValidIPAddress(address) {
					hostAddresses = append(hostAddresses, &HostAddress{hostname, address, "", ""})
				} else if cidr, cidrErr := netip.ParsePrefix(address); cidrErr == nil {
					newAddress := address
					if cidr.Bits() == cidr.Addr().BitLen() {
						// /32 mask. Remove the /32 and make it a normal IP address
						newAddress = cidr.Addr().String()
					}
					hostAddresses = append(hostAddresses, &HostAddress{hostname, newAddress, "", ""})
				}
			}
		} else {
			var v4, v6 string
			if autoAddresses, ok := addressLookup[hostname]; ok {
				for _, aa := range autoAddresses {
					if aa.Is4() {
						v4 = aa.String()
					}
					if aa.Is6() {
						v6 = aa.String()
					}
				}
			}
			hostAddresses = append(hostAddresses, &HostAddress{hostname, constants.UnspecifiedIP, v4, v6})
		}
	}

	out := make([]*model.Service, 0, len(hostAddresses))
	lbls := cfg.Labels
	if features.CanonicalServiceForMeshExternalServiceEntry && serviceEntry.Location == networking.ServiceEntry_MESH_EXTERNAL {
		lbls = ensureCanonicalServiceLabels(cfg.Name, cfg.Labels)
	}
	for _, ha := range hostAddresses {
		svc := &model.Service{
			CreationTime:   creationTime,
			MeshExternal:   serviceEntry.Location == networking.ServiceEntry_MESH_EXTERNAL,
			Hostname:       host.Name(ha.host),
			DefaultAddress: ha.address,
			Ports:          svcPorts,
			Resolution:     resolution,
			Attributes: model.ServiceAttributes{
				ServiceRegistry:        provider.External,
				PassthroughTargetPorts: portOverrides,
				Name:                   ha.host,
				Namespace:              cfg.Namespace,
				Labels:                 lbls,
				ExportTo:               exportTo,
				LabelSelectors:         labelSelectors,
				K8sAttributes:          model.K8sAttributes{ObjectName: cfg.Name, TrafficDistribution: trafficDistribution},
			},
			ServiceAccounts: serviceEntry.SubjectAltNames,
		}
		if ha.autoAssignedV4 != "" {
			svc.AutoAllocatedIPv4Address = ha.autoAssignedV4
		}
		if ha.autoAssignedV6 != "" {
			svc.AutoAllocatedIPv6Address = ha.autoAssignedV6
		}
		out = append(out, svc)
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

func convertServiceEntryToInstances(
	ctx krt.HandlerContext,
	cfg config.Config,
	service *model.Service,
	meshConfig krt.Collection[meshwatcher.MeshConfigResource],
	clusterID cluster.ID,
	networkIDFn networkIDCallback,
) []*model.ServiceInstance {
	serviceEntry := cfg.Spec.(*networking.ServiceEntry)
	if serviceEntry == nil {
		return nil
	}

	endpointsNum := len(serviceEntry.Endpoints)
	hostnameToServiceInstance := false
	if len(serviceEntry.Endpoints) == 0 && serviceEntry.WorkloadSelector == nil && isDNSTypeService(service) {
		hostnameToServiceInstance = true
		endpointsNum = 1
	}

	out := make([]*model.ServiceInstance, 0, len(serviceEntry.Ports)*endpointsNum)
	if hostnameToServiceInstance {
		for _, serviceEntryPort := range serviceEntry.Ports {
			// Note: only convert the hostname to service instance if WorkloadSelector is not set
			// when service entry has discovery type DNS and no endpoints.
			// We create endpoints from service's host, do not use serviceentry.hosts
			// as a service entry is converted into multiple services (one for each host)
			endpointPort := serviceEntryPort.Number
			if serviceEntryPort.TargetPort > 0 {
				endpointPort = serviceEntryPort.TargetPort
			}
			out = append(out, &model.ServiceInstance{
				Endpoint: &model.IstioEndpoint{
					Addresses:            []string{string(service.Hostname)},
					EndpointPort:         endpointPort,
					ServicePortName:      serviceEntryPort.Name,
					LegacyClusterPortKey: int(serviceEntryPort.Number),
					Labels:               nil,
					TLSMode:              model.DisabledTLSModeLabel,
					Locality: model.Locality{
						ClusterID: clusterID,
					},
					Namespace:    cfg.Namespace,
					WorkloadName: cfg.Name,
				},
				Service:     service,
				ServicePort: convertPort(serviceEntryPort),
			})
		}
	} else {
		for i, endpoint := range serviceEntry.Endpoints {
			// uniquely identify the endpoint by serviceentry namespace, name and index
			meta := config.Meta{
				Namespace: cfg.Namespace,
				Name:      cfg.Name + "-" + strconv.Itoa(i),
			}
			wli := convertWorkloadEntryToWorkloadInstance(ctx, endpoint, meta, meshConfig, cfg.Namespace, clusterID, networkIDFn)
			out = append(out, convertWorkloadInstanceToServiceInstance(wli, service, serviceEntry.Ports)...)
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
func convertWorkloadInstanceToServiceInstance(workloadInstance *model.WorkloadInstance, service *model.Service,
	serviceEntryPorts []*networking.ServicePort,
) []*model.ServiceInstance {
	out := make([]*model.ServiceInstance, 0, len(serviceEntryPorts))
	for _, serviceEntryPort := range serviceEntryPorts {
		var targetPort uint32
		addrs := workloadInstance.Endpoint.Addresses
		// priority level: unixAddress > we.ports > se.port.targetPort > se.port.number
		// unix addresses can only happen on workload entries which have only one address
		if len(workloadInstance.Endpoint.Addresses) == 1 && strings.HasPrefix(workloadInstance.Endpoint.Addresses[0], model.UnixAddressPrefix) {
			addrs = []string{strings.TrimPrefix(workloadInstance.Endpoint.Addresses[0], model.UnixAddressPrefix)}
			targetPort = 0
		} else if port, ok := workloadInstance.PortMap[serviceEntryPort.Name]; ok && port > 0 {
			targetPort = port
		} else if serviceEntryPort.TargetPort > 0 {
			targetPort = serviceEntryPort.TargetPort
		} else {
			targetPort = serviceEntryPort.Number
		}
		ep := workloadInstance.Endpoint.ShallowCopy()
		ep.ServicePortName = serviceEntryPort.Name
		ep.LegacyClusterPortKey = int(serviceEntryPort.Number)
		ep.Addresses = addrs
		ep.EndpointPort = targetPort
		if ep.Namespace == "" {
			ep.Namespace = workloadInstance.Namespace
		}
		if ep.WorkloadName == "" {
			ep.WorkloadName = workloadInstance.Name
		}

		out = append(out, &model.ServiceInstance{
			Endpoint:    ep,
			Service:     service,
			ServicePort: convertPort(serviceEntryPort),
		})
	}
	return out
}

// Convenience function to convert a workloadEntry into a WorkloadInstance object encoding the endpoint (without service
// port names) and the namespace - k8s will consume this workload instance when selecting workload entries
func convertWorkloadEntryToWorkloadInstance(
	ctx krt.HandlerContext,
	we *networking.WorkloadEntry,
	meta config.Meta,
	meshConfig krt.Collection[meshwatcher.MeshConfigResource],
	spiffeNamespace string,
	clusterID cluster.ID,
	networkIDFn networkIDCallback,
) *model.WorkloadInstance {
	addr := we.GetAddress()
	dnsServiceEntryOnly := false
	if strings.HasPrefix(addr, model.UnixAddressPrefix) {
		// k8s can't use uds for service objects
		dnsServiceEntryOnly = true
	} else if addr != "" && !netutil.IsValidIPAddress(addr) {
		// k8s can't use workloads with hostnames in the address field.
		dnsServiceEntryOnly = true
	}
	tlsMode := getTLSModeFromWorkloadEntry(we)
	sa := ""
	if we.ServiceAccount != "" {
		mesh := krt.FetchOne(ctx, meshConfig)
		sa = spiffe.MustGenSpiffeURI(mesh.MeshConfig, spiffeNamespace, we.ServiceAccount)
	}
	networkID := workloadEntryNetwork(we, networkIDFn)
	locality := we.Locality
	localityLabel := pm.GetLocalityLabel(we.Labels)
	if locality == "" && localityLabel != "" {
		locality = pm.SanitizeLocalityLabel(localityLabel)
	}
	lbls := labelutil.AugmentLabels(we.Labels, clusterID, locality, "", networkID)
	return &model.WorkloadInstance{
		Endpoint: &model.IstioEndpoint{
			Addresses: []string{addr},
			// Not setting ports here as its done by k8s controller
			Network: network.ID(we.Network),
			Locality: model.Locality{
				Label:     locality,
				ClusterID: clusterID,
			},
			LbWeight:  we.Weight,
			Namespace: meta.Namespace,
			// Workload entry config name is used as workload name, which will appear in metric label.
			// After VM auto registry is introduced, workload group annotation should be used for workload name.
			WorkloadName:   labels.WorkloadNameFromWorkloadEntry(meta.Name, meta.Annotations, meta.Labels),
			Labels:         lbls,
			TLSMode:        tlsMode,
			ServiceAccount: sa,
		},
		PortMap:             we.Ports,
		Namespace:           meta.Namespace,
		Name:                meta.Name,
		Kind:                model.WorkloadEntryKind,
		DNSServiceEntryOnly: dnsServiceEntryOnly,
	}
}

// Services derived from ServiceEntry configs
func services(
	serviceEntries krt.Collection[config.Config],
	meshConfig krt.Collection[meshwatcher.MeshConfigResource],
	workloads krt.Collection[*model.WorkloadInstance],
	workloadsByNamespace krt.Index[string, *model.WorkloadInstance],
	clusterID cluster.ID,
	networkIDFn networkIDCallback,
	opts krt.OptionsBuilder,
) (krt.Collection[ServiceWithInstances], krt.Index[string, ServiceWithInstances], krt.Index[string, ServiceWithInstances]) {
	collection := krt.NewManyCollection(serviceEntries, func(ctx krt.HandlerContext, cfg config.Config) []ServiceWithInstances {
		se := cfg.Spec.(*networking.ServiceEntry)
		services := convertServices(cfg)
		if se.WorkloadSelector == nil {
			// No selector: endpoints from SE directly
			return slices.Map(services, func(ss *model.Service) ServiceWithInstances {
				return ServiceWithInstances{
					Service: ss,
					TargetPorts: slices.Map(se.Ports, func(p *networking.ServicePort) uint32 {
						return p.TargetPort
					}),
					Instances: convertServiceEntryToInstances(ctx, cfg, ss, meshConfig, clusterID, networkIDFn),
				}
			})
		}

		dnsService := isDNSTypeService(services[0])
		selectedWorkloads := krt.Fetch(
			ctx,
			workloads,
			krt.FilterIndex(workloadsByNamespace, cfg.Namespace),
			krt.FilterLabel(se.WorkloadSelector.Labels),
			krt.FilterGeneric(func(o any) bool {
				wi := o.(*model.WorkloadInstance)
				if wi.DNSServiceEntryOnly && !dnsService {
					return false
				}
				return true
			}),
		)
		// krt fetching does not guarantee order, so we need to sort the selected workloads to ensure determinism
		slices.SortStableFunc(selectedWorkloads, func(a, b *model.WorkloadInstance) int {
			if r := cmp.Compare(a.Kind, b.Kind); r != 0 {
				return r
			}
			if r := cmp.Compare(a.Namespace, b.Namespace); r != 0 {
				return r
			}
			return cmp.Compare(a.Name, b.Name)
		})

		res := make([]ServiceWithInstances, 0, len(services))
		for _, service := range services {
			swi := ServiceWithInstances{
				Service: service,
				TargetPorts: slices.Map(se.Ports, func(p *networking.ServicePort) uint32 {
					return p.TargetPort
				}),
				Instances: make([]*model.ServiceInstance, 0, len(selectedWorkloads)*len(se.Ports)),
			}
			for _, wi := range selectedWorkloads {
				swi.Instances = append(swi.Instances, convertWorkloadInstanceToServiceInstance(wi, service, se.Ports)...)
			}
			res = append(res, swi)
		}
		return res
	}, opts.WithName("outputs/Services")...)

	nsHostIndex := krt.NewIndex(collection, "byNsHost", func(swi ServiceWithInstances) []string {
		return []string{swi.Service.Attributes.Namespace + "/" + swi.Service.Hostname.String()}
	})

	hostIndex := krt.NewIndex(collection, "byHost", func(ss ServiceWithInstances) []string {
		key := ss.Service.Hostname.String()
		return []string{key}
	})

	return collection, nsHostIndex, hostIndex
}

// Merge services with the same namespace and hostname into a single service instance with all the instances.
// Also filters multiple DNS round robin service instances with the same host and port.
func mergeServicesByNamespaceHost(
	servicesByNsHost krt.Collection[krt.IndexObject[string, ServiceWithInstances]],
	opts krt.OptionsBuilder,
) krt.Collection[InstancesByNamespaceHost] {
	return krt.NewCollection(servicesByNsHost, func(ctx krt.HandlerContext, s krt.IndexObject[string, ServiceWithInstances]) *InstancesByNamespaceHost {
		namespace, hostname, _ := strings.Cut(s.Key, "/")
		sortServicesByCreationTime(s.Objects)

		ports := sets.New[int]()
		instances := make([]*model.ServiceInstance, 0)
		anyDNSServiceEndpoint := false
		for _, swi := range s.Objects {
			for _, si := range swi.Instances {
				if si.Service.Resolution == model.DNSRoundRobinLB {
					if ports.Contains(si.ServicePort.Port) {
						log.Debugf("skipping service %s from service entry %s with DnsRoundRobinLB. A service entry with the same host "+
							"already exists. Only one locality lb end point is allowed for DnsRoundRobinLB services.",
							si.Service.Hostname, si.Service.Attributes.Name+"/"+si.Service.Attributes.Namespace)
						continue
					}
				}
				instances = append(instances, si)
				ports.Insert(si.ServicePort.Port)
				if isDNSTypeService(si.Service) {
					anyDNSServiceEndpoint = true
				}
			}
		}

		return &InstancesByNamespaceHost{
			Namespace:             namespace,
			Hostname:              hostname,
			Instances:             instances,
			HasDNSServiceEndpoint: anyDNSServiceEndpoint,
		}
	}, opts.WithName("outputs/ServiceInstancesByNamespaceHost")...)
}

// return the mesh network for the workload entry. Empty string if not found.
func workloadEntryNetwork(wle *networking.WorkloadEntry, networkIDFn networkIDCallback) network.ID {
	// 1. first check the wle.Network
	if wle.Network != "" {
		return network.ID(wle.Network)
	}

	// 2. fall back to the passed in getNetworkCb func.
	if networkIDFn != nil {
		return networkIDFn(wle.Address, wle.Labels)
	}
	return ""
}

func isDNSTypeService(se *model.Service) bool {
	if se == nil {
		return false
	}
	return se.Resolution == model.DNSLB || se.Resolution == model.DNSRoundRobinLB
}
