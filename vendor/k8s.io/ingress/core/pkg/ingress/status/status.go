/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package status

import (
	"fmt"
	"net"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/golang/glog"

	v1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"

	"k8s.io/ingress/core/pkg/ingress/annotations/class"
	"k8s.io/ingress/core/pkg/ingress/store"
	"k8s.io/ingress/core/pkg/k8s"
	"k8s.io/ingress/core/pkg/strings"
	"k8s.io/ingress/core/pkg/task"
)

const (
	updateInterval = 30 * time.Second
)

// Sync ...
type Sync interface {
	Run(stopCh <-chan struct{})
	Shutdown()
}

// Config ...
type Config struct {
	Client         clientset.Interface
	PublishService string
	IngressLister  store.IngressLister

	ElectionID string

	UpdateStatusOnShutdown bool

	DefaultIngressClass string
	IngressClass        string

	// CustomIngressStatus allows to set custom values in Ingress status
	CustomIngressStatus func(*extensions.Ingress) []v1.LoadBalancerIngress
}

// statusSync keeps the status IP in each Ingress rule updated executing a periodic check
// in all the defined rules. To simplify the process leader election is used so the update
// is executed only in one node (Ingress controllers can be scaled to more than one)
// If the controller is running with the flag --publish-service (with a valid service)
// the IP address behind the service is used, if not the source is the IP/s of the node/s
type statusSync struct {
	Config
	// pod contains runtime information about this pod
	pod *k8s.PodInfo

	elector *leaderelection.LeaderElector
	// workqueue used to keep in sync the status IP/s
	// in the Ingress rules
	syncQueue *task.Queue

	runLock *sync.Mutex
}

// Run starts the loop to keep the status in sync
func (s statusSync) Run(stopCh <-chan struct{}) {
	go wait.Forever(s.elector.Run, 0)
	go s.run()

	go s.syncQueue.Run(time.Second, stopCh)

	<-stopCh
}

// Shutdown stop the sync. In case the instance is the leader it will remove the current IP
// if there is no other instances running.
func (s statusSync) Shutdown() {
	go s.syncQueue.Shutdown()
	// remove IP from Ingress
	if !s.elector.IsLeader() {
		return
	}

	if !s.UpdateStatusOnShutdown {
		glog.Warningf("skipping update of status of Ingress rules")
		return
	}

	glog.Infof("updating status of Ingress rules (remove)")

	addrs, err := s.runningAddresses()
	if err != nil {
		glog.Errorf("error obtaining running IPs: %v", addrs)
		return
	}

	if len(addrs) > 1 {
		// leave the job to the next leader
		glog.Infof("leaving status update for next leader (%v)", len(addrs))
		return
	}

	if s.isRunningMultiplePods() {
		glog.V(2).Infof("skipping Ingress status update (multiple pods running - another one will be elected as master)")
		return
	}

	glog.Infof("removing address from ingress status (%v)", addrs)
	s.updateStatus([]v1.LoadBalancerIngress{})
}

func (s *statusSync) run() {
	err := wait.PollInfinite(updateInterval, func() (bool, error) {
		if s.syncQueue.IsShuttingDown() {
			return true, nil
		}
		// send a dummy object to the queue to force a sync
		s.syncQueue.Enqueue("dummy")
		return false, nil
	})
	if err != nil {
		glog.Errorf("error waiting shutdown: %v", err)
	}
}

func (s *statusSync) sync(key interface{}) error {
	s.runLock.Lock()
	defer s.runLock.Unlock()

	if s.syncQueue.IsShuttingDown() {
		glog.V(2).Infof("skipping Ingress status update (shutting down in progress)")
		return nil
	}

	if !s.elector.IsLeader() {
		glog.V(2).Infof("skipping Ingress status update (I am not the current leader)")
		return nil
	}

	addrs, err := s.runningAddresses()
	if err != nil {
		return err
	}
	s.updateStatus(sliceToStatus(addrs))

	return nil
}

// callback invoked function when a new leader is elected
func (s *statusSync) callback(leader string) {
	if s.syncQueue.IsShuttingDown() {
		return
	}

	glog.V(2).Infof("new leader elected (%v)", leader)
	if leader == s.pod.Name {
		glog.V(2).Infof("I am the new status update leader")
	}
}

func (s statusSync) keyfunc(input interface{}) (interface{}, error) {
	return input, nil
}

// NewStatusSyncer returns a new Sync instance
func NewStatusSyncer(config Config) Sync {
	pod, err := k8s.GetPodDetails(config.Client)
	if err != nil {
		glog.Fatalf("unexpected error obtaining pod information: %v", err)
	}

	st := statusSync{
		pod:     pod,
		runLock: &sync.Mutex{},
		Config:  config,
	}
	st.syncQueue = task.NewCustomTaskQueue(st.sync, st.keyfunc)

	// we need to use the defined ingress class to allow multiple leaders
	// in order to update information about ingress status
	electionID := fmt.Sprintf("%v-%v", config.ElectionID, config.DefaultIngressClass)
	if config.IngressClass != "" {
		electionID = fmt.Sprintf("%v-%v", config.ElectionID, config.IngressClass)
	}

	callbacks := leaderelection.LeaderCallbacks{
		OnStartedLeading: func(stop <-chan struct{}) {
			st.callback(pod.Name)
		},
		OnStoppedLeading: func() {
			st.callback("")
		},
	}

	broadcaster := record.NewBroadcaster()
	hostname, _ := os.Hostname()

	recorder := broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{
		Component: "ingress-leader-elector",
		Host:      hostname,
	})

	lock := resourcelock.ConfigMapLock{
		ConfigMapMeta: meta_v1.ObjectMeta{Namespace: pod.Namespace, Name: electionID},
		Client:        config.Client.Core(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity:      electionID,
			EventRecorder: recorder,
		},
	}

	le, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:          &lock,
		LeaseDuration: 30 * time.Second,
		RenewDeadline: 15 * time.Second,
		RetryPeriod:   5 * time.Second,
		Callbacks:     callbacks,
	})

	if err != nil {
		glog.Fatalf("unexpected error starting leader election: %v", err)
	}

	st.elector = le
	return st
}

// runningAddresses returns a list of IP addresses and/or FQDN where the
// ingress controller is currently running
func (s *statusSync) runningAddresses() ([]string, error) {
	if s.PublishService != "" {
		ns, name, _ := k8s.ParseNameNS(s.PublishService)
		svc, err := s.Client.Core().Services(ns).Get(name, meta_v1.GetOptions{})
		if err != nil {
			return nil, err
		}

		addrs := []string{}
		for _, ip := range svc.Status.LoadBalancer.Ingress {
			if ip.IP == "" {
				addrs = append(addrs, ip.Hostname)
			} else {
				addrs = append(addrs, ip.IP)
			}
		}

		return addrs, nil
	}

	// get information about all the pods running the ingress controller
	pods, err := s.Client.Core().Pods(s.pod.Namespace).List(meta_v1.ListOptions{
		LabelSelector: labels.SelectorFromSet(s.pod.Labels).String(),
	})
	if err != nil {
		return nil, err
	}

	addrs := []string{}
	for _, pod := range pods.Items {
		name := k8s.GetNodeIP(s.Client, pod.Spec.NodeName)
		if !strings.StringInSlice(name, addrs) {
			addrs = append(addrs, name)
		}
	}
	return addrs, nil
}

func (s *statusSync) isRunningMultiplePods() bool {
	pods, err := s.Client.Core().Pods(s.pod.Namespace).List(meta_v1.ListOptions{
		LabelSelector: labels.SelectorFromSet(s.pod.Labels).String(),
	})
	if err != nil {
		return false
	}

	return len(pods.Items) > 1
}

// sliceToStatus converts a slice of IP and/or hostnames to LoadBalancerIngress
func sliceToStatus(endpoints []string) []v1.LoadBalancerIngress {
	lbi := []v1.LoadBalancerIngress{}
	for _, ep := range endpoints {
		if net.ParseIP(ep) == nil {
			lbi = append(lbi, v1.LoadBalancerIngress{Hostname: ep})
		} else {
			lbi = append(lbi, v1.LoadBalancerIngress{IP: ep})
		}
	}

	sort.Sort(loadBalancerIngressByIP(lbi))
	return lbi
}

// updateStatus changes the status information of Ingress rules
// If the backend function CustomIngressStatus returns a value different
// of nil then it uses the returned value or the newIngressPoint values
func (s *statusSync) updateStatus(newIngressPoint []v1.LoadBalancerIngress) {
	ings := s.IngressLister.List()
	var wg sync.WaitGroup
	wg.Add(len(ings))
	for _, cur := range ings {
		ing := cur.(*extensions.Ingress)

		if !class.IsValid(ing, s.Config.IngressClass, s.Config.DefaultIngressClass) {
			wg.Done()
			continue
		}

		go func(wg *sync.WaitGroup, ing *extensions.Ingress) {
			defer wg.Done()
			ingClient := s.Client.Extensions().Ingresses(ing.Namespace)
			currIng, err := ingClient.Get(ing.Name, meta_v1.GetOptions{})
			if err != nil {
				glog.Errorf("unexpected error searching Ingress %v/%v: %v", ing.Namespace, ing.Name, err)
				return
			}

			addrs := newIngressPoint
			ca := s.CustomIngressStatus(currIng)
			if ca != nil {
				addrs = ca
			}

			curIPs := currIng.Status.LoadBalancer.Ingress
			sort.Sort(loadBalancerIngressByIP(curIPs))
			if ingressSliceEqual(addrs, curIPs) {
				glog.V(3).Infof("skipping update of Ingress %v/%v (no change)", currIng.Namespace, currIng.Name)
				return
			}

			glog.Infof("updating Ingress %v/%v status to %v", currIng.Namespace, currIng.Name, addrs)
			currIng.Status.LoadBalancer.Ingress = addrs
			_, err = ingClient.UpdateStatus(currIng)
			if err != nil {
				glog.Warningf("error updating ingress rule: %v", err)
			}
		}(&wg, ing)
	}

	wg.Wait()
}

func ingressSliceEqual(lhs, rhs []v1.LoadBalancerIngress) bool {
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

// loadBalancerIngressByIP sorts LoadBalancerIngress using the field IP
type loadBalancerIngressByIP []v1.LoadBalancerIngress

func (c loadBalancerIngressByIP) Len() int      { return len(c) }
func (c loadBalancerIngressByIP) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c loadBalancerIngressByIP) Less(i, j int) bool {
	return c[i].IP < c[j].IP
}
