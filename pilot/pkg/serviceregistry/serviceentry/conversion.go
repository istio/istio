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
	"net/netip"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/api/annotation"
	"istio.io/api/label"
	networking "istio.io/api/networking/v1alpha3"
	clientnetworking "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/status"
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
	"istio.io/istio/pkg/maps"
	pm "istio.io/istio/pkg/model"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/spiffe"
	netutil "istio.io/istio/pkg/util/net"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
)

func convertPort(port *networking.ServicePort) *model.Port {
	return &model.Port{
		Name:       port.Name,
		Port:       int(port.Number),
		TargetPort: intstr.FromInt32(int32(port.TargetPort)),
		Protocol:   protocol.Parse(port.Protocol),
	}
}

type hostAddress struct {
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

// ConvertClientWorkloadEntry merges the metadata.labels and spec.labels
func ConvertClientWorkloadEntry(cfg *clientnetworking.WorkloadEntry) *clientnetworking.WorkloadEntry {
	if cfg.Spec.Labels == nil {
		// Short circuit, we don't have to do any conversion
		return cfg
	}
	cfg = cfg.DeepCopy()
	// Set both fields to be the merged result, so either can be used
	cfg.Spec.Labels = maps.MergeCopy(cfg.Spec.Labels, cfg.Labels)
	cfg.Labels = cfg.Spec.Labels

	return cfg
}

// ConvertWorkloadEntry convert wle from Config.Spec and populate the metadata labels into it.
func ConvertWorkloadEntry(cfg config.Config) *networking.WorkloadEntry {
	wle := cfg.Spec.(*networking.WorkloadEntry)
	if wle == nil {
		return nil
	}

	// we will merge labels from metadata with spec, with precedence to the metadata
	labels := maps.MergeCopy(wle.Labels, cfg.Labels)
	// shallow copy
	copied := protomarshal.ShallowClone(wle)
	copied.Labels = labels
	return copied
}

// convertServices transforms a ServiceEntry config to a list of internal Service objects.
// nsAnnotations are the namespace annotations for traffic distribution inheritance.
func convertServices(cfg config.Config, nsAnnotations map[string]string, canonicalServiceForMeshExternal bool) []*model.Service {
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

	trafficDistribution := model.GetTrafficDistribution(nil, cfg.Annotations, nsAnnotations)
	dnsConnectStrategy := model.GetDNSConnectStrategy(cfg.Annotations)

	svcPorts := make(model.PortList, 0, len(serviceEntry.Ports))
	for _, port := range serviceEntry.Ports {
		svcPorts = append(svcPorts, convertPort(port))
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
	hostAddresses := []*hostAddress{}
	for _, hostname := range serviceEntry.Hosts {
		if len(serviceEntry.Addresses) > 0 {
			for _, address := range serviceEntry.Addresses {
				// Check if address is an IP first because that is the most common case.
				if netutil.IsValidIPAddress(address) {
					hostAddresses = append(hostAddresses, &hostAddress{hostname, address, "", ""})
				} else if cidr, cidrErr := netip.ParsePrefix(address); cidrErr == nil {
					newAddress := address
					if cidr.Bits() == cidr.Addr().BitLen() {
						// /32 mask. Remove the /32 and make it a normal IP address
						newAddress = cidr.Addr().String()
					}
					hostAddresses = append(hostAddresses, &hostAddress{hostname, newAddress, "", ""})
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
			hostAddresses = append(hostAddresses, &hostAddress{hostname, constants.UnspecifiedIP, v4, v6})
		}
	}

	out := make([]*model.Service, 0, len(hostAddresses))
	labels := cfg.Labels
	if canonicalServiceForMeshExternal && serviceEntry.Location == networking.ServiceEntry_MESH_EXTERNAL {
		labels = ensureCanonicalServiceLabels(cfg.Name, cfg.Labels)
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
				ServiceRegistry: provider.External,
				Name:            ha.host,
				Namespace:       cfg.Namespace,
				Labels:          labels,
				ExportTo:        exportTo,
				LabelSelectors:  labelSelectors,
				K8sAttributes:   model.K8sAttributes{ObjectName: cfg.Name, TrafficDistribution: trafficDistribution, DNSConnectStrategy: dnsConnectStrategy},
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
) []*WorkloadServiceInstance {
	serviceEntry := cfg.Spec.(*networking.ServiceEntry)
	endpointsNum := len(serviceEntry.Endpoints)
	hostnameToServiceInstance := false
	if len(serviceEntry.Endpoints) == 0 && serviceEntry.WorkloadSelector == nil && isDNSTypeService(service) {
		hostnameToServiceInstance = true
		endpointsNum = 1
	}

	out := make([]*WorkloadServiceInstance, 0, len(serviceEntry.Ports)*endpointsNum)
	if hostnameToServiceInstance {
		// The endpoint comes from the ServiceEntry itself, like inline endpoints, which are WorkloadEntry kind.
		uidPrefix := workloadServiceInstanceUIDPrefix(model.WorkloadEntryKind.String(), cfg.Namespace, cfg.Name, service)
		for _, servicePort := range service.Ports {
			// Note: only convert the hostname to service instance if WorkloadSelector is not set
			// when service entry has discovery type DNS and no endpoints.
			// We create endpoints from service's host, do not use serviceentry.hosts
			// as a service entry is converted into multiple services (one for each host)
			endpointPort := servicePortTargetPort(servicePort)
			instance := &WorkloadServiceInstance{
				UID:       workloadServiceInstanceUID(uidPrefix, servicePort),
				Namespace: cfg.Namespace,
				Name:      cfg.Name,
				Endpoint: &model.IstioEndpoint{
					Addresses:            []string{string(service.Hostname)},
					EndpointPort:         endpointPort,
					ServicePortName:      servicePort.Name,
					LegacyClusterPortKey: servicePort.Port,
					Labels:               nil,
					TLSMode:              model.DisabledTLSModeLabel,
					Locality: model.Locality{
						ClusterID: clusterID,
					},
					Namespace:    cfg.Namespace,
					WorkloadName: cfg.Name,
				},
				Service:     service,
				ServicePort: servicePort,
			}
			out = append(out, instance)
		}
	} else {
		for i, endpoint := range serviceEntry.Endpoints {
			// uniquely identify the endpoint by serviceentry namespace, name and index
			meta := config.Meta{
				Namespace: cfg.Namespace,
				Name:      cfg.Name + "-" + strconv.Itoa(i),
			}
			wli := convertWorkloadEntryToWorkloadInstance(ctx, endpoint, meta, meshConfig, cfg.Namespace, clusterID, networkIDFn)
			out = appendWorkloadInstanceToInstances(out, wli, service)
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

// The workload instance has no service or service-port association, so create one instance per service port.
func convertWorkloadInstanceToInstances(workloadInstance *model.WorkloadInstance, service *model.Service) []*WorkloadServiceInstance {
	out := make([]*WorkloadServiceInstance, 0, len(service.Ports))
	return appendWorkloadInstanceToInstances(out, workloadInstance, service)
}

// appendWorkloadInstanceToInstances appends one instance per service port to out.
func appendWorkloadInstanceToInstances(
	out []*WorkloadServiceInstance,
	workloadInstance *model.WorkloadInstance,
	service *model.Service,
) []*WorkloadServiceInstance {
	// unix addresses can only happen on workload entries which have only one address
	addrs := workloadInstance.Endpoint.Addresses
	unixAddress := len(addrs) == 1 && strings.HasPrefix(addrs[0], model.UnixAddressPrefix)
	if unixAddress {
		addrs = []string{strings.TrimPrefix(addrs[0], model.UnixAddressPrefix)}
	}
	uidPrefix := workloadServiceInstanceUIDPrefix(workloadInstance.Kind.String(), workloadInstance.Namespace, workloadInstance.Name, service)
	for _, servicePort := range service.Ports {
		var targetPort uint32
		// priority level: unixAddress > we.ports > se.port.targetPort > se.port.number
		if unixAddress {
			targetPort = 0
		} else if port, ok := workloadInstance.PortMap[servicePort.Name]; ok && port > 0 {
			targetPort = port
		} else {
			targetPort = servicePortTargetPort(servicePort)
		}
		ep := workloadInstance.Endpoint.ShallowCopy()
		ep.ServicePortName = servicePort.Name
		ep.LegacyClusterPortKey = servicePort.Port
		ep.Addresses = addrs
		ep.EndpointPort = targetPort
		if ep.Namespace == "" {
			ep.Namespace = workloadInstance.Namespace
		}
		if ep.WorkloadName == "" {
			ep.WorkloadName = workloadInstance.Name
		}

		instance := &WorkloadServiceInstance{
			UID:         workloadServiceInstanceUID(uidPrefix, servicePort),
			Namespace:   workloadInstance.Namespace,
			Name:        workloadInstance.Name,
			Endpoint:    ep,
			Service:     service,
			ServicePort: servicePort,
		}
		out = append(out, instance)
	}
	return out
}

// workloadServiceInstanceUIDPrefix returns the part of an instance UID shared by all ports of a workload
// for a given service. The workload kind leads so that, after the service, merged instances order Pods
// before WorkloadEntries; DNS round robin services keep only the first instance per port, so this
// decides which one wins.
// The endpoint is deliberately left out: a workload yields exactly one endpoint per service port, so it
// adds nothing to uniqueness, and keeping the address out makes an address change an update of the
// same instance rather than a delete and add.
func workloadServiceInstanceUIDPrefix(kind, namespace, name string, service *model.Service) string {
	return kind + "/" + namespace + "/" + name + "/" + service.ResourceName() + "/"
}

func workloadServiceInstanceUID(prefix string, servicePort *model.Port) string {
	return prefix + strconv.Itoa(servicePort.Port)
}

func servicePortTargetPort(port *model.Port) uint32 {
	if port.TargetPort.Type == intstr.Int && port.TargetPort.IntVal > 0 {
		return uint32(port.TargetPort.IntVal)
	}
	return uint32(port.Port)
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
	capturedByZtunnel := meta.Annotations[annotation.AmbientRedirection.Name] == constants.AmbientRedirectionEnabled
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
			WorkloadName:      labels.WorkloadNameFromWorkloadEntry(meta.Name, meta.Annotations, meta.Labels),
			Labels:            lbls,
			TLSMode:           tlsMode,
			ServiceAccount:    sa,
			CapturedByZtunnel: capturedByZtunnel,
		},
		PortMap:             we.Ports,
		Namespace:           meta.Namespace,
		Name:                meta.Name,
		Kind:                model.WorkloadEntryKind,
		DNSServiceEntryOnly: dnsServiceEntryOnly,
	}
}

func services(
	serviceEntries krt.Collection[config.Config],
	serviceEntryVisibility krt.Singleton[model.ServiceEntryVisibilityMatcher],
	meshConfig krt.Collection[meshwatcher.MeshConfigResource],
	namespaces krt.Collection[*v1.Namespace],
	clusterID cluster.ID,
	networkIDFn networkIDCallback,
	canonicalServiceForMeshExternal bool,
	opts krt.OptionsBuilder,
) krt.Collection[*ServiceWithInstances] {
	return krt.NewManyCollection(serviceEntries, func(ctx krt.HandlerContext, cfg config.Config) []*ServiceWithInstances {
		se := cfg.Spec.(*networking.ServiceEntry)
		namespace := krt.FetchOne(ctx, namespaces, krt.FilterKey(cfg.Namespace))
		var namespaceAnnotations map[string]string
		var namespaceLabels map[string]string
		if namespace != nil {
			namespaceAnnotations = (*namespace).Annotations
			namespaceLabels = (*namespace).Labels
		}

		services := convertServices(cfg, namespaceAnnotations, canonicalServiceForMeshExternal)
		// Resolve the ServiceEntry's visibility from the precompiled serviceEntryVisibility matcher so
		// classic (sidecar) exportTo can be capped by it (see PushContext.serviceExportTo). The default
		// (feature unset) resolves to Public, leaving services at their zero value.
		if vis := krt.FetchOne(ctx, serviceEntryVisibility.AsCollection()); vis != nil {
			resolved := vis.VisibilityFor(namespaceLabels)
			for _, svc := range services {
				svc.Attributes.Visibility = resolved
			}
		}

		if se.WorkloadSelector != nil {
			return slices.Map(services, func(ss *model.Service) *ServiceWithInstances {
				return &ServiceWithInstances{
					Service: ss,
				}
			})
		}

		// No selector: endpoints from SE directly
		return slices.Map(services, func(ss *model.Service) *ServiceWithInstances {
			return &ServiceWithInstances{
				Service:   ss,
				Instances: convertServiceEntryToInstances(ctx, cfg, ss, meshConfig, clusterID, networkIDFn),
			}
		})
	}, opts.WithName("outputs/ServicesWithInstances")...)
}

func serviceInstances(
	allWorkloads krt.Collection[*model.WorkloadInstance],
	servicesByNamespace krt.Index[string, *model.Service],
	opts krt.OptionsBuilder,
) krt.Collection[*WorkloadServiceInstance] {
	return krt.NewManyCollection(allWorkloads, func(ctx krt.HandlerContext, wi *model.WorkloadInstance) []*WorkloadServiceInstance {
		filters := []krt.FetchOption{
			krt.FilterSelectsNonEmpty(wi.GetLabels()),
		}
		if wi.DNSServiceEntryOnly {
			filters = append(filters, krt.FilterGeneric(func(o any) bool {
				return isDNSTypeService(o.(*model.Service))
			}))
		}

		selectedServices := servicesByNamespace.Fetch(
			ctx,
			wi.Namespace,
			filters...,
		)

		if len(selectedServices) == 0 {
			return nil
		}
		if len(selectedServices) == 1 {
			// Common case: a workload is selected by a single service, and
			// convertWorkloadInstanceToInstances already returns an exactly-sized slice.
			return convertWorkloadInstanceToInstances(wi, selectedServices[0])
		}

		// One instance per service port.
		n := 0
		for _, s := range selectedServices {
			n += len(s.Ports)
		}
		res := make([]*WorkloadServiceInstance, 0, n)
		for _, s := range selectedServices {
			res = appendWorkloadInstanceToInstances(res, wi, s)
		}
		return res
	}, opts.WithName("outputs/WorkloadServiceInstances")...)
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

// isHealthy checks that the provided WorkloadEntry is healthy. If health checks are not enabled,
// it is assumed to always be healthy
func isHealthy(cfg config.Config) bool {
	if parseHealthAnnotation(cfg.Annotations[status.WorkloadEntryHealthCheckAnnotation]) {
		// We default to false if the condition is not set. This ensures newly created WorkloadEntries
		// are treated as unhealthy until we prove they are healthy by probe success.
		return status.GetBoolConditionFromSpec(cfg, status.ConditionHealthy, false)
	}
	// If health check is not enabled, assume its healthy
	return true
}

func parseHealthAnnotation(s string) bool {
	if s == "" {
		return false
	}
	p, err := strconv.ParseBool(s)
	if err != nil {
		return false
	}
	return p
}
