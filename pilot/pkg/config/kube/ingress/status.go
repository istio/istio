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
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	listerv1beta1 "k8s.io/client-go/listers/networking/v1beta1"

	"istio.io/istio/pkg/config/mesh"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/queue"
	"istio.io/pkg/log"
)

const (
	updateInterval = 60 * time.Second
)

// StatusSyncer keeps the status IP in each Ingress resource updated
type StatusSyncer struct {
	meshHolder mesh.Holder
	client     kubernetes.Interface

	queue              queue.Instance
	ingressLister      listerv1beta1.IngressLister
	podLister          listerv1.PodLister
	serviceLister      listerv1.ServiceLister
	nodeLister         listerv1.NodeLister
	ingressClassLister listerv1beta1.IngressClassLister
}

// Run the syncer until stopCh is closed
func (s *StatusSyncer) Run(stopCh <-chan struct{}) {
	go s.queue.Run(stopCh)
	go s.runUpdateStatus(stopCh)
}

// NewStatusSyncer creates a new instance
func NewStatusSyncer(meshHolder mesh.Holder, client kubelib.Client) *StatusSyncer {
	// as in controller, ingressClassListener can be nil since not supported in k8s version <1.18
	var ingressClassLister listerv1beta1.IngressClassLister
	if NetworkingIngressAvailable(client) {
		ingressClassLister = client.KubeInformer().Networking().V1beta1().IngressClasses().Lister()
	}

	// queue requires a time duration for a retry delay after a handler error
	q := queue.NewQueue(5 * time.Second)

	return &StatusSyncer{
		meshHolder:         meshHolder,
		client:             client.Kube(),
		ingressLister:      client.KubeInformer().Networking().V1beta1().Ingresses().Lister(),
		podLister:          client.KubeInformer().Core().V1().Pods().Lister(),
		serviceLister:      client.KubeInformer().Core().V1().Services().Lister(),
		nodeLister:         client.KubeInformer().Core().V1().Nodes().Lister(),
		ingressClassLister: ingressClassLister,
		queue:              q,
	}
}

func (s *StatusSyncer) onEvent() error {
	addrs, err := s.runningAddresses(ingressNamespace)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return s.updateStatus(sliceToStatus(addrs))
}

func (s *StatusSyncer) runUpdateStatus(stop <-chan struct{}) {
	if _, err := s.runningAddresses(ingressNamespace); err != nil {
		log.Warn("Missing ingress, skip status updates")
		err = wait.PollUntil(10*time.Second, func() (bool, error) {
			if sa, err := s.runningAddresses(ingressNamespace); err != nil || len(sa) == 0 {
				return false, nil
			}
			return true, nil
		}, stop)
		if err != nil {
			log.Warn("Error waiting for ingress")
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

	if len(l) == 0 {
		return nil
	}

	sort.SliceStable(status, lessLoadBalancerIngress(status))

	for _, currIng := range l {
		shouldTarget, err := s.shouldTargetIngress(currIng)
		if err != nil {
			log.Warnf("error determining whether should target ingress for status update: %v", err)
			return err
		}
		if !shouldTarget {
			continue
		}

		curIPs := currIng.Status.LoadBalancer.Ingress
		sort.SliceStable(curIPs, lessLoadBalancerIngress(curIPs))

		if ingressSliceEqual(status, curIPs) {
			log.Debugf("skipping update of Ingress %v/%v (no change)", currIng.Namespace, currIng.Name)
			continue
		}

		currIng.Status.LoadBalancer.Ingress = status

		_, err = s.client.NetworkingV1beta1().Ingresses(currIng.Namespace).UpdateStatus(context.TODO(), currIng, metaV1.UpdateOptions{})
		if err != nil {
			log.Warnf("error updating ingress status: %v", err)
		}
	}

	return nil
}

// runningAddresses returns a list of IP addresses and/or FQDN in the namespace
// where the ingress controller is currently running
func (s *StatusSyncer) runningAddresses(ingressNs string) ([]string, error) {
	addrs := make([]string, 0)
	ingressService := s.meshHolder.Mesh().IngressService
	ingressSelector := s.meshHolder.Mesh().IngressSelector

	if ingressService != "" {
		svc, err := s.serviceLister.Services(ingressNs).Get(ingressService)
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

	// get all pods acting as ingress gateways
	igSelector := getIngressGatewaySelector(ingressSelector, ingressService)
	igPods, err := s.podLister.Pods(ingressNamespace).List(labels.SelectorFromSet(igSelector))
	if err != nil {
		return nil, err
	}

	for _, pod := range igPods {
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
	lbi := make([]coreV1.LoadBalancerIngress, 0, len(endpoints))
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

// shouldTargetIngress determines whether the status watcher should target a given ingress resource
func (s *StatusSyncer) shouldTargetIngress(ingress *v1beta1.Ingress) (bool, error) {
	var ingressClass *v1beta1.IngressClass
	if s.ingressClassLister != nil && ingress.Spec.IngressClassName != nil {
		c, err := s.ingressClassLister.Get(*ingress.Spec.IngressClassName)
		if err != nil && !kerrors.IsNotFound(err) {
			return false, err
		}
		ingressClass = c
	}
	return shouldProcessIngressWithClass(s.meshHolder.Mesh(), ingress, ingressClass), nil
}
