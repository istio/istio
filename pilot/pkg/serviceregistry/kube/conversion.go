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

package kube

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/go-multierror"

	"istio.io/api/annotation"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/spiffe"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// IngressClassAnnotation is the annotation on ingress resources for the class of controllers
	// responsible for it
	IngressClassAnnotation = "kubernetes.io/ingress.class"

	managementPortPrefix = "mgmt-"
)

func ConvertLabels(obj metaV1.ObjectMeta) model.Labels {
	out := make(model.Labels, len(obj.Labels))
	for k, v := range obj.Labels {
		out[k] = v
	}
	return out
}

func convertPort(port coreV1.ServicePort) *model.Port {
	return &model.Port{
		Name:     port.Name,
		Port:     int(port.Port),
		Protocol: ConvertProtocol(port.Name, port.Protocol),
	}
}

func ConvertService(svc coreV1.Service, domainSuffix string, clusterID string) *model.Service {
	addr, external := model.UnspecifiedIP, ""
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

	if addr == model.UnspecifiedIP && external == "" { // headless services should not be load balanced
		resolution = model.Passthrough
	}

	ports := make([]*model.Port, 0, len(svc.Spec.Ports))
	for _, port := range svc.Spec.Ports {
		ports = append(ports, convertPort(port))
	}

	var exportTo map[model.Visibility]bool
	serviceaccounts := make([]string, 0)
	if svc.Annotations != nil {
		if svc.Annotations[annotation.AlphaCanonicalServiceAccounts.Name] != "" {
			serviceaccounts = append(serviceaccounts, strings.Split(svc.Annotations[annotation.AlphaCanonicalServiceAccounts.Name], ",")...)
		}
		if svc.Annotations[annotation.AlphaKubernetesServiceAccounts.Name] != "" {
			for _, ksa := range strings.Split(svc.Annotations[annotation.AlphaKubernetesServiceAccounts.Name], ",") {
				serviceaccounts = append(serviceaccounts, kubeToIstioServiceAccount(ksa, svc.Namespace))
			}
		}
		if svc.Annotations[annotation.NetworkingExportTo.Name] != "" {
			exportTo = make(map[model.Visibility]bool)
			for _, e := range strings.Split(svc.Annotations[annotation.NetworkingExportTo.Name], ",") {
				exportTo[model.Visibility(e)] = true
			}
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
			Name:      svc.Name,
			Namespace: svc.Namespace,
			UID:       fmt.Sprintf("istio://%s/services/%s", svc.Namespace, svc.Name),
			ExportTo:  exportTo,
		},
	}

	if svc.Spec.Type == coreV1.ServiceTypeLoadBalancer && len(svc.Status.LoadBalancer.Ingress) > 0 {
		var lbAddrs []string
		for _, ingress := range svc.Status.LoadBalancer.Ingress {
			if len(ingress.IP) > 0 {
				lbAddrs = append(lbAddrs, ingress.IP)
			} else if len(ingress.Hostname) > 0 {
				addrs, err := net.DefaultResolver.LookupHost(context.TODO(), ingress.Hostname)
				if err != nil {
					lbAddrs = append(lbAddrs, addrs...)
				}
			}
		}
		if len(lbAddrs) > 0 {
			istioService.Attributes.ClusterExternalAddresses = map[string][]string{clusterID: lbAddrs}
		}
	}

	return istioService
}

func ExternalNameServiceInstances(k8sSvc coreV1.Service, svc *model.Service) []*model.ServiceInstance {
	if k8sSvc.Spec.Type != coreV1.ServiceTypeExternalName || k8sSvc.Spec.ExternalName == "" {
		return nil
	}
	out := make([]*model.ServiceInstance, 0, len(svc.Ports))
	for _, portEntry := range svc.Ports {
		out = append(out, &model.ServiceInstance{
			Endpoint: model.NetworkEndpoint{
				Address:     k8sSvc.Spec.ExternalName,
				Port:        portEntry.Port,
				ServicePort: portEntry,
			},
			Service: svc,
			Labels:  k8sSvc.Labels,
		})
	}
	return out
}

// ServiceHostname produces FQDN for a k8s service
func ServiceHostname(name, namespace, domainSuffix string) model.Hostname {
	return model.Hostname(fmt.Sprintf("%s.%s.svc.%s", name, namespace, domainSuffix))
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

// KeyFunc is the internal API key function that returns "namespace"/"name" or
// "name" if "namespace" is empty
func KeyFunc(name, namespace string) string {
	if len(namespace) == 0 {
		return name
	}
	return namespace + "/" + name
}

// ParseHostname extracts service name and namespace from the service hostname
func ParseHostname(hostname model.Hostname) (name string, namespace string, err error) {
	parts := strings.Split(string(hostname), ".")
	if len(parts) < 2 {
		err = fmt.Errorf("missing service name and namespace from the service hostname %q", hostname)
		return
	}
	name = parts[0]
	namespace = parts[1]
	return
}

var grpcWeb = string(model.ProtocolGRPCWeb)
var grpcWebLen = len(grpcWeb)

// ConvertProtocol from k8s protocol and port name
func ConvertProtocol(name string, proto coreV1.Protocol) model.Protocol {
	out := model.ProtocolTCP
	switch proto {
	case coreV1.ProtocolUDP:
		out = model.ProtocolUDP
	case coreV1.ProtocolTCP:
		if len(name) >= grpcWebLen && strings.EqualFold(name[:grpcWebLen], grpcWeb) {
			out = model.ProtocolGRPCWeb
			break
		}
		i := strings.IndexByte(name, '-')
		if i >= 0 {
			name = name[:i]
		}
		protocol := model.ParseProtocol(name)
		if protocol != model.ProtocolUDP && protocol != model.ProtocolUnsupported {
			out = protocol
		}
	}
	return out
}

func ConvertProbePort(c *coreV1.Container, handler *coreV1.Handler) (*model.Port, error) {
	if handler == nil {
		return nil, nil
	}

	var protocol model.Protocol
	var portVal intstr.IntOrString

	// Only two types of handler is allowed by Kubernetes (HTTPGet or TCPSocket)
	switch {
	case handler.HTTPGet != nil:
		portVal = handler.HTTPGet.Port
		protocol = model.ProtocolHTTP
	case handler.TCPSocket != nil:
		portVal = handler.TCPSocket.Port
		protocol = model.ProtocolTCP
	default:
		return nil, nil
	}

	switch portVal.Type {
	case intstr.Int:
		port := portVal.IntValue()
		return &model.Port{
			Name:     managementPortPrefix + strconv.Itoa(port),
			Port:     port,
			Protocol: protocol,
		}, nil
	case intstr.String:
		for _, named := range c.Ports {
			if named.Name == portVal.String() {
				port := int(named.ContainerPort)
				return &model.Port{
					Name:     managementPortPrefix + strconv.Itoa(port),
					Port:     port,
					Protocol: protocol,
				}, nil
			}
		}
		return nil, fmt.Errorf("missing named port %q", portVal)
	default:
		return nil, fmt.Errorf("incorrect port type %q", portVal.Type)
	}
}

// ConvertProbesToPorts returns a PortList consisting of the ports where the
// pod is configured to do Liveness and Readiness probes
func ConvertProbesToPorts(t *coreV1.PodSpec) (model.PortList, error) {
	set := make(map[string]*model.Port)
	var errs error
	for _, container := range t.Containers {
		for _, probe := range []*coreV1.Probe{container.LivenessProbe, container.ReadinessProbe} {
			if probe == nil {
				continue
			}

			p, err := ConvertProbePort(&container, &probe.Handler)
			if err != nil {
				errs = multierror.Append(errs, err)
			} else if p != nil && set[p.Name] == nil {
				// Deduplicate along the way. We don't differentiate between HTTP vs TCP mgmt ports
				set[p.Name] = p
			}
		}
	}

	mgmtPorts := make(model.PortList, 0, len(set))
	for _, p := range set {
		mgmtPorts = append(mgmtPorts, p)
	}
	sort.Slice(mgmtPorts, func(i, j int) bool { return mgmtPorts[i].Port < mgmtPorts[j].Port })

	return mgmtPorts, errs
}
