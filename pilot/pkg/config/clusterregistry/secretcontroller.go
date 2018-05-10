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

package clusterregistry

import (
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
	k8s_cr "k8s.io/cluster-registry/pkg/apis/clusterregistry/v1alpha1"

	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/log"
)

const (
	mcLabel    = "istio/multiCluster"
	maxRetries = 5
)

var (
	serverStartTime time.Time
)

// Controller is the controller implementation for Secret resources
type Controller struct {
	kubeclientset     kubernetes.Interface
	namespace         string
	cs                *ClusterStore
	queue             workqueue.RateLimitingInterface
	informer          cache.SharedIndexInformer
	watchedNamespace  string
	domainSufix       string
	resyncInterval    time.Duration
	serviceController *aggregate.Controller
}

// NewController returns a new secret controller
func NewController(
	kubeclientset kubernetes.Interface,
	namespace string,
	cs *ClusterStore,
	serviceController *aggregate.Controller,
	resyncInterval time.Duration,
	watchedNamespace string,
	domainSufix string) *Controller {

	secretsInformer := cache.NewSharedIndexInformer(&cache.ListWatch{
		ListFunc: func(opts meta_v1.ListOptions) (runtime.Object, error) {
			opts.LabelSelector = mcLabel + "=true"
			return kubeclientset.CoreV1().Secrets(namespace).List(opts)
		},
		WatchFunc: func(opts meta_v1.ListOptions) (watch.Interface, error) {
			opts.LabelSelector = mcLabel + "=true"
			return kubeclientset.CoreV1().Secrets(namespace).Watch(opts)
		},
	},
		&corev1.Secret{},
		0,
		cache.Indexers{})

	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	controller := &Controller{
		kubeclientset:     kubeclientset,
		namespace:         namespace,
		cs:                cs,
		informer:          secretsInformer,
		queue:             queue,
		watchedNamespace:  watchedNamespace,
		domainSufix:       domainSufix,
		resyncInterval:    resyncInterval,
		serviceController: serviceController,
	}

	log.Info("Setting up event handlers")
	secretsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
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

// Run starts the controller until it receves a message over stopCh
func (c *Controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	log.Info("Starting Secrets controller")
	serverStartTime = time.Now().Local()
	go c.informer.Run(stopCh)

	// Wait for the caches to be synced before starting workers
	log.Info("Waiting for informer caches to sync")
	if !cache.WaitForCacheSync(stopCh, c.informer.HasSynced) {
		utilruntime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}

	wait.Until(c.runWorker, 5*time.Second, stopCh)
}

// StartSecretController start k8s controller which will be watching Secret object
// in a specified namesapce
func StartSecretController(k8s kubernetes.Interface,
	cs *ClusterStore,
	serviceController *aggregate.Controller,
	namespace string,
	resyncInterval time.Duration,
	watchedNamespace,
	domainSufix string) error {
	stopCh := make(chan struct{})
	controller := NewController(k8s, namespace, cs, serviceController, resyncInterval, watchedNamespace, domainSufix)

	go controller.Run(stopCh)

	return nil
}

func (c *Controller) runWorker() {
	for c.processNextItem() {
		// continue looping
	}
}

func (c *Controller) processNextItem() bool {
	key, quit := c.queue.Get()

	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.processItem(key.(string))
	if err == nil {
		// No error, reset the ratelimit counters
		c.queue.Forget(key)
	} else if c.queue.NumRequeues(key) < maxRetries {
		log.Errorf("Error processing %s (will retry): %v", key, err)
		c.queue.AddRateLimited(key)
	} else {
		log.Errorf("Error processing %s (giving up): %v", key, err)
		c.queue.Forget(key)
		utilruntime.HandleError(err)
	}

	return true
}

func (c *Controller) processItem(key string) error {
	obj, exists, err := c.informer.GetIndexer().GetByKey(key)
	if err != nil {
		return fmt.Errorf("error fetching object with key %s from store: %v", key, err)
	}

	if !exists {
		c.secretDelete(key)
		return nil
	}
	c.secretAdd(obj)

	return nil
}

func addMemberCluster(s *corev1.Secret, c *Controller) {
	c.cs.storeLock.Lock()
	defer c.cs.storeLock.Unlock()
	// Check if there is already a cluster member with the specified
	if _, ok := c.cs.clientConfigs[s.ObjectMeta.Name]; !ok {
		log.Infof("Adding new cluster member: %s", s.ObjectMeta.Name)
		clientConfig, err := clientcmd.Load(s.Data[s.ObjectMeta.Name])
		if err != nil {
			log.Errorf("failed to load client config from secret %s in namespace %s with error: %v",
				s.ObjectMeta.Name, s.ObjectMeta.Namespace, err)
		}
		cluster := k8s_cr.Cluster{
			TypeMeta: meta_v1.TypeMeta{
				Kind:       "Cluster",
				APIVersion: "clusterregistry.k8s.io/v1alpha1",
			},
			ObjectMeta: meta_v1.ObjectMeta{
				Name:      s.ObjectMeta.Name,
				Namespace: s.ObjectMeta.Namespace,
			},
		}
		c.cs.clientConfigs[s.ObjectMeta.Name] = *clientConfig
		c.cs.clusters = append(c.cs.clusters, &cluster)
		_, client, _ := kube.CreateInterfaceFromClusterConfig(clientConfig)
		kubectl := kube.NewController(client, kube.ControllerOptions{
			WatchedNamespace: c.watchedNamespace,
			ResyncPeriod:     c.resyncInterval,
			DomainSuffix:     c.domainSufix,
		})

		c.serviceController.AddRegistry(
			aggregate.Registry{
				Name:             serviceregistry.KubernetesRegistry,
				ClusterID:        GetClusterID(&cluster),
				ServiceDiscovery: kubectl,
				ServiceAccounts:  kubectl,
				Controller:       kubectl,
			})
	}
	// TODO Add exporting a number of cluster to Prometheus
	// for now for debbuging purposes, print it to the log.
	log.Infof("Number of clusters in the cluster store: %d", len(c.cs.clientConfigs))
}

func deleteMemberCluster(s string, cs *ClusterStore) {
	cs.storeLock.Lock()
	defer cs.storeLock.Unlock()
	// Check if there is a cluster member with the specified name
	if _, ok := cs.clientConfigs[s]; ok {
		log.Infof("Deleting cluster member: %s", s)
		delete(cs.clientConfigs, s)
		for i, c := range cs.clusters {
			if c.ObjectMeta.Name == s {
				cs.clusters = append(cs.clusters[:i], cs.clusters[i+1:]...)
				break
			}
		}
	}
	log.Infof("Number of clusters in the cluster store: %d", len(cs.clientConfigs))
}

func (c *Controller) secretAdd(obj interface{}) {
	s := obj.(*corev1.Secret)
	addMemberCluster(s, c)
}

func (c *Controller) secretDelete(key string) {
	s := key
	if strings.Contains(key, "/") {
		s = strings.Split(key, "/")[1]
	}
	deleteMemberCluster(s, c.cs)
}
