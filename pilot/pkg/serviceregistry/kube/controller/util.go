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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"istio.io/api/annotation"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
)

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
	if nodeSelector := svc.Annotations[annotation.TrafficNodeSelector.Name]; nodeSelector != "" {
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
	_, ok := svc.Annotations[annotation.TrafficNodeSelector.Name]
	return ok && svc.Spec.Type == v1.ServiceTypeNodePort
}

// Get the pod key of the proxy which can be used to get pod from the informer cache
func podKeyByProxy(proxy *model.Proxy) types.NamespacedName {
	name, namespace, ok := strings.Cut(proxy.ID, ".")
	if ok && proxy.Metadata.Namespace == namespace {
		return types.NamespacedName{Name: name, Namespace: namespace}
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
