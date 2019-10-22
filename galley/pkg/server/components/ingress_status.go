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

package components

import (
	"context"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	coreV1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"

	meshconfigapi "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/galley/pkg/server/settings"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	queue2 "istio.io/istio/pkg/queue"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

const (
	updateInterval    = 60 * time.Second
	ingressElectionID = "istio-ingress-controller-leader"
	electorTTL        = 30 * time.Second
)

type IngressStatusSyncer struct {
	client kubernetes.Interface

	// Name of service (ingressgateway default) to find the IP
	ingressService string
	mode           meshconfigapi.MeshConfig_IngressControllerMode
	ingressClass   string

	queue    queue2.Instance
	informer cache.SharedIndexInformer
	elector  *leaderelection.LeaderElector

	informerStopCh chan struct{}
	electorCancel  context.CancelFunc

	ingressNamespace string
}

var podNameVar = env.RegisterStringVar("POD_NAME", "", "")

func NewIngressStatusSyncer(client kubernetes.Interface,
	args *settings.Args) *IngressStatusSyncer {
	if !args.EnableServiceDiscovery {
		return nil
	}

	if client == nil {
		scope.Errorf("no kube client provided")
		return nil
	}

	meshConfigCache, err := newMeshConfigCache(args.MeshConfigFile)
	if err != nil {
		scope.Errorf("status syncer: mesh config cache %s", err)
		return nil
	}
	meshConfig := meshConfigCache.Get()

	mode := meshConfig.GetIngressControllerMode()
	ingressClass := meshConfig.GetIngressClass()
	electionID := fmt.Sprintf("%v-%v", ingressElectionID, ingressClass)

	// queue requires a time duration for a retry delay after a handler error
	queue := queue2.NewQueue(1 * time.Second)

	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(opts metaV1.ListOptions) (runtime.Object, error) {
				return client.ExtensionsV1beta1().Ingresses(args.WatchedNamespace).List(opts)
			},
			WatchFunc: func(opts metaV1.ListOptions) (watch.Interface, error) {
				return client.ExtensionsV1beta1().Ingresses(args.WatchedNamespace).Watch(opts)
			},
		},
		&v1beta1.Ingress{}, args.ResyncPeriod, cache.Indexers{},
	)

	st := IngressStatusSyncer{
		client:           client,
		informer:         informer,
		queue:            queue,
		mode:             mode,
		ingressClass:     ingressClass,
		ingressService:   meshConfig.IngressService,
		ingressNamespace: args.IstioIngressNamespace,
	}

	st.elector, err = st.buildElector(st, electionID)
	if err != nil {
		scope.Errorf("unexpected error starting leader election: %v", err)
		return nil
	}

	return &st
}

func (i *IngressStatusSyncer) Start() (err error) {
	i.informerStopCh = make(chan struct{})
	go i.informer.Run(i.informerStopCh)

	ctx, cancel := context.WithCancel(context.Background())
	i.electorCancel = cancel
	go i.elector.Run(ctx)

	return nil
}

func (i *IngressStatusSyncer) Stop() {
	if i.informerStopCh != nil {
		close(i.informerStopCh)
		i.informerStopCh = nil
	}

	if i.electorCancel != nil {
		i.electorCancel()
		i.electorCancel = nil
	}
}

func (i *IngressStatusSyncer) buildElector(st IngressStatusSyncer,
	electionID string) (*leaderelection.LeaderElector, error) {
	callbacks := leaderelection.LeaderCallbacks{
		OnStartedLeading: func(ctx context.Context) {
			scope.Infof("I am the new status update leader")
			go st.queue.Run(ctx.Done())
			go st.runUpdateStatus(ctx)
		},
		OnStoppedLeading: func() {
			scope.Infof("I am not status update leader anymore")
		},
		OnNewLeader: func(identity string) {
			scope.Infof("New leader elected: %v", identity)
		},
	}

	broadcaster := record.NewBroadcaster()
	hostname, _ := os.Hostname()

	recorder := broadcaster.NewRecorder(scheme.Scheme, coreV1.EventSource{
		Component: "ingress-leader-elector",
		Host:      hostname,
	})

	lock := resourcelock.ConfigMapLock{
		ConfigMapMeta: metaV1.ObjectMeta{Namespace: st.ingressNamespace, Name: electionID},
		Client:        st.client.CoreV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity:      podNameVar.Get(),
			EventRecorder: recorder,
		},
	}

	le, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:          &lock,
		LeaseDuration: electorTTL,
		RenewDeadline: electorTTL / 2,
		RetryPeriod:   electorTTL / 4,
		Callbacks:     callbacks,
	})

	if err != nil {
		return nil, err
	}

	return le, nil
}

func (i *IngressStatusSyncer) onEvent() error {
	addrs, err := i.runningAddresses(i.ingressNamespace)
	if err != nil {
		return err
	}

	return i.updateStatus(sliceToStatus(addrs))
}

func (i *IngressStatusSyncer) runUpdateStatus(ctx context.Context) {
	if _, err := i.runningAddresses(i.ingressNamespace); err != nil {
		scope.Warna("Missing ingress, skip status updates")
		err = wait.PollUntil(10*time.Second, func() (bool, error) {
			if sa, err := i.runningAddresses(i.ingressNamespace); err != nil || len(sa) == 0 {
				return false, nil
			}
			return true, nil
		}, ctx.Done())
		if err != nil {
			log.Warna("Error waiting for ingress")
			return
		}
	}
	err := wait.PollUntil(updateInterval, func() (bool, error) {
		i.queue.Push(i.onEvent)
		return false, nil
	}, ctx.Done())

	if err != nil {
		log.Errorf("Stop requested")
	}
}

// runningAddresses returns a list of IP addresses and/or FQDN where the
// ingress controller is currently running
func (i *IngressStatusSyncer) runningAddresses(ingressNs string) ([]string, error) {
	addrs := make([]string, 0)

	if i.ingressService != "" {
		svc, err := i.client.CoreV1().Services(ingressNs).Get(i.ingressService, metaV1.GetOptions{})
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
	pods, err := i.client.CoreV1().Pods(ingressNs).List(metaV1.ListOptions{
		// TODO: make it a const or maybe setting ( unless we remove k8s ingress support first)
		LabelSelector: labels.SelectorFromSet(map[string]string{"app": "ingressgateway"}).String(),
	})
	if err != nil {
		return nil, err
	}

	for _, pod := range pods.Items {
		// only Running pods are valid
		if pod.Status.Phase != coreV1.PodRunning {
			continue
		}

		// Find node external IP
		node, err := i.client.CoreV1().Nodes().Get(pod.Spec.NodeName, metaV1.GetOptions{})
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

// updateStatus updates ingress status with the list of IP
func (i *IngressStatusSyncer) updateStatus(status []coreV1.LoadBalancerIngress) error {
	ingressStore := i.informer.GetStore()
	for _, obj := range ingressStore.List() {
		currIng := obj.(*v1beta1.Ingress)

		var annotation string
		if currIng != nil && len(currIng.GetAnnotations()) != 0 {
			annotation = currIng.GetAnnotations()[kube.IngressClassAnnotation]
		}

		if !shouldUpdateStatus(i.mode, annotation, i.ingressClass) {
			continue
		}

		curIPs := currIng.Status.LoadBalancer.Ingress
		sort.SliceStable(status, lessLoadBalancerIngress(status))
		sort.SliceStable(curIPs, lessLoadBalancerIngress(curIPs))

		if ingressSliceEqual(status, curIPs) {
			scope.Infof("skipping update of Ingress %v/%v (no change)", currIng.Namespace, currIng.Name)
			return nil
		}

		currIng.Status.LoadBalancer.Ingress = status

		ingClient := i.client.ExtensionsV1beta1().Ingresses(currIng.Namespace)
		_, err := ingClient.UpdateStatus(currIng)
		if err != nil {
			scope.Warnf("error updating ingress status: %v", err)
		}
	}

	return nil
}

func addressInSlice(addr string, list []string) bool {
	for _, v := range list {
		if v == addr {
			return true
		}
	}

	return false
}

func shouldUpdateStatus(mode meshconfigapi.MeshConfig_IngressControllerMode,
	annotation, ingressClass string) bool {

	switch mode {
	case meshconfigapi.MeshConfig_DEFAULT:
		return annotation == ""
	case meshconfigapi.MeshConfig_STRICT:
		return annotation == ingressClass
	}

	return false
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
