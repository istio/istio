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
	"sort"
	"strings"

	coreV1 "k8s.io/api/core/v1"

	"istio.io/api/annotation"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/kube"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/spiffe"
)

const (
	// IngressClassAnnotation is the annotation on ingress resources for the class of controllers
	// responsible for it
	IngressClassAnnotation = "kubernetes.io/ingress.class"

	// TODO: move to API
	// The value for this annotation is a set of key value pairs (node labels)
	// that can be used to select a subset of nodes from the pool of k8s nodes
	// It is used for multi-cluster scenario, and with nodePort type gateway service.
	NodeSelectorAnnotation = "traffic.istio.io/nodeSelector"
)

func convertPort(port coreV1.ServicePort) *model.Port {
	return &model.Port{
		Name:     port.Name,
		Port:     int(port.Port),
		Protocol: kube.ConvertProtocol(port.Port, port.Name, port.Protocol, port.AppProtocol),
	}
}

func ConvertService(svc coreV1.Service, domainSuffix string, clusterID string) *model.Service {
	addr, external := constants.UnspecifiedIP, ""
	if svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != coreV1.ClusterIPNone {
		addr = svc.Spec.ClusterIP
	}

	resolution := model.ClientSideLB
	meshExternal := false

	if svc.Spec.Type == coreV1.ServiceTypeExternalName && svc.Spec.ExternalName != "" {
		external = svc.Spec.ExternalName
		resolution = model.DNSLB
		meshExternal = true
	}

	if addr == constants.UnspecifiedIP && external == "" { // headless services should not be load balanced
		resolution = model.Passthrough
	}

	var labelSelectors map[string]string
	if svc.Spec.ClusterIP != coreV1.ClusterIPNone && svc.Spec.Type != coreV1.ServiceTypeExternalName {
		labelSelectors = svc.Spec.Selector
	}

	ports := make([]*model.Port, 0, len(svc.Spec.Ports))
	for _, port := range svc.Spec.Ports {
		ports = append(ports, convertPort(port))
	}

	var exportTo map[visibility.Instance]bool
	serviceaccounts := make([]string, 0)
	if svc.Annotations[annotation.AlphaCanonicalServiceAccounts.Name] != "" {
		serviceaccounts = append(serviceaccounts, strings.Split(svc.Annotations[annotation.AlphaCanonicalServiceAccounts.Name], ",")...)
	}
	if svc.Annotations[annotation.AlphaKubernetesServiceAccounts.Name] != "" {
		for _, ksa := range strings.Split(svc.Annotations[annotation.AlphaKubernetesServiceAccounts.Name], ",") {
			serviceaccounts = append(serviceaccounts, kubeToIstioServiceAccount(ksa, svc.Namespace))
		}
	}
	if svc.Annotations[annotation.NetworkingExportTo.Name] != "" {
		exportTo = make(map[visibility.Instance]bool)
		for _, e := range strings.Split(svc.Annotations[annotation.NetworkingExportTo.Name], ",") {
			exportTo[visibility.Instance(e)] = true
		}
	}
	sort.Strings(serviceaccounts)

	istioService := &model.Service{
		Hostname:        ServiceHostname(svc.Name, svc.Namespace, domainSuffix),
		Ports:           ports,
		Address:         addr,
		ServiceAccounts: serviceaccounts,
		MeshExternal:    meshExternal,
		Resolution:      resolution,
		CreationTime:    svc.CreationTimestamp.Time,
		Attributes: model.ServiceAttributes{
			ServiceRegistry: string(serviceregistry.Kubernetes),
			Name:            svc.Name,
			Namespace:       svc.Namespace,
			UID:             formatUID(svc.Namespace, svc.Name),
			ExportTo:        exportTo,
			LabelSelectors:  labelSelectors,
		},
	}

	switch svc.Spec.Type {
	case coreV1.ServiceTypeNodePort:
		if _, ok := svc.Annotations[NodeSelectorAnnotation]; !ok {
			// only do this for istio ingress-gateway services
			break
		}
		// store the service port to node port mappings
		portMap := make(map[uint32]uint32)
		for _, p := range svc.Spec.Ports {
			portMap[uint32(p.Port)] = uint32(p.NodePort)
		}
		istioService.Attributes.ClusterExternalPorts = map[string]map[uint32]uint32{clusterID: portMap}
		// address mappings will be done elsewhere
	case coreV1.ServiceTypeLoadBalancer:
		if len(svc.Status.LoadBalancer.Ingress) > 0 {
			var lbAddrs []string
			for _, ingress := range svc.Status.LoadBalancer.Ingress {
				if len(ingress.IP) > 0 {
					lbAddrs = append(lbAddrs, ingress.IP)
				} else if len(ingress.Hostname) > 0 {
					// DO NOT resolve the DNS here. In environments like AWS, the ELB hostname
					// does not have a repeatable DNS address and IPs resolved at an earlier point
					// in time may not work. So, when we get just hostnames instead of IPs, we need
					// to smartly switch from EDS to strict_dns rather than doing the naive thing of
					// resolving the DNS name and hoping the resolution is one-time task.
					lbAddrs = append(lbAddrs, ingress.Hostname)
				}
			}
			if len(lbAddrs) > 0 {
				istioService.Attributes.ClusterExternalAddresses = map[string][]string{clusterID: lbAddrs}
			}
		}
	}

	return istioService
}

func ExternalNameServiceInstances(k8sSvc *coreV1.Service, svc *model.Service) []*model.ServiceInstance {
	if k8sSvc.Spec.Type != coreV1.ServiceTypeExternalName || k8sSvc.Spec.ExternalName == "" {
		return nil
	}
	out := make([]*model.ServiceInstance, 0, len(svc.Ports))
	for _, portEntry := range svc.Ports {
		out = append(out, &model.ServiceInstance{
			Service:     svc,
			ServicePort: portEntry,
			Endpoint: &model.IstioEndpoint{
				Address:         k8sSvc.Spec.ExternalName,
				EndpointPort:    uint32(portEntry.Port),
				ServicePortName: portEntry.Name,
				Labels:          k8sSvc.Labels,
			},
		})
	}
	return out
}

// ServiceHostname produces FQDN for a k8s service
func ServiceHostname(name, namespace, domainSuffix string) host.Name {
	return host.Name(name + "." + namespace + "." + "svc" + "." + domainSuffix) // Format: "%s.%s.svc.%s"
}

// kubeToIstioServiceAccount converts a K8s service account to an Istio service account
func kubeToIstioServiceAccount(saname string, ns string) string {
	return spiffe.MustGenSpiffeURI(ns, saname)
}

// SecureNamingSAN creates the secure naming used for SAN verification from pod metadata
func SecureNamingSAN(pod *coreV1.Pod) string {

	//use the identity annotation
	if identity, exist := pod.Annotations[annotation.AlphaIdentity.Name]; exist {
		return spiffe.GenCustomSpiffe(identity)
	}

	return spiffe.MustGenSpiffeURI(pod.Namespace, pod.Spec.ServiceAccountName)
}

// PodTLSMode returns the tls mode associated with the pod if pod has been injected with sidecar
func PodTLSMode(pod *coreV1.Pod) string {
	if pod == nil {
		return model.DisabledTLSModeLabel
	}
	return model.GetTLSModeFromEndpointLabels(pod.Labels)
}

// KeyFunc is the internal API key function that returns "namespace"/"name" or
// "name" if "namespace" is empty
func KeyFunc(name, namespace string) string {
	if len(namespace) == 0 {
		return name
	}
	return namespace + "/" + name
}

func formatUID(namespace, name string) string {
	return "istio://" + namespace + "/services/" + name // Format : "istio://%s/services/%s"
}
