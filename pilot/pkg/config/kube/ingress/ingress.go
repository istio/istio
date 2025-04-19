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
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	knetworking "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	netutil "istio.io/istio/pkg/util/net"
)

func SupportedIngresses(
	ingressClass krt.Collection[*knetworking.IngressClass],
	ingress krt.Collection[*knetworking.Ingress],
	meshConfig meshwatcher.WatcherCollection,
	services krt.Collection[*corev1.Service],
	nodes krt.Collection[*corev1.Node],
	pods krt.Collection[*corev1.Pod],
	opts krt.OptionsBuilder,
) (krt.Collection[krt.ObjectWithStatus[*knetworking.Ingress, knetworking.IngressStatus]], krt.Collection[*knetworking.Ingress]) {
	podsByNamespace := krt.NewNamespaceIndex(pods)
	return krt.NewStatusCollection(
		ingress,
		func(ctx krt.HandlerContext, i *knetworking.Ingress) (*knetworking.IngressStatus, **knetworking.Ingress) {
			var class *knetworking.IngressClass
			if i.Spec.IngressClassName != nil {
				c := krt.FetchOne(ctx, ingressClass, krt.FilterKey(*i.Spec.IngressClassName))
				if c != nil {
					class = *c
				}
			}

			mesh := krt.FetchOne(ctx, meshConfig.AsCollection())
			if !shouldProcessIngressWithClass(mesh.MeshConfig, i, class) {
				return nil, nil
			}

			wantIPs := sliceToStatus(runningAddresses(ctx, meshConfig, services, nodes, pods, podsByNamespace))

			return &knetworking.IngressStatus{
				LoadBalancer: knetworking.IngressLoadBalancerStatus{
					Ingress: wantIPs,
				},
			}, &i
		},
		opts.WithName("SupportedIngresses")...,
	)
}

func runningAddresses(
	ctx krt.HandlerContext,
	meshConfig meshwatcher.WatcherCollection,
	services krt.Collection[*corev1.Service],
	nodes krt.Collection[*corev1.Node],
	pods krt.Collection[*corev1.Pod],
	podsByNamespace krt.Index[string, *corev1.Pod],
) []string {
	mesh := krt.FetchOne(ctx, meshConfig.AsCollection())
	ingressService := mesh.IngressService
	ingressSelector := mesh.IngressSelector

	if ingressService != "" {
		svcPtr := krt.FetchOne(ctx, services, krt.FilterObjectName(types.NamespacedName{Namespace: IngressNamespace, Name: ingressService}))
		if svcPtr == nil {
			return nil
		}

		svc := *svcPtr
		if svc.Spec.Type == corev1.ServiceTypeExternalName {
			return []string{svc.Spec.ExternalName}
		}

		addrs := make([]string, 0)
		for _, ip := range svc.Status.LoadBalancer.Ingress {
			if ip.IP == "" {
				addrs = append(addrs, ip.Hostname)
			} else {
				addrs = append(addrs, ip.IP)
			}
		}

		return append(addrs, svc.Spec.ExternalIPs...)
	}

	// get all pods acting as ingress gateways
	igSelector := getIngressGatewaySelector(ingressSelector, ingressService)
	igPods := krt.Fetch(
		ctx,
		pods,
		krt.FilterLabel(igSelector),
		krt.FilterIndex(podsByNamespace, IngressNamespace),
	)

	for _, pod := range igPods {
		// only Running pods are valid
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}

		// Find node external IP
		nodePtr := krt.FetchOne(ctx, nodes, krt.FilterKey(pod.Spec.NodeName))
		if nodePtr == nil {
			continue
		}

		node := *nodePtr
		addrs := make([]string, 0)
		for _, address := range node.Status.Addresses {
			if address.Type == corev1.NodeExternalIP {
				if address.Address != "" && !addressInSlice(address.Address, addrs) {
					addrs = append(addrs, address.Address)
				}
			}
		}
		return addrs
	}

	return nil
}

type IngressRule struct {
	IngressName       string
	IngressNamespace  string
	RuleIndex         int
	CreationTimestamp time.Time
	Rule              *knetworking.IngressRule
}

func (ir IngressRule) ResourceName() string {
	return ir.IngressNamespace + "/" + ir.IngressName + "/" + strconv.Itoa(ir.RuleIndex)
}

func RuleCollection(
	ingress krt.Collection[*knetworking.Ingress],
	opts krt.OptionsBuilder,
) (krt.Collection[*IngressRule], krt.Index[string, *IngressRule]) {
	collection := krt.NewManyCollection(
		ingress,
		func(ctx krt.HandlerContext, i *knetworking.Ingress) []*IngressRule {
			// Matches * and "/". Currently not supported - would conflict
			// with any other explicit VirtualService.
			if i.Spec.DefaultBackend != nil {
				log.Infof("Ignore default wildcard ingress, use VirtualService %s:%s",
					i.Namespace, i.Name)
			}

			var rules []*IngressRule
			for idx, rule := range i.Spec.Rules {
				if rule.HTTP == nil {
					log.Infof("invalid ingress rule %s:%s for host %q, no paths defined", i.Namespace, i.Name, rule.Host)
					continue
				}

				rules = append(rules, &IngressRule{
					IngressName:       i.Name,
					IngressNamespace:  i.Namespace,
					RuleIndex:         idx,
					CreationTimestamp: i.CreationTimestamp.Time,
					Rule:              &rule,
				})
			}
			return rules
		},
		opts.WithName("IngressRuleCollection")...,
	)

	index := krt.NewIndex(collection, func(rule *IngressRule) []string {
		host := rule.Rule.Host
		if host == "" {
			host = "*"
		}
		return []string{host}
	})

	return collection, index
}

type ServiceWithPorts struct {
	Name      string
	Namespace string
	Ports     []corev1.ServicePort
}

func (s ServiceWithPorts) ResourceName() string {
	return s.Namespace + "/" + s.Name
}

func ServicesWithPorts(
	services krt.Collection[*corev1.Service],
	opts krt.OptionsBuilder,
) krt.Collection[ServiceWithPorts] {
	return krt.NewCollection(
		services,
		func(ctx krt.HandlerContext, svc *corev1.Service) *ServiceWithPorts {
			return &ServiceWithPorts{
				Name:      svc.Name,
				Namespace: svc.Namespace,
				Ports:     svc.Spec.Ports,
			}
		},
		opts.WithName("ServiceWithPorts")...,
	)
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

func addressInSlice(addr string, list []string) bool {
	for _, v := range list {
		if v == addr {
			return true
		}
	}

	return false
}
