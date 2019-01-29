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
	"fmt"
	"sort"
	"strconv"
	"strings"

	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/spiffe"

	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// IngressClassAnnotation is the annotation on ingress resources for the class of controllers
	// responsible for it
	IngressClassAnnotation = "kubernetes.io/ingress.class"

	// KubeServiceAccountsOnVMAnnotation is to specify the K8s service accounts that are allowed to run
	// this service on the VMs
	KubeServiceAccountsOnVMAnnotation = "alpha.istio.io/kubernetes-serviceaccounts"

	// CanonicalServiceAccountsAnnotation is to specify the non-Kubernetes service accounts that
	// are allowed to run this service.
	CanonicalServiceAccountsAnnotation = "alpha.istio.io/canonical-serviceaccounts"

	// ServiceExportAnnotation specifies the namespaces to which this service should be exported to.
	//   "*" which is the default, indicates it is reachable within the mesh
	//   "." indicates it is reachable within its namespace
	ServiceExportAnnotation = "networking.istio.io/exportTo"

	managementPortPrefix = "mgmt-"
)

func convertLabels(obj meta_v1.ObjectMeta) model.Labels {
	out := make(model.Labels, len(obj.Labels))
	for k, v := range obj.Labels {
		out[k] = v
	}
	return out
}

func convertPort(port v1.ServicePort) *model.Port {
	return &model.Port{
		Name:     port.Name,
		Port:     int(port.Port),
		Protocol: ConvertProtocol(port.Name, port.Protocol),
	}
}

func convertService(svc v1.Service, domainSuffix string) *model.Service {
	addr, external := model.UnspecifiedIP, ""
	if svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != v1.ClusterIPNone {
		addr = svc.Spec.ClusterIP
	}

	resolution := model.ClientSideLB
	meshExternal := false

	if svc.Spec.Type == v1.ServiceTypeExternalName && svc.Spec.ExternalName != "" {
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
		if svc.Annotations[CanonicalServiceAccountsAnnotation] != "" {
			for _, csa := range strings.Split(svc.Annotations[CanonicalServiceAccountsAnnotation], ",") {
				serviceaccounts = append(serviceaccounts, csa)
			}
		}
		if svc.Annotations[KubeServiceAccountsOnVMAnnotation] != "" {
			for _, ksa := range strings.Split(svc.Annotations[KubeServiceAccountsOnVMAnnotation], ",") {
				serviceaccounts = append(serviceaccounts, kubeToIstioServiceAccount(ksa, svc.Namespace))
			}
		}
		if svc.Annotations[ServiceExportAnnotation] != "" {
			exportTo = make(map[model.Visibility]bool)
			for _, e := range strings.Split(svc.Annotations[ServiceExportAnnotation], ",") {
				exportTo[model.Visibility(e)] = true
			}
		}
	}
	sort.Sort(sort.StringSlice(serviceaccounts))

	return &model.Service{
		Hostname:        serviceHostname(svc.Name, svc.Namespace, domainSuffix),
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
}

func externalNameServiceInstances(k8sSvc v1.Service, svc *model.Service) []*model.ServiceInstance {
	if k8sSvc.Spec.Type != v1.ServiceTypeExternalName || k8sSvc.Spec.ExternalName == "" {
		return nil
	}
	var out []*model.ServiceInstance
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

// serviceHostname produces FQDN for a k8s service
func serviceHostname(name, namespace, domainSuffix string) model.Hostname {
	return model.Hostname(fmt.Sprintf("%s.%s.svc.%s", name, namespace, domainSuffix))
}

// kubeToIstioServiceAccount converts a K8s service account to an Istio service account
func kubeToIstioServiceAccount(saname string, ns string) string {
	return spiffe.MustGenSpiffeURI(ns, saname)
}

// KeyFunc is the internal API key function that returns "namespace"/"name" or
// "name" if "namespace" is empty
func KeyFunc(name, namespace string) string {
	if len(namespace) == 0 {
		return name
	}
	return namespace + "/" + name
}

// parseHostname extracts service name and namespace from the service hostname
func parseHostname(hostname model.Hostname) (name string, namespace string, err error) {
	parts := strings.Split(string(hostname), ".")
	if len(parts) < 2 {
		err = fmt.Errorf("missing service name and namespace from the service hostname %q", hostname)
		return
	}
	name = parts[0]
	namespace = parts[1]
	return
}

// ConvertProtocol from k8s protocol and port name
func ConvertProtocol(name string, proto v1.Protocol) model.Protocol {
	out := model.ProtocolTCP
	switch proto {
	case v1.ProtocolUDP:
		out = model.ProtocolUDP
	case v1.ProtocolTCP:
		prefix := name
		if strings.HasPrefix(strings.ToLower(prefix), strings.ToLower(string(model.ProtocolGRPCWeb))) {
			out = model.ProtocolGRPCWeb
			break
		}
		i := strings.Index(name, "-")
		if i >= 0 {
			prefix = name[:i]
		}
		protocol := model.ParseProtocol(prefix)
		if protocol != model.ProtocolUDP && protocol != model.ProtocolUnsupported {
			out = protocol
		}
	}
	return out
}

func convertProbePort(c *v1.Container, handler *v1.Handler) (*model.Port, error) {
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

// convertProbesToPorts returns a PortList consisting of the ports where the
// pod is configured to do Liveness and Readiness probes
func convertProbesToPorts(t *v1.PodSpec) (model.PortList, error) {
	set := make(map[string]*model.Port)
	var errs error
	for _, container := range t.Containers {
		for _, probe := range []*v1.Probe{container.LivenessProbe, container.ReadinessProbe} {
			if probe == nil {
				continue
			}

			p, err := convertProbePort(&container, &probe.Handler)
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
