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

package controller

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

const (
	initialSyncSignal = "INIT"
	maxRetries        = 5
)

// RemoteKubeConfigController is the controller implementation for `istio-kubeconfig` secret
type RemoteKubeConfigController struct {
	kubeclientset kubernetes.Interface
	queue         workqueue.RateLimitingInterface
	informer      cache.SharedIndexInformer
	mu            sync.RWMutex
	initialSync   bool
}

// NewRemoteKubeConfigController returns a pointer to a newly constructed RemoteKubeConfigController instance.
func NewRemoteKubeConfigController(kubeClient kubernetes.Interface, ns, name string) *RemoteKubeConfigController {
	secretsInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
				opts.FieldSelector = "metadata.name=" + name
				return kubeClient.CoreV1().Secrets(ns).List(context.TODO(), opts)
			},
			WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
				opts.FieldSelector = "metadata.name=" + name
				return kubeClient.CoreV1().Secrets(ns).Watch(context.TODO(), opts)
			},
		},
		&corev1.Secret{}, 0, cache.Indexers{},
	)
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	controller := &RemoteKubeConfigController{
		kubeclientset: kubeClient,
		informer:      secretsInformer,
		queue:         queue,
	}
	log.Info("Setting up event handlers")
	secretsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			log.Infof("secret % add: %s", key)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if oldObj == newObj || reflect.DeepEqual(oldObj, newObj) {
				return
			}

			key, err := cache.MetaNamespaceKeyFunc(newObj)
			log.Infof("Processing update: %s", key)
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

// Run starts the controller until it receives a message over stopCh
func (c *RemoteKubeConfigController) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	log.Info("Starting Secrets controller for istio-kubeconfig secret")

	go c.informer.Run(stopCh)

	// Wait for the caches to be synced before starting workers
	log.Info("Waiting for informer caches to sync")
	if !cache.WaitForCacheSync(stopCh, c.informer.HasSynced) {
		utilruntime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}
	// all secret events before this signal must be processed before we're marked "ready"
	c.queue.Add(initialSyncSignal)
	wait.Until(c.runWorker, 5*time.Second, stopCh)
}

func (c *RemoteKubeConfigController) HasSynced() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.initialSync
}

// StartSecretController creates and starts the secret controller.
func StartSecretController(k8s kubernetes.Interface, namespace, name string, stopCh <-chan struct{}) {
	controller := NewRemoteKubeConfigController(k8s, namespace, name)
	go controller.Run(stopCh)
}

func (c *RemoteKubeConfigController) runWorker() {
	for c.processNextItem() {
	}
}

func (c *RemoteKubeConfigController) processNextItem() bool {
	secretName, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(secretName)

	err := c.processItem(secretName.(string))
	if err == nil {
		// No error, reset the ratelimit counters
		c.queue.Forget(secretName)
	} else if c.queue.NumRequeues(secretName) < maxRetries {
		log.Errorf("Error processing %s (will retry): %v", secretName, err)
		c.queue.AddRateLimited(secretName)
	} else {
		log.Errorf("Error processing %s (giving up): %v", secretName, err)
		c.queue.Forget(secretName)
		utilruntime.HandleError(err)
	}

	return true
}

func (c *RemoteKubeConfigController) processItem(secretName string) error {
	if secretName == initialSyncSignal {
		c.mu.Lock()
		c.initialSync = true
		c.mu.Unlock()
		return nil
	}

	obj, exists, err := c.informer.GetIndexer().GetByKey(secretName)
	if err != nil {
		return fmt.Errorf("error fetching object %s error: %v", secretName, err)
	}
	if exists {
		log.Infof("remote secret: %v updated", secretName)
		sec := obj.(*corev1.Secret)
		//update TokenFile - this will make our client automatically get updated to use the latest token
		_, err := kube.UpdateTokenFile(sec)
		if err != nil {
			return err
		}
	} else {
		log.Warnf("the required secret %v has been deleted!", secretName)
	}
	return nil
}
