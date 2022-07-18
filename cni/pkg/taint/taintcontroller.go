// Copyright 2020 Istio Authors
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

package taint

import (
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	client "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/kubectl/pkg/util/podutils"

	"istio.io/pkg/log"
)

type Controller struct {
	clientset     client.Interface
	podWorkQueue  workqueue.RateLimitingInterface
	nodeWorkQueue workqueue.RateLimitingInterface
	podController []cache.Controller
	// map[namespace][label_selector]store first key denotes the namespace of pods where cache store should fetch on
	// and the second key is the label selector which set as a list option during list watch
	// thus the store inside the map is caching pods from namespace and label selector  given first and second key
	cachedPodsStore map[string]map[string]cache.Store // store sync with list watch given namespace and labelselector
	nodeController  cache.Controller
	nodeStore       cache.Store
	taintsetter     *Setter
}

func NewTaintSetterController(ts *Setter) (*Controller, error) {
	c := &Controller{
		clientset:       ts.Client,
		podWorkQueue:    workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		nodeWorkQueue:   workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		taintsetter:     ts,
		cachedPodsStore: make(map[string]map[string]cache.Store),
	}
	// construct a series of pod controller according to the configmaps' namespace and labelselector
	c.podController = []cache.Controller{}
	for _, config := range ts.configs {
		label := config.LabelSelector
		podListWatch := cache.NewFilteredListWatchFromClient(
			c.clientset.CoreV1().RESTClient(),
			"pods",
			config.Namespace,
			func(options *metav1.ListOptions) {
				options.LabelSelector = label
			},
		)
		tempcontroller := buildPodController(c, config, podListWatch)
		c.podController = append(c.podController, tempcontroller)
	}
	nodeListWatch := cache.NewFilteredListWatchFromClient(c.clientset.CoreV1().RESTClient(), "nodes", metav1.NamespaceAll, func(options *metav1.ListOptions) {
	})
	c.nodeStore, c.nodeController = buildNodeController(c, nodeListWatch)
	return c, nil
}

// build a listwatch based on the config
// monitoring on pod with namespace and label-selectors defined in configmap
// and add store to cache given namespace and label-selector,
// return a controller built by listwatch function
// add : add it to workqueue
// update: add it to workqueue
// remove: retaint the node
func buildPodController(c *Controller, config ConfigSettings, source cache.ListerWatcher) cache.Controller {
	tempstore, tempcontroller := cache.NewInformer(source, &v1.Pod{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(newObj interface{}) {
			// remove filter condition will introduce a lot of error handling in workqueue
			c.podWorkQueue.AddRateLimited(newObj)
		},
		UpdateFunc: func(_, newObj interface{}) {
			c.podWorkQueue.AddRateLimited(newObj)
		},
		DeleteFunc: func(newObj interface{}) {
			err := reTaintNodeByPod(newObj, c)
			if err != nil {
				log.Errorf("Error in pod remove process.")
				return
			}
		},
	})
	if _, ok := c.cachedPodsStore[config.Namespace]; !ok {
		c.cachedPodsStore[config.Namespace] = make(map[string]cache.Store)
	}
	c.cachedPodsStore[config.Namespace][config.LabelSelector] = tempstore
	return tempcontroller
}

func buildNodeController(c *Controller, nodeListWatch cache.ListerWatcher) (cache.Store, cache.Controller) {
	store, controller := cache.NewInformer(nodeListWatch, &v1.Node{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(newObj interface{}) {
			_, ok := newObj.(*v1.Node)
			if !ok {
				log.Errorf("Error decoding object, invalid type.")
				return
			}
			c.nodeWorkQueue.AddRateLimited(newObj)
		},
		UpdateFunc: func(_, newObj interface{}) {
			c.nodeWorkQueue.AddRateLimited(newObj)
		},
	})
	return store, controller
}

func reTaintNodeByPod(obj interface{}, c *Controller) error {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		log.Errorf("error decoding object, invalid type.")
		return fmt.Errorf("error decoding object, invalid type")
	}
	node, err := c.getNodeByPod(pod)
	if err != nil {
		return err
	}
	err = c.taintsetter.AddReadinessTaint(node)
	if err != nil {
		return err
	}
	return nil
}

// controller will run all of the critical pod controllers and node controllers, process node and pod in every second
func (tc *Controller) Run(stopCh <-chan struct{}) {
	for _, podController := range tc.podController {
		go podController.Run(stopCh)
		// wait for cache sync up
		err := wait.Poll(100*time.Millisecond, 60*time.Second, func() (bool, error) {
			return podController.HasSynced(), nil
		})
		if err != nil || !podController.HasSynced() {
			runtime.HandleError(fmt.Errorf("timed out waiting for caches to sync pod controller, err: %s", err))
			return
		}
	}
	go tc.nodeController.Run(stopCh)
	// wait for cache sync up
	err := wait.Poll(100*time.Millisecond, 60*time.Second, func() (bool, error) {
		return tc.nodeController.HasSynced(), nil
	})
	if err != nil || !tc.nodeController.HasSynced() {
		runtime.HandleError(fmt.Errorf("timed out waiting for caches to sync node controller"))
		return
	}
	// This will run the func every 1 second until stopCh is sent
	go wait.Until(
		func() {
			// Runs processNextItem in a loop, if it returns false it will
			// be restarted by wait.Until unless stopCh is sent.
			for tc.processNextPod() {
			}
		},
		time.Second,
		stopCh)
	// This will run the func every 1 second until stopCh is sent
	go wait.Until(
		func() {
			// Runs processNextItem in a loop, if it returns false it will
			// be restarted by wait.Until unless stopCh is sent.
			for tc.processNextNode() {
			}
		},
		time.Second,
		stopCh)
	<-stopCh
	log.Infof("stop taint readiness controller")
}

func (tc *Controller) processNextPod() bool {
	errorHandler := func(obj interface{}, pod *v1.Pod, err error) {
		podName := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
		if err == nil {
			log.Debugf("Removing %s from work queue", podName)
			tc.podWorkQueue.Forget(obj)
		} else {
			log.Errorf("Error while processing pod %s: %s", podName, err.Error())
			if tc.podWorkQueue.NumRequeues(obj) < 50 {
				log.Infof("Re-adding %s to work queue", podName)
				tc.podWorkQueue.AddRateLimited(obj)
			} else {
				log.Infof("Requeue limit reached, removing %s", podName)
				tc.podWorkQueue.Forget(obj)
				runtime.HandleError(err)
			}
		}
	}
	obj, quit := tc.podWorkQueue.Get()
	if quit {
		// Exit permanently
		return false
	}
	defer tc.podWorkQueue.Done(obj)
	pod, ok := obj.(*v1.Pod)
	if !ok {
		log.Errorf("Error decoding object, invalid type. Dropping.")
		tc.podWorkQueue.Forget(obj)
		// Short-circuit on this item, but return true to keep
		// processing.
		return true
	}
	// check for pod readiness
	// if pod is ready, check all of critical labels in the corresponding node, if all of them are ready, untaint the node
	// if pod is not ready or some critical pods are not ready, taint the node
	if podutils.IsPodReady(pod) {
		err := tc.processReadyPod(pod)
		errorHandler(obj, pod, err)
	} else {
		err := tc.processUnreadyPod(pod)
		errorHandler(obj, pod, err)
	}
	// Return true to let the loop process the next item
	return true
}

// list all candidate pods given node name, namespace and selector using cached storage
func (tc Controller) listCandidatePods(nodeName string, namespace string, selector string) []*v1.Pod {
	if _, ok := tc.cachedPodsStore[namespace]; !ok {
		return nil
	}
	if _, ok := tc.cachedPodsStore[namespace][selector]; !ok {
		return nil
	}
	podList := make([]*v1.Pod, 0)
	for _, item := range tc.cachedPodsStore[namespace][selector].List() {
		pod, ok := item.(*v1.Pod)
		if !ok {
			continue
		}
		if pod.Spec.NodeName == nodeName && podutils.IsPodReady(pod) {
			podList = append(podList, pod)
		}
	}
	return podList
}

func (tc Controller) CheckNodeReadiness(node v1.Node) bool {
	if tc.taintsetter.configs == nil || len(tc.taintsetter.configs) == 0 {
		return true
	}
	for namespace, labelMap := range tc.cachedPodsStore {
		for label := range labelMap {
			if len(tc.listCandidatePods(node.GetName(), namespace, label)) == 0 {
				return false
			}
		}
	}
	return true
}

// if pod is ready, check all related pod in the corresponding node, if any critical pod is unready, it should be tainted
func (tc Controller) processReadyPod(pod *v1.Pod) error {
	node, err := tc.getNodeByPod(pod)
	if err != nil {
		return fmt.Errorf("cannot get node by  %s in namespace %s : %s", pod.Name, pod.Namespace, err)
	}
	if GetNodeLatestReadiness(*node) && tc.CheckNodeReadiness(*node) {
		err = tc.taintsetter.RemoveReadinessTaint(node)
		if err != nil {
			return fmt.Errorf("cannot remove node readiness taint: %s ", err.Error())
		}
		log.Infof("Readiness Taint removed to the node %v because all pods inside is ready", node.Name)
		return nil
	}
	if tc.taintsetter.HasReadinessTaint(node) {
		log.Infof("node %v has readiness taint because pod %v in namespace %v is not ready", node.Name, pod.Name, pod.Namespace)
		return nil
	}
	err = tc.taintsetter.AddReadinessTaint(node)
	if err != nil {
		return fmt.Errorf("cannot add taint to node: %s", err.Error())
	}
	log.Infof("node %v add readiness taint because some other pods is not ready", node.Name)
	return nil
}

// if pod is unready, it should be tainted
func (tc Controller) processUnreadyPod(pod *v1.Pod) error {
	node, err := tc.getNodeByPod(pod)
	if err != nil {
		return fmt.Errorf("cannot get node by  %s in namespace %s : %s", pod.Name, pod.Namespace, err)
	}
	if tc.taintsetter.HasReadinessTaint(node) {
		log.Infof("node %v has readiness taint because pod %v in namespace %v is not ready", node.Name, pod.Name, pod.Namespace)
		return nil
	}
	err = tc.taintsetter.AddReadinessTaint(node)
	if err != nil {
		return fmt.Errorf("cannot add taint to node: %s", err.Error())
	}
	log.Infof("node %+v add readiness taint because pod %v in namespace %v is not ready", node.Name, pod.Name, pod.Namespace)
	return nil
}

func (tc Controller) ListAllNode() []*v1.Node {
	items := tc.nodeStore.List()
	nodes := make([]*v1.Node, 0)
	for _, item := range items {
		node, ok := item.(*v1.Node)
		if !ok {
			continue
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func (tc Controller) RegisterTaints() {
	nodes := tc.ListAllNode()
	for _, node := range nodes {
		err := tc.taintsetter.AddReadinessTaint(node)
		if err != nil {
			log.Fatalf("error cannot taint node: %+v", err)
		}
	}
}

// if node is ready, check all of its critical labels and if all of them are ready , remove readiness taint
// else taint it
func (tc Controller) ProcessNode(node *v1.Node) error {
	if GetNodeLatestReadiness(*node) {
		if tc.CheckNodeReadiness(*node) {
			err := tc.taintsetter.RemoveReadinessTaint(node)
			if err != nil {
				return fmt.Errorf("cannot remove readiness taint in node: %s", node.Name)
			}
			log.Infof("node %+v remove readiness taint because it is ready", node.Name)
		} else {
			if tc.taintsetter.HasReadinessTaint(node) {
				log.Infof("node %v has readiness taint because it is not ready", node.Name)
			} else {
				err := tc.taintsetter.AddReadinessTaint(node)
				if err != nil {
					return fmt.Errorf("cannot add readiness taint in node: %s", node.Name)
				}
				log.Infof("node %v add readiness taint because it is not ready", node.Name)
			}
		}
	} else {
		if tc.taintsetter.HasReadinessTaint(node) {
			log.Infof("node %v has readiness taint because it is not ready", node.Name)
		} else {
			err := tc.taintsetter.AddReadinessTaint(node)
			if err != nil {
				return fmt.Errorf("cannot add readiness taint in node: %s", node.Name)
			}
			log.Infof("node %v add readiness taint because it is not ready", node.Name)
		}
	}
	return nil
}

func (tc Controller) getNodeByPod(pod *v1.Pod) (*v1.Node, error) {
	if pod == nil {
		return nil, fmt.Errorf("cannot handle a nil pod")
	}
	obj, exists, err := tc.nodeStore.GetByKey(pod.Spec.NodeName)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("cannot find node given pod %s in namespace %s", pod.Name, pod.Namespace)
	}
	node, ok := obj.(*v1.Node)
	if !ok {
		return nil, fmt.Errorf("cannot convert given pod %s in namespace %s's  node", pod.Name, pod.Namespace)
	}
	return node, nil
}

// check node readiess if node is ready, remove taint and forget obj, retry 50 times when taint have error
// if node is not ready, taint it and retry
func (tc *Controller) processNextNode() bool {
	obj, quit := tc.nodeWorkQueue.Get()
	if quit {
		// Exit permanently
		return false
	}
	defer tc.nodeWorkQueue.Done(obj)
	node, ok := obj.(*v1.Node)
	if !ok {
		log.Errorf("Error decoding object, invalid type. Dropping.")
		tc.nodeWorkQueue.Forget(obj)
		// Short-circuit on this item, but return true to keep
		// processing.
		return true
	}
	err := tc.ProcessNode(node)
	if err == nil {
		log.Debugf("Removing %s from work queue", node.Name)
		tc.nodeWorkQueue.Forget(obj)
	} else if tc.nodeWorkQueue.NumRequeues(obj) < 50 {
		log.Infof("Re-adding %s to work queue", node.Name)
		tc.nodeWorkQueue.AddRateLimited(obj)
	} else {
		tc.nodeWorkQueue.Forget(obj)
	}
	// Return true to let the loop process the next item
	return true
}
