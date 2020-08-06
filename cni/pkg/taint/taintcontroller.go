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
	clientset      client.Interface
	podWorkQueue   workqueue.RateLimitingInterface
	nodeWorkQueue  workqueue.RateLimitingInterface
	podController  []cache.Controller
	nodeController cache.Controller
	taintsetter    *TaintSetter
}

func NewTaintSetterController(ts *TaintSetter) (*Controller, error) {
	c := &Controller{
		clientset:     ts.Client,
		podWorkQueue:  workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		nodeWorkQueue: workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		taintsetter:   ts,
	}
	//construct a series of pod controller according to the configmaps' namespace and labelselector
	watchlists := make([]*cache.ListWatch, 0)
	for _, config := range ts.configs {
		temp := buildPodListWatch(*c, config)
		watchlists = append(watchlists, temp)
	}
	c.podController = []cache.Controller{}
	for _, listwatch := range watchlists {
		tempcontroller := buildPodController(c, listwatch)
		c.podController = append(c.podController, tempcontroller)
	}
	//construct a node controller watch on all nodes
	nodeListWatch := cache.NewFilteredListWatchFromClient(c.clientset.CoreV1().RESTClient(), "nodes", metav1.NamespaceAll, func(options *metav1.ListOptions) {
		return
	})
	c.nodeController = buildNodeControler(c, nodeListWatch)
	return c, nil
}

//build a listwatch based on the config
//monitoring on pod with namespace and labelselectors defined in configmap
func buildPodListWatch(c Controller, config ConfigSettings) *cache.ListWatch {
	podListWatch := cache.NewFilteredListWatchFromClient(
		c.clientset.CoreV1().RESTClient(),
		"pods",
		config.namespace,
		func(options *metav1.ListOptions) {
			options.LabelSelector = config.LabelSelectors
		},
	)
	return podListWatch
}

//build a pod controller by listwatch function
//add : add readiness taint if not added yet, add it to workqueue
//update: add it to workquueue
//remove: retaint the node
func buildPodController(c *Controller, listwatch *cache.ListWatch) cache.Controller {
	_, tempcontroller := cache.NewInformer(listwatch, &v1.Pod{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(newObj interface{}) {
			err := validTaintByPod(newObj, c)
			if err != nil {
				log.Errorf("Error in pod registration and validation.")
				return
			}
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
	return tempcontroller
}
func buildNodeControler(c *Controller, nodeListWatch *cache.ListWatch) cache.Controller {
	_, controller := cache.NewInformer(nodeListWatch, &v1.Node{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(newObj interface{}) {
			node, ok := newObj.(*v1.Node)
			if !ok {
				log.Errorf("Error decoding object, invalid type.")
				return
			}
			err := c.taintsetter.AddReadinessTaint(node)
			if err != nil {
				log.Errorf("Error in readiness taint add")
				return
			}
			c.nodeWorkQueue.AddRateLimited(newObj)
		},
		UpdateFunc: func(_, newObj interface{}) {
			c.nodeWorkQueue.AddRateLimited(newObj)
		},
	})
	return controller
}
func validTaintByPod(obj interface{}, c *Controller) error {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		log.Errorf("Error decoding object, invalid type.")
		return fmt.Errorf("Error decoding object, invalid type.")
	}
	node, err := c.taintsetter.GetNodeByPod(pod)
	if err != nil {
		return err
	}
	err = c.taintsetter.AddReadinessTaint(node)
	if err != nil {
		return err
	}
	return nil
}
func reTaintNodeByPod(obj interface{}, c *Controller) error {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		log.Errorf("Error decoding object, invalid type.")
		return fmt.Errorf("Error decoding object, invalid type.")
	}
	node, err := c.taintsetter.GetNodeByPod(pod)
	if err != nil {
		return err
	}
	err = c.taintsetter.AddReadinessTaint(node)
	if err != nil {
		return err
	}
	return nil
}

//controller will run all of the critical pod controllers and node controllers, process node and pod in every second
func (tc *Controller) Run(stopCh <-chan struct{}) {
	for _, podcontroller := range tc.podController {
		go podcontroller.Run(stopCh)
		if !cache.WaitForCacheSync(stopCh, podcontroller.HasSynced) {
			runtime.HandleError(fmt.Errorf("timed out waiting for caches to sync pod controller"))
			return
		}
	}
	go tc.nodeController.Run(stopCh)
	if !cache.WaitForCacheSync(stopCh, tc.nodeController.HasSynced) {
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
		if err == nil {
			log.Debugf("Removing %s/%s from work queue", pod.Namespace, pod.Name)
			tc.podWorkQueue.Forget(obj)
		} else if tc.podWorkQueue.NumRequeues(obj) < 50 {
			log.Errorf("Error: %s", err.Error())
			log.Infof("Re-adding %s/%s to work queue", pod.Namespace, pod.Name)
			tc.podWorkQueue.AddRateLimited(obj)
		} else {
			log.Infof("Requeue limit reached, removing %s/%s", pod.Namespace, pod.Name)
			tc.podWorkQueue.Forget(obj)
			runtime.HandleError(err)
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
	//check for pod readiness
	//if pod is ready, check all of critical labels in the corresponding node, if all of them are ready, untaint the node
	//if pod is not ready or some critical pods are not ready, taint the node
	if podutils.IsPodReady(pod) {
		err := tc.taintsetter.ProcessReadyPod(pod)
		errorHandler(obj, pod, err)
	} else {
		err := tc.taintsetter.ProcessUnReadyPod(pod)
		errorHandler(obj, pod, err)
	}
	// Return true to let the loop process the next item
	return true
}

//check node readiess if node is ready, remove taint and forget obj, retry 50 times when taint have error
//if node is not ready, taint it and retry
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
	err := tc.taintsetter.ProcessNode(node)
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
