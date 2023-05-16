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

package controller

import (
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"istio.io/api/annotation"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
)

func hasProxyIP(addresses []v1.EndpointAddress, proxyIP string) bool {
	for _, addr := range addresses {
		if addr.IP == proxyIP {
			return true
		}
	}
	return false
}

func getLabelValue(metadata metav1.ObjectMeta, label string, fallBackLabel string) string {
	metaLabels := metadata.GetLabels()
	val := metaLabels[label]
	if val != "" {
		return val
	}

	return metaLabels[fallBackLabel]
}

// Forked from Kubernetes k8s.io/kubernetes/pkg/api/v1/pod
// FindPort locates the container port for the given pod and portName.  If the
// targetPort is a number, use that.  If the targetPort is a string, look that
// string up in all named ports in all containers in the target pod.  If no
// match is found, fail.
func FindPort(pod *v1.Pod, svcPort *v1.ServicePort) (int, error) {
	portName := svcPort.TargetPort
	switch portName.Type {
	case intstr.String:
		name := portName.StrVal
		for _, container := range pod.Spec.Containers {
			for _, port := range container.Ports {
				if port.Name == name && port.Protocol == svcPort.Protocol {
					return int(port.ContainerPort), nil
				}
			}
		}
	case intstr.Int:
		return portName.IntValue(), nil
	}

	return 0, fmt.Errorf("no suitable port for manifest: %s", pod.UID)
}

// findPortFromMetadata resolves the TargetPort of a Service Port, by reading the Pod spec.
func findPortFromMetadata(svcPort v1.ServicePort, podPorts []model.PodPort) (int, error) {
	target := svcPort.TargetPort

	switch target.Type {
	case intstr.String:
		name := target.StrVal
		for _, port := range podPorts {
			if port.Name == name && port.Protocol == string(svcPort.Protocol) {
				return port.ContainerPort, nil
			}
		}
	case intstr.Int:
		// For a direct reference we can just return the port number
		return target.IntValue(), nil
	}

	return 0, fmt.Errorf("no matching port found for %+v", svcPort)
}

type serviceTargetPort struct {
	// the mapped port number, or 0 if unspecified
	num int
	// the mapped port name
	name string
	// a bool indicating if the mapped port name was explicitly set on the TargetPort field, or inferred from k8s' port.Name
	explicitName bool
}

func findServiceTargetPort(servicePort *model.Port, k8sService *v1.Service) serviceTargetPort {
	for _, p := range k8sService.Spec.Ports {
		// TODO(@hzxuzhonghu): check protocol as well as port
		if p.Name == servicePort.Name || p.Port == int32(servicePort.Port) {
			if p.TargetPort.Type == intstr.Int && p.TargetPort.IntVal > 0 {
				return serviceTargetPort{num: int(p.TargetPort.IntVal), name: p.Name, explicitName: false}
			}
			return serviceTargetPort{num: 0, name: p.TargetPort.StrVal, explicitName: true}
		}
	}
	// should never happen
	log.Debugf("did not find matching target port for %v on service %s", servicePort, k8sService.Name)
	return serviceTargetPort{num: 0, name: "", explicitName: false}
}

func getPodServices(allServices []*v1.Service, pod *v1.Pod) []*v1.Service {
	var services []*v1.Service
	for _, service := range allServices {
		if labels.Instance(service.Spec.Selector).Match(pod.Labels) {
			services = append(services, service)
		}
	}

	return services
}

func getNodeSelectorsForService(svc *v1.Service) labels.Instance {
	if nodeSelector := svc.Annotations[kube.NodeSelectorAnnotation]; nodeSelector != "" {
		var nodeSelectorKV map[string]string
		if err := json.Unmarshal([]byte(nodeSelector), &nodeSelectorKV); err != nil {
			log.Debugf("failed to unmarshal node selector annotation value for service %s.%s: %v",
				svc.Name, svc.Namespace, err)
		}
		return nodeSelectorKV
	}
	return nil
}

func nodeEquals(a, b kubernetesNode) bool {
	return a.address == b.address && a.labels.Equals(b.labels)
}

func isNodePortGatewayService(svc *v1.Service) bool {
	if svc == nil {
		return false
	}
	_, ok := svc.Annotations[kube.NodeSelectorAnnotation]
	return ok && svc.Spec.Type == v1.ServiceTypeNodePort
}

// Get the pod key of the proxy which can be used to get pod from the informer cache
func podKeyByProxy(proxy *model.Proxy) types.NamespacedName {
	parts := strings.Split(proxy.ID, ".")
	if len(parts) == 2 && proxy.Metadata.Namespace == parts[1] {
		return types.NamespacedName{Name: parts[0], Namespace: parts[1]}
	}

	return types.NamespacedName{}
}

func namespacedNameForService(svc *model.Service) types.NamespacedName {
	return types.NamespacedName{
		Namespace: svc.Attributes.Namespace,
		Name:      svc.Attributes.Name,
	}
}

// serviceClusterSetLocalHostname produces Kubernetes Multi-Cluster Services (MCS) ClusterSet FQDN for a k8s service
func serviceClusterSetLocalHostname(nn types.NamespacedName) host.Name {
	return host.Name(nn.Name + "." + nn.Namespace + "." + "svc" + "." + constants.DefaultClusterSetLocalDomain)
}

// serviceClusterSetLocalHostnameForKR calls serviceClusterSetLocalHostname with the name and namespace of the given kubernetes resource.
func serviceClusterSetLocalHostnameForKR(obj metav1.Object) host.Name {
	return serviceClusterSetLocalHostname(config.NamespacedName(obj))
}

func labelRequirement(key string, op selection.Operator, vals []string, opts ...field.PathOption) *klabels.Requirement {
	out, err := klabels.NewRequirement(key, op, vals, opts...)
	if err != nil {
		panic(fmt.Sprintf("failed creating requirements for Service: %v", err))
	}
	return out
}

func serviceEqual(first, second *v1.Service) bool {
	if first == nil {
		return second == nil
	}
	if second == nil {
		return first == nil
	}

	if first.Name != second.Name ||
		first.Namespace != second.Namespace {
		return false
	}

	if !maps.Equal(first.Labels, second.Labels) {
		return false
	}

	if !serviceSpecEqual(&first.Spec, &second.Spec) {
		return false
	}

	if !serviceStatusEqual(&first.Status, &second.Status) {
		return false
	}

	ann1 := map[string]string{}
	ann2 := map[string]string{}
	for key, value := range first.Annotations {
		if istioAnnotation(key) || lookupAnnotation(key) != nil {
			ann1[key] = value
		}
	}
	for key, value := range second.Annotations {
		if istioAnnotation(key) || lookupAnnotation(key) != nil {
			ann2[key] = value
		}
	}
	return maps.Equal(ann1, ann2)
}

func serviceSpecEqual(first, second *v1.ServiceSpec) bool {
	if !slices.EqualFunc[v1.ServicePort, v1.ServicePort](first.Ports, second.Ports, servicePortEqual) {
		return false
	}

	if !maps.Equal(first.Selector, second.Selector) {
		return false
	}

	if first.ClusterIP != second.ClusterIP {
		return false
	}

	if !slices.EqualFunc[string, string](first.ClusterIPs, second.ClusterIPs, func(s1, s2 string) bool { return s1 == s2 }) {
		return false
	}

	if first.Type != second.Type {
		return false
	}

	if !slices.EqualFunc[string, string](first.ExternalIPs, second.ExternalIPs, func(s1, s2 string) bool { return s1 == s2 }) {
		return false
	}

	if first.ExternalName != second.ExternalName {
		return false
	}

	if first.SessionAffinity != second.SessionAffinity {
		return false
	}

	if first.LoadBalancerIP != second.LoadBalancerIP {
		return false
	}

	if !slices.EqualFunc[string, string](first.LoadBalancerSourceRanges, second.LoadBalancerSourceRanges, func(s1, s2 string) bool { return s1 == s2 }) {
		return false
	}

	if first.ExternalTrafficPolicy != second.ExternalTrafficPolicy {
		return false
	}

	if first.HealthCheckNodePort != second.HealthCheckNodePort {
		return false
	}

	if first.PublishNotReadyAddresses != second.PublishNotReadyAddresses {
		return false
	}

	if !serviceSessionAffinityConfigEqual(first.SessionAffinityConfig, second.SessionAffinityConfig) {
		return false
	}

	if !slices.EqualFunc[v1.IPFamily, v1.IPFamily](first.IPFamilies, second.IPFamilies, func(s1, s2 v1.IPFamily) bool { return s1 == s2 }) {
		return false
	}

	if !dataPointerEqual[v1.IPFamilyPolicy](first.IPFamilyPolicy, second.IPFamilyPolicy) {
		return false
	}

	if !dataPointerEqual[bool](first.AllocateLoadBalancerNodePorts, second.AllocateLoadBalancerNodePorts) {
		return false
	}

	if !dataPointerEqual[string](first.LoadBalancerClass, second.LoadBalancerClass) {
		return false
	}

	if !dataPointerEqual[v1.ServiceInternalTrafficPolicy](first.InternalTrafficPolicy, second.InternalTrafficPolicy) {
		return false
	}

	return true
}

func servicePortEqual(first, second v1.ServicePort) bool {
	if first.Name != second.Name {
		return false
	}

	if first.Protocol != second.Protocol {
		return false
	}

	if !dataPointerEqual[string](first.AppProtocol, second.AppProtocol) {
		return false
	}

	if first.Port != second.Port {
		return false
	}

	if first.TargetPort != second.TargetPort {
		return false
	}

	if first.NodePort != second.NodePort {
		return false
	}

	return true
}

func serviceSessionAffinityConfigEqual(first, second *v1.SessionAffinityConfig) bool {
	if first == nil {
		return second == nil
	}
	if second == nil {
		return first == nil
	}

	if first.ClientIP == nil {
		return second.ClientIP == nil
	}
	if second.ClientIP == nil {
		return first.ClientIP == nil
	}

	if first.ClientIP.TimeoutSeconds == nil {
		return second.ClientIP.TimeoutSeconds == nil
	}
	if second.ClientIP.TimeoutSeconds == nil {
		return first.ClientIP.TimeoutSeconds == nil
	}

	return *first.ClientIP.TimeoutSeconds == *second.ClientIP.TimeoutSeconds
}

func serviceStatusEqual(first, second *v1.ServiceStatus) bool {
	ingress1 := first.LoadBalancer.Ingress
	ingress2 := second.LoadBalancer.Ingress
	return slices.EqualFunc[v1.LoadBalancerIngress, v1.LoadBalancerIngress](ingress1, ingress2, serviceLoadBalancerIngress)
}

func serviceLoadBalancerIngress(first, second v1.LoadBalancerIngress) bool {
	if first.IP != second.IP {
		return false
	}

	if first.Hostname != second.Hostname {
		return false
	}

	if !slices.EqualFunc[v1.PortStatus, v1.PortStatus](first.Ports, second.Ports, servicePortStatus) {
		return false
	}

	return true
}

func servicePortStatus(first, second v1.PortStatus) bool {
	if first.Port != second.Port {
		return false
	}

	if first.Protocol != second.Protocol {
		return false
	}

	if !dataPointerEqual[string](first.Error, second.Error) {
		return false
	}

	return true
}

func dataPointerEqual[E comparable](first, second *E) bool {
	if first == nil {
		return second == nil
	}
	if second == nil {
		return first == nil
	}

	return *first == *second
}

// istioAnnotation is true if the annotation is in Istio's namespace
func istioAnnotation(ann string) bool {
	// We document this Kubernetes annotation, we should analyze it as well
	if ann == "kubernetes.io/ingress.class" {
		return true
	}

	parts := strings.Split(ann, "/")
	if len(parts) == 0 {
		return false
	}

	if !strings.HasSuffix(parts[0], "istio.io") {
		return false
	}

	return true
}

func lookupAnnotation(ann string) *annotation.Instance {
	istioAnnotations := annotation.AllResourceAnnotations()
	for _, candidate := range istioAnnotations {
		if candidate.Name == ann {
			return candidate
		}
	}

	return nil
}
