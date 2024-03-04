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
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	knetworking "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	istiolabels "istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/log"
	netutil "istio.io/istio/pkg/util/net"
)

var statusLog = log.RegisterScope("ingress status", "")

// StatusSyncer keeps the status IP in each Ingress resource updated
type StatusSyncer struct {
	meshConfig mesh.Watcher

	queue          controllers.Queue
	ingresses      kclient.Client[*knetworking.Ingress]
	ingressClasses kclient.Client[*knetworking.IngressClass]
	pods           kclient.Client[*corev1.Pod]
	services       kclient.Client[*corev1.Service]
	nodes          kclient.Client[*corev1.Node]
}

// Run the syncer until stopCh is closed
func (s *StatusSyncer) Run(stopCh <-chan struct{}) {
	s.queue.Run(stopCh)
	controllers.ShutdownAll(s.services, s.nodes, s.pods, s.ingressClasses, s.ingresses)
}

// NewStatusSyncer creates a new instance
func NewStatusSyncer(meshHolder mesh.Watcher, kc kubelib.Client) *StatusSyncer {
	c := &StatusSyncer{
		meshConfig:     meshHolder,
		ingresses:      kclient.NewFiltered[*knetworking.Ingress](kc, kclient.Filter{ObjectFilter: kc.ObjectFilter()}),
		ingressClasses: kclient.New[*knetworking.IngressClass](kc),
		pods: kclient.NewFiltered[*corev1.Pod](kc, kclient.Filter{
			ObjectFilter:    kc.ObjectFilter(),
			ObjectTransform: kubelib.StripPodUnusedFields,
		}),
		services: kclient.NewFiltered[*corev1.Service](kc, kclient.Filter{ObjectFilter: kc.ObjectFilter()}),
		nodes: kclient.NewFiltered[*corev1.Node](kc, kclient.Filter{
			ObjectTransform: kubelib.StripNodeUnusedFields,
		}),
	}
	c.queue = controllers.NewQueue("ingress status",
		controllers.WithReconciler(c.Reconcile),
		controllers.WithMaxAttempts(5))

	// For any ingress change, enqueue it - we may need to update the status.
	c.ingresses.AddEventHandler(controllers.ObjectHandler(c.queue.AddObject))
	// For any class change, sync all ingress; the handler will filter non-matching ones already
	c.ingressClasses.AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
		// Just sync them all
		c.enqueueAll()
	}))
	// For services, we queue all Ingress if its the ingress service
	c.services.AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
		if o.GetName() == c.meshConfig.Mesh().IngressService && o.GetNamespace() == IngressNamespace {
			c.enqueueAll()
		}
	}))
	// For pods, we enqueue all Ingress if its part of the ingress service
	c.pods.AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
		if c.meshConfig.Mesh().IngressService != "" {
			// Ingress Service takes precedence
			return
		}
		ingressSelector := c.meshConfig.Mesh().IngressSelector

		// get all pods acting as ingress gateways
		igSelector := getIngressGatewaySelector(ingressSelector, "")
		if istiolabels.Instance(igSelector).SubsetOf(o.GetLabels()) {
			// Ingress selector matches this pod, enqueue everything
			c.enqueueAll()
		}
	}))
	// Mesh may have changed ingress fields, enqueue everything
	c.meshConfig.AddMeshHandler(c.enqueueAll)
	return c
}

// runningAddresses returns a list of IP addresses and/or FQDN in the namespace
// where the ingress controller is currently running
func (s *StatusSyncer) runningAddresses() []string {
	addrs := make([]string, 0)
	ingressService := s.meshConfig.Mesh().IngressService
	ingressSelector := s.meshConfig.Mesh().IngressSelector

	if ingressService != "" {
		svc := s.services.Get(ingressService, IngressNamespace)
		if svc == nil {
			return nil
		}

		if svc.Spec.Type == corev1.ServiceTypeExternalName {
			addrs = append(addrs, svc.Spec.ExternalName)

			return addrs
		}

		for _, ip := range svc.Status.LoadBalancer.Ingress {
			if ip.IP == "" {
				addrs = append(addrs, ip.Hostname)
			} else {
				addrs = append(addrs, ip.IP)
			}
		}

		addrs = append(addrs, svc.Spec.ExternalIPs...)
		return addrs
	}

	// get all pods acting as ingress gateways
	igSelector := getIngressGatewaySelector(ingressSelector, ingressService)
	igPods := s.pods.List(IngressNamespace, labels.SelectorFromSet(igSelector))

	for _, pod := range igPods {
		// only Running pods are valid
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}

		// Find node external IP
		node := s.nodes.Get(pod.Spec.NodeName, "")
		if node == nil {
			continue
		}

		for _, address := range node.Status.Addresses {
			if address.Type == corev1.NodeExternalIP {
				if address.Address != "" && !addressInSlice(address.Address, addrs) {
					addrs = append(addrs, address.Address)
				}
			}
		}
	}

	return addrs
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
func sliceToStatus(endpoints []string) []knetworking.IngressLoadBalancerIngress {
	lbi := make([]knetworking.IngressLoadBalancerIngress, 0, len(endpoints))
	for _, ep := range endpoints {
		if !netutil.IsValidIPAddress(ep) {
			lbi = append(lbi, knetworking.IngressLoadBalancerIngress{Hostname: ep})
		} else {
			lbi = append(lbi, knetworking.IngressLoadBalancerIngress{IP: ep})
		}
	}

	sort.SliceStable(lbi, lessLoadBalancerIngress(lbi))
	return lbi
}

func lessLoadBalancerIngress(addrs []knetworking.IngressLoadBalancerIngress) func(int, int) bool {
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

func ingressSliceEqual(lhs, rhs []knetworking.IngressLoadBalancerIngress) bool {
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
func (s *StatusSyncer) shouldTargetIngress(ingress *knetworking.Ingress) bool {
	var ingressClass *knetworking.IngressClass
	if ingress.Spec.IngressClassName != nil {
		ingressClass = s.ingressClasses.Get(*ingress.Spec.IngressClassName, "")
	}
	return shouldProcessIngressWithClass(s.meshConfig.Mesh(), ingress, ingressClass)
}

func (s *StatusSyncer) enqueueAll() {
	for _, ing := range s.ingresses.List(metav1.NamespaceAll, labels.Everything()) {
		s.queue.AddObject(ing)
	}
}

func (s *StatusSyncer) Reconcile(key types.NamespacedName) error {
	log := statusLog.WithLabels("ingress", key)
	ing := s.ingresses.Get(key.Name, key.Namespace)
	if ing == nil {
		log.Debugf("ingress removed, no action")
		return nil
	}
	shouldTarget := s.shouldTargetIngress(ing)
	if !shouldTarget {
		log.Debugf("ingress not selected, no action")
		return nil
	}

	curIPs := ing.Status.LoadBalancer.Ingress
	sort.SliceStable(curIPs, lessLoadBalancerIngress(curIPs))

	wantIPs := sliceToStatus(s.runningAddresses())

	if ingressSliceEqual(wantIPs, curIPs) {
		log.Debugf("ingress has no change, no action")
		return nil
	}

	log.Infof("updating IPs (%v)", wantIPs)
	ing = ing.DeepCopy()
	ing.Status.LoadBalancer.Ingress = wantIPs
	_, err := s.ingresses.UpdateStatus(ing)
	if err != nil {
		return fmt.Errorf("error updating ingress status: %v", err)
	}
	return nil
}
