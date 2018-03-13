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

package ingress

import (
	"errors"
	"fmt"
	"os"
	"time"

	api_v1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/log"
)

const (
	ingressElectionID = "istio-ingress-controller-leader"
	updateInterval    = 60 * time.Second
)

// StatusSyncer keeps the status IP in each Ingress resource updated
type StatusSyncer struct {
	Client kubernetes.Interface

	DefaultIngressClass string
	IngressClass        string

	IngressNameSpace string
	IngressService   string
	Pod              *api_v1.Pod

	ElectionID string

	Queue kube.Queue

	Informer cache.SharedIndexInformer

	Elector *leaderelection.LeaderElector

	Handler *kube.ChainHandler
}

// Run the syncer until stopCh is closed
func (s *StatusSyncer) Run(stopCh <-chan struct{}) {
	go s.Informer.Run(stopCh)
	go s.Elector.Run()
	<-stopCh
}

// updateStatus updates ingress status with the list of IP
func (s *StatusSyncer) updateStatus(status []api_v1.LoadBalancerIngress) error {
	ingressStore := s.Informer.GetStore()
	for _, obj := range ingressStore.List() {
		currIng := obj.(*v1beta1.Ingress)
		currIng.Status.LoadBalancer.Ingress = status

		ingClient := s.Client.ExtensionsV1beta1().Ingresses(currIng.Namespace)
		_, err := ingClient.UpdateStatus(currIng)
		if err != nil {
			log.Warnf("error updating ingress status: %v", err)
		}
	}

	return nil
}

// runningAddresses returns a list of IP addresses and/or FQDN where the
// ingress controller is currently running
func (s *StatusSyncer) runningAddresses() ([]api_v1.LoadBalancerIngress, error) {
	addrs := []api_v1.LoadBalancerIngress{}

	if s.IngressService != "" {
		svc, err := s.Client.CoreV1().Services(s.IngressNameSpace).Get(s.IngressService, meta_v1.GetOptions{})
		if err != nil {
			return nil, err
		}

		return svc.Status.LoadBalancer.Ingress, nil
	}

	// get information about all the pods running the ingress controller
	pods, err := s.Client.CoreV1().Pods(s.Pod.GetNamespace()).List(meta_v1.ListOptions{
		LabelSelector: labels.SelectorFromSet(s.Pod.GetLabels()).String(),
	})
	if err != nil {
		return nil, err
	}

	for _, pod := range pods.Items {
		// only Running pods are valid
		if pod.Status.Phase != api_v1.PodRunning {
			continue
		}

		// Find node external IP
		node, err := s.Client.CoreV1().Nodes().Get(pod.Spec.NodeName, meta_v1.GetOptions{})
		if err != nil {
			continue
		}

		for _, address := range node.Status.Addresses {
			if address.Type == api_v1.NodeExternalIP {
				if address.Address != "" {
					addrs = append(addrs, api_v1.LoadBalancerIngress{IP: address.Address})
					break
				}
			}
		}
	}

	return addrs, nil
}

// NewStatusSyncer creates a new instance
func NewStatusSyncer(mesh *meshconfig.MeshConfig,
	client kubernetes.Interface,
	ingressNamespace string,
	options kube.ControllerOptions) (*StatusSyncer, error) {

	podName, exists := os.LookupEnv("POD_NAME")
	if !exists {
		return nil, errors.New("POD_NAME environment variable must be defined")
	}
	podNamespace, exists := os.LookupEnv("POD_NAMESPACE")
	if !exists {
		return nil, errors.New("POD_NAMESPACE environment variable must be defined")
	}

	pod, _ := client.CoreV1().Pods(podNamespace).Get(podName, meta_v1.GetOptions{})
	if pod == nil {
		return nil, fmt.Errorf("unable to get POD information")
	}

	handler := &kube.ChainHandler{}
	// queue requires a time duration for a retry delay after a handler error
	queue := kube.NewQueue(1 * time.Second)

	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(opts meta_v1.ListOptions) (runtime.Object, error) {
				return client.ExtensionsV1beta1().Ingresses(options.WatchedNamespace).List(opts)
			},
			WatchFunc: func(opts meta_v1.ListOptions) (watch.Interface, error) {
				return client.ExtensionsV1beta1().Ingresses(options.WatchedNamespace).Watch(opts)
			},
		},
		&v1beta1.Ingress{}, options.ResyncPeriod, cache.Indexers{},
	)

	ingressClass, defaultIngressClass := convertIngressControllerMode(mesh.IngressControllerMode, mesh.IngressClass)

	st := StatusSyncer{
		Client:              client,
		DefaultIngressClass: defaultIngressClass,
		IngressClass:        ingressClass,
		Informer:            informer,
		ElectionID:          ingressElectionID,
		Queue:               queue,
		IngressNameSpace:    ingressNamespace,
		IngressService:      mesh.IngressService,
		Pod:                 pod,
	}

	callbacks := leaderelection.LeaderCallbacks{
		OnStartedLeading: func(stop <-chan struct{}) {
			log.Infof("I am the new status update leader")
			go st.Queue.Run(stop)
			err := wait.PollUntil(updateInterval, func() (bool, error) {
				st.Queue.Push(kube.NewTask(st.Handler.Apply, "Start leading", model.EventUpdate))
				return false, nil
			}, stop)

			if err != nil {
				log.Errorf("Stop requested")
			}
		},
		OnStoppedLeading: func() {
			log.Infof("I am not status update leader anymore")
		},
		OnNewLeader: func(identity string) {
			log.Infof("New leader elected: %v", identity)
		},
	}

	broadcaster := record.NewBroadcaster()
	hostname, _ := os.Hostname()

	recorder := broadcaster.NewRecorder(scheme.Scheme, api_v1.EventSource{
		Component: "ingress-leader-elector",
		Host:      hostname,
	})

	lock := resourcelock.ConfigMapLock{
		ConfigMapMeta: meta_v1.ObjectMeta{Namespace: podNamespace, Name: ingressElectionID},
		Client:        client.CoreV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity:      podName,
			EventRecorder: recorder,
		},
	}

	ttl := 30 * time.Second
	le, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:          &lock,
		LeaseDuration: ttl,
		RenewDeadline: ttl / 2,
		RetryPeriod:   ttl / 4,
		Callbacks:     callbacks,
	})

	if err != nil {
		log.Errorf("unexpected error starting leader election: %v", err)
	}

	st.Elector = le

	// Register handler at the beginning
	handler.Append(func(obj interface{}, event model.Event) error {
		addrs, err := st.runningAddresses()
		if err != nil {
			return err
		}

		return st.updateStatus(addrs)
	})

	st.Handler = handler

	return &st, nil
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
