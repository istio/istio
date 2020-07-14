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

package ingress

import (
	"context"
	"net"
	"sort"
	"strings"
	"time"

	coreV1 "k8s.io/api/core/v1"
	"k8s.io/api/networking/v1beta1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	listerv1beta1 "k8s.io/client-go/listers/networking/v1beta1"

	meshconfig "istio.io/api/mesh/v1alpha1"

	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	kubelib "istio.io/istio/pkg/kube"
	queue2 "istio.io/istio/pkg/queue"

	"istio.io/pkg/log"
)

const (
	updateInterval = 60 * time.Second
)

// StatusSyncer keeps the status IP in each Ingress resource updated
type StatusSyncer struct {
	client kubernetes.Interface

	ingressClass        string
	defaultIngressClass string

	// Name of service (ingressgateway default) to find the IP
	ingressService string

	queue         queue2.Instance
	ingressLister listerv1beta1.IngressLister
	podLister     listerv1.PodLister
	serviceLister listerv1.ServiceLister
	nodeLister    listerv1.NodeLister
}

// Run the syncer until stopCh is closed
func (s *StatusSyncer) Run(stopCh <-chan struct{}) {
	go s.queue.Run(stopCh)
	go s.runUpdateStatus(stopCh)
	<-stopCh
}

// NewStatusSyncer creates a new instance
func NewStatusSyncer(mesh *meshconfig.MeshConfig, client kubelib.Client) (*StatusSyncer, error) {

	// we need to use the defined ingress class to allow multiple leaders
	// in order to update information about ingress status
	ingressClass, defaultIngressClass := convertIngressControllerMode(mesh.IngressControllerMode, mesh.IngressClass)

	// queue requires a time duration for a retry delay after a handler error
	queue := queue2.NewQueue(1 * time.Second)

	st := StatusSyncer{
		client:              client,
		ingressLister:       client.KubeInformer().Networking().V1beta1().Ingresses().Lister(),
		podLister:           client.KubeInformer().Core().V1().Pods().Lister(),
		serviceLister:       client.KubeInformer().Core().V1().Services().Lister(),
		nodeLister:          client.KubeInformer().Core().V1().Nodes().Lister(),
		queue:               queue,
		ingressClass:        ingressClass,
		defaultIngressClass: defaultIngressClass,
		ingressService:      mesh.IngressService,
	}

	return &st, nil
}

func (s *StatusSyncer) onEvent() error {
	addrs, err := s.runningAddresses(ingressNamespace)
	if err != nil {
		return err
	}

	return s.updateStatus(sliceToStatus(addrs))
}

func (s *StatusSyncer) runUpdateStatus(stop <-chan struct{}) {
	if _, err := s.runningAddresses(ingressNamespace); err != nil {
		log.Warna("Missing ingress, skip status updates")
		err = wait.PollUntil(10*time.Second, func() (bool, error) {
			if sa, err := s.runningAddresses(ingressNamespace); err != nil || len(sa) == 0 {
				return false, nil
			}
			return true, nil
		}, stop)
		if err != nil {
			log.Warna("Error waiting for ingress")
			return
		}
	}
	err := wait.PollUntil(updateInterval, func() (bool, error) {
		s.queue.Push(s.onEvent)
		return false, nil
	}, stop)

	if err != nil {
		log.Errorf("Stop requested")
	}
}

// updateStatus updates ingress status with the list of IP
func (s *StatusSyncer) updateStatus(status []coreV1.LoadBalancerIngress) error {
	l, err := s.ingressLister.List(labels.Everything())
	if err != nil {
		return err
	}
	for _, currIng := range l {
		if !classIsValid(currIng, s.ingressClass, s.defaultIngressClass) {
			continue
		}

		curIPs := currIng.Status.LoadBalancer.Ingress
		sort.SliceStable(status, lessLoadBalancerIngress(status))
		sort.SliceStable(curIPs, lessLoadBalancerIngress(curIPs))

		if ingressSliceEqual(status, curIPs) {
			log.Debugf("skipping update of Ingress %v/%v (no change)", currIng.Namespace, currIng.Name)
			return nil
		}

		currIng.Status.LoadBalancer.Ingress = status

		_, err := s.client.NetworkingV1beta1().Ingresses(currIng.Namespace).UpdateStatus(context.TODO(), currIng, metaV1.UpdateOptions{})
		if err != nil {
			log.Warnf("error updating ingress status: %v", err)
		}
	}

	return nil
}

// runningAddresses returns a list of IP addresses and/or FQDN where the
// ingress controller is currently running
func (s *StatusSyncer) runningAddresses(ingressNs string) ([]string, error) {
	addrs := make([]string, 0)

	if s.ingressService != "" {
		svc, err := s.serviceLister.Services(ingressNs).Get(s.ingressService)
		if err != nil {
			return nil, err
		}

		if svc.Spec.Type == coreV1.ServiceTypeExternalName {
			addrs = append(addrs, svc.Spec.ExternalName)
			return addrs, nil
		}

		for _, ip := range svc.Status.LoadBalancer.Ingress {
			if ip.IP == "" {
				addrs = append(addrs, ip.Hostname)
			} else {
				addrs = append(addrs, ip.IP)
			}
		}

		addrs = append(addrs, svc.Spec.ExternalIPs...)
		return addrs, nil
	}

	// get information about all the pods running the ingress controller (gateway)
	pods, err := s.podLister.Pods(ingressNamespace).List(labels.SelectorFromSet(map[string]string{"app": "ingressgateway"}))
	if err != nil {
		return nil, err
	}

	for _, pod := range pods {
		// only Running pods are valid
		if pod.Status.Phase != coreV1.PodRunning {
			continue
		}

		// Find node external IP
		node, err := s.nodeLister.Get(pod.Spec.NodeName)
		if err != nil {
			continue
		}

		for _, address := range node.Status.Addresses {
			if address.Type == coreV1.NodeExternalIP {
				if address.Address != "" && !addressInSlice(address.Address, addrs) {
					addrs = append(addrs, address.Address)
				}
			}
		}
	}

	return addrs, nil
}

func addressInSlice(addr string, list []string) bool {
	for _, v := range list {
		if v == addr {
			return true
		}
	}

	return false
}

// sliceToStatus converts a slice of IP and/or hostnames to LoadBalancerIngress
func sliceToStatus(endpoints []string) []coreV1.LoadBalancerIngress {
	lbi := make([]coreV1.LoadBalancerIngress, 0)
	for _, ep := range endpoints {
		if net.ParseIP(ep) == nil {
			lbi = append(lbi, coreV1.LoadBalancerIngress{Hostname: ep})
		} else {
			lbi = append(lbi, coreV1.LoadBalancerIngress{IP: ep})
		}
	}

	return lbi
}

func lessLoadBalancerIngress(addrs []coreV1.LoadBalancerIngress) func(int, int) bool {
	return func(a, b int) bool {
		switch strings.Compare(addrs[a].Hostname, addrs[b].Hostname) {
		case -1:
			return true
		case 1:
			return false
		}
		return addrs[a].IP < addrs[b].IP
	}
}

func ingressSliceEqual(lhs, rhs []coreV1.LoadBalancerIngress) bool {
	if len(lhs) != len(rhs) {
		return false
	}

	for i := range lhs {
		if lhs[i].IP != rhs[i].IP {
			return false
		}
		if lhs[i].Hostname != rhs[i].Hostname {
			return false
		}
	}
	return true
}

// convertIngressControllerMode converts Ingress controller mode into k8s ingress status syncer ingress class and
// default ingress class. Ingress class and default ingress class are used by the syncer to determine whether or not to
// update the IP of a ingress resource.
func convertIngressControllerMode(mode meshconfig.MeshConfig_IngressControllerMode,
	class string) (string, string) {
	var ingressClass, defaultIngressClass string
	switch mode {
	case meshconfig.MeshConfig_DEFAULT:
		defaultIngressClass = class
		ingressClass = class
	case meshconfig.MeshConfig_STRICT:
		ingressClass = class
	}
	return ingressClass, defaultIngressClass
}

// classIsValid returns true if the given Ingress either doesn't specify
// the ingress.class annotation, or it's set to the configured in the
// ingress controller.
func classIsValid(ing *v1beta1.Ingress, controller, defClass string) bool {
	// ingress fetched through annotation.
	var ingress string
	if ing != nil && len(ing.GetAnnotations()) != 0 {
		ingress = ing.GetAnnotations()[kube.IngressClassAnnotation]
	}

	// we have 2 valid combinations
	// 1 - ingress with default class | blank annotation on ingress
	// 2 - ingress with specific class | same annotation on ingress
	//
	// and 2 invalid combinations
	// 3 - ingress with default class | fixed annotation on ingress
	// 4 - ingress with specific class | different annotation on ingress
	if ingress == "" && controller == defClass {
		return true
	}

	return ingress == controller
}
