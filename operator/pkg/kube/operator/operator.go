// Copyright 2018 Istio Authors
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

package operator

import (
	"fmt"
	"strings"
	"time"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"github.com/ghodss/yaml"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
        apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
        "k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
        apiclient "k8s.io/client-go/kubernetes"
        "k8s.io/client-go/rest"
	"k8s.io/client-go/util/workqueue"
)

const (
	operatorLabel = "istio-operator"
	maxRetries = 5
)

var (
        runtimeScheme = runtime.NewScheme()
        codecs        = serializer.NewCodecFactory(runtimeScheme)
        deserializer  = codecs.UniversalDeserializer()
)

// LoadKubeConfig is a unit test override variable for loading the k8s config.
// DO NOT USE - TEST ONLY.
var LoadKubeConfig = clientcmd.Load

// CreateInterfaceFromClusterConfig is a unit test override variable for interface create.
// DO NOT USE - TEST ONLY.
var CreateInterfaceFromClusterConfig = kube.CreateInterfaceFromClusterConfig

// addConfigMapCallback prototype for unit testing.
// DO NOT USE - TEST ONLY.
type addConfigMapCallback func(*corev1.ConfigMap) error

// removeConfigMapCallback prototype for unit testing.
// DO NOT USE - TEST ONLY.
type removeConfigMapCallback func(string) error

// addConfigMapCallback is called during unit tetsts to verify this controller is working properly.
// DO NOT USE - TEST ONLY.
//var addConfigMapCallback = nil

// rmoveConfigMapCallback is called during unit tetsts to verify this controller is working properly.
// DO NOT USE - TEST ONLY.
//var removeConfigMapCallback = nil

// Controller is the controller implementation for ConfigMap resources.
type Controller struct {
        clientExt *apiextensionsclient.Clientset
	kuberest  *rest.Config
	namespace string
	queue     workqueue.RateLimitingInterface
	informer  cache.SharedIndexInformer
}

func init() {
	corev1.AddToScheme(runtimeScheme)
        apiextensionsv1beta1.AddToScheme(runtimeScheme)
}

// NewController returns a new operator controller.
func NewController(
	restConfig *rest.Config,
	namespace string) *Controller {

	kubeclientset, _ := apiclient.NewForConfig(restConfig)

	clientExt, _ := apiextensionsclient.NewForConfig(restConfig)

	configMapInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(opts meta_v1.ListOptions) (runtime.Object, error) {
				opts.LabelSelector = operatorLabel + "=true"
				return kubeclientset.CoreV1().ConfigMaps(namespace).List(opts)
			},
			WatchFunc: func(opts meta_v1.ListOptions) (watch.Interface, error) {
				opts.LabelSelector = operatorLabel + "=true"
				opts.Watch = true
				return kubeclientset.CoreV1().ConfigMaps(namespace).Watch(opts)
			},
		},
		&corev1.ConfigMap{}, 0, cache.Indexers{},
	)

	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	controller := &Controller{
		clientExt:	clientExt,
		namespace:      namespace,
		informer:       configMapInformer,
		queue:          queue,
	}

	configMapInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			log.Infof("Processing add: %s", key)
			if err == nil {
				queue.Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			log.Infof("Processing delete: %s", key)
			if err == nil {
				queue.Add(key)
			}
		},
	})

	return controller
}

// Run starts the controller until it receves a message over stopCh.
func (c *Controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	go c.informer.Run(stopCh)

	// Wait for the caches to be synced before starting workers
	log.Info("Waiting for informer caches to sync")
	if !cache.WaitForCacheSync(stopCh, c.informer.HasSynced) {
		utilruntime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}
	wait.Until(c.runWorker, 5*time.Second, stopCh)
}

// RunWorker executes the processing loop.
func (c *Controller) runWorker() {
	for c.processNextItem() {
		// continue looping
	}
}

// processNextItem retrieves a configmap event from the cache queue.
func (c *Controller) processNextItem() bool {
	configMapName, quit := c.queue.Get()

	if quit {
		return false
	}
	defer c.queue.Done(configMapName)

	err := c.processItem(configMapName.(string))
	if err == nil {
		// No error, reset the ratelimit counters
		c.queue.Forget(configMapName)
	} else if c.queue.NumRequeues(configMapName) < maxRetries {
		log.Errorf("Error processing %s (will retry): %v", configMapName, err)
		c.queue.AddRateLimited(configMapName)
	} else {
		log.Errorf("Error processing %s (giving up): %v", configMapName, err)
		c.queue.Forget(configMapName)
		utilruntime.HandleError(err)
	}

	return true
}

// processItem processese a configmap event.
func (c *Controller) processItem(configMapName string) error {
	obj, exists, err := c.informer.GetIndexer().GetByKey(configMapName)
	if err != nil {
		return fmt.Errorf("error fetching object %s error: %v", configMapName, err)
	}

	// This looks a little racy...
	if exists {
		c.addConfigMap(obj.(*corev1.ConfigMap))
	} else {
		c.deleteConfigMap(configMapName)
	}
	return nil
}

// addConfigMap is called in the event that a configmap was added to the system.
func (c *Controller) addConfigMap(s *corev1.ConfigMap) {
	log.Info("Converting ConfigMap to CRD object.")

	// yaml.YAMLtoJSON does not appear to understand "---".
	yamlStrings := strings.Split(s.Data["config"], "---")

	// Build a set of yaml strings and range over them.
	for _, yamlCRD := range yamlStrings {
		// Convert the yaml that is split into seprate blobs	
		jsonBlob, _ := yaml.YAMLToJSON([]byte(yamlCRD))

		// Decode the json into a Kubernetes CRD object
		crdDeserialized, _, err := deserializer.Decode(jsonBlob, nil, nil)
		if err != nil {
			// Coudln't decode the object, no sense creating the CRD
			fmt.Printf("%v", err)
			continue
		}
		// typecast the object into a CRD
		CRD := crdDeserialized.(*apiextensionsv1beta1.CustomResourceDefinition)

		// Create  the CRD object
		if _, err := c.clientExt.ApiextensionsV1beta1().CustomResourceDefinitions().Create(CRD); err != nil {
			fmt.Printf("%v", err)
                }
	}
}

// deleteConfigMap is called in the event that a configmap was deleted from the system.
// We do nothing with this event.  We could potentially delete CRDs, but that seems dangerous.
func (c *Controller) deleteConfigMap(configMapName string) {
	log.Info("ConfigMap deleted from system")
}
