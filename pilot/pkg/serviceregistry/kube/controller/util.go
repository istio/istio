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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	listerv1 "k8s.io/client-go/listers/core/v1"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
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

func getLabelValue(metadata metav1.Object, label string, fallBackLabel string) string {
	labels := metadata.GetLabels()
	val := labels[label]
	if val != "" {
		return val
	}

	return labels[fallBackLabel]
}

func createUID(podName, namespace string) string {
	return "kubernetes://" + podName + "." + namespace
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

// get the target Port for this service port
func findServiceTargetPort(servicePort *model.Port, k8sService *v1.Service) (int, string) {
	targetPort := 0
	targetPortName := ""
	for _, p := range k8sService.Spec.Ports {
		// TODO(@hzxuzhonghu): check protocol as well as port
		if p.Name == servicePort.Name || p.Port == int32(servicePort.Port) {
			if p.TargetPort.Type == intstr.Int && p.TargetPort.IntVal > 0 {
				targetPort = int(p.TargetPort.IntVal)
			} else {
				targetPortName = p.TargetPort.StrVal
			}
			break
		}
	}
	return targetPort, targetPortName
}

func getPodServices(s listerv1.ServiceLister, pod *v1.Pod) ([]*v1.Service, error) {
	allServices, err := s.Services(pod.Namespace).List(klabels.Everything())
	if err != nil {
		return nil, err
	}

	var services []*v1.Service
	for _, service := range allServices {
		if service.Spec.Selector == nil {
			// services with nil selectors match nothing, not everything.
			continue
		}
		selector := klabels.Set(service.Spec.Selector).AsSelectorPreValidated()
		if selector.Matches(klabels.Set(pod.Labels)) {
			services = append(services, service)
		}
	}

	return services, nil
}

func portsEqual(a, b []v1.EndpointPort) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i].Name != b[i].Name || a[i].Port != b[i].Port || a[i].Protocol != b[i].Protocol ||
			ptrValueOrEmpty(a[i].AppProtocol) != ptrValueOrEmpty(b[i].AppProtocol) {
			return false
		}
	}

	return true
}

func addressesEqual(a, b []v1.EndpointAddress) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i].IP != b[i].IP || a[i].Hostname != b[i].Hostname ||
			ptrValueOrEmpty(a[i].NodeName) != ptrValueOrEmpty(b[i].NodeName) {
			return false
		}
	}

	return true
}

func ptrValueOrEmpty(ptr *string) string {
	if ptr != nil {
		return *ptr
	}
	return ""
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
	_, ok := svc.Annotations[kube.NodeSelectorAnnotation]
	return ok && svc.Spec.Type == v1.ServiceTypeNodePort
}
