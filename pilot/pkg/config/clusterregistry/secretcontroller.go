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

	"istio.io/istio/pilot/pkg/model"
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
	configUpdater     model.ConfigUpdater
	edsUpdater        model.EDSUpdater
}

// NewController returns a new secret controller
func NewController(
	kubeclientset kubernetes.Interface,
	namespace string,
	cs *ClusterStore,
	serviceController *aggregate.Controller,
	discoveryServer model.ConfigUpdater,
	edsUpdater model.EDSUpdater,
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
		configUpdater:     discoveryServer,
		edsUpdater:        edsUpdater,
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
	configUpdater model.ConfigUpdater,
	edsUpdater model.EDSUpdater,
	namespace string,
	resyncInterval time.Duration,
	watchedNamespace,
	domainSufix string) error {
	stopCh := make(chan struct{})
	controller := NewController(k8s, namespace, cs, serviceController, configUpdater, edsUpdater, resyncInterval, watchedNamespace, domainSufix)

	go controller.Run(stopCh)

	return nil
}

func (c *Controller) runWorker() {
	for c.processNextItem() {
		// continue looping
	}
}

func (c *Controller) processNextItem() bool {
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

func (c *Controller) processItem(secretName string) error {
	obj, exists, err := c.informer.GetIndexer().GetByKey(secretName)
	if err != nil {
		return fmt.Errorf("error fetching object %s from store: %v", secretName, err)
	}

	if !exists {
		c.deleteMemberCluster(secretName)
		return nil
	}
	c.addMemberCluster(secretName, obj.(*corev1.Secret))

	return nil
}

func (c *Controller) addMemberCluster(secretName string, s *corev1.Secret) {
	c.cs.storeLock.Lock()
	defer c.cs.storeLock.Unlock()
	// Check if there is already a cluster member with the specified
	for clusterID, kubeConfig := range s.Data {
		if _, ok := c.cs.rc[clusterID]; !ok {
			// Disregard it if it's empty
			if len(kubeConfig) == 0 {
				log.Infof("Data '%s' in the secret %s in namespace %s is empty, and disregarded ",
					clusterID, secretName, s.ObjectMeta.Namespace)
				continue
			}
			clientConfig, err := clientcmd.Load(kubeConfig)
			if err != nil {
				log.Infof("Data '%s' in the secret %s in namespace %s is not a kubeconfig: %v",
					clusterID, secretName, s.ObjectMeta.Namespace, err)
				continue
			}
			log.Infof("Adding new cluster member: %s", clusterID)
			c.cs.rc[clusterID] = &RemoteCluster{}
			c.cs.rc[clusterID].Client = clientConfig
			c.cs.rc[clusterID].FromSecret = secretName
			client, _ := kube.CreateInterfaceFromClusterConfig(clientConfig)
			kubectl := kube.NewController(client, kube.ControllerOptions{
				WatchedNamespace: c.watchedNamespace,
				ResyncPeriod:     c.resyncInterval,
				DomainSuffix:     c.domainSufix,
				ClusterID:        clusterID,
				EDSUpdater:       c.edsUpdater,
			})
			c.cs.rc[clusterID].Controller = kubectl
			c.serviceController.AddRegistry(
				aggregate.Registry{
					Name:             serviceregistry.KubernetesRegistry,
					ClusterID:        clusterID,
					ServiceDiscovery: kubectl,
					ServiceAccounts:  kubectl,
					Controller:       kubectl,
				})
			stopCh := make(chan struct{})
			c.cs.rc[clusterID].ControlChannel = stopCh
			_ = kubectl.AppendServiceHandler(func(*model.Service, model.Event) { c.configUpdater.ConfigUpdate(true) })
			_ = kubectl.AppendInstanceHandler(func(*model.ServiceInstance, model.Event) { c.configUpdater.ConfigUpdate(true) })
			go kubectl.Run(stopCh)
		} else {
			log.Infof("Cluster %s in the secret %s in namespace %s already exists",
				clusterID, secretName, s.ObjectMeta.Namespace)
		}
	}
	// TODO Add exporting a number of cluster to Prometheus
	// for now for debbuging purposes, print it to the log.
	log.Infof("Number of clusters in the cluster store: %d", len(c.cs.rc))
}

func (c *Controller) deleteMemberCluster(secretName string) {
	c.cs.storeLock.Lock()
	defer c.cs.storeLock.Unlock()
	// Check if there is a cluster member with the specified name
	for clusterID, cluster := range c.cs.rc {
		if cluster.FromSecret == secretName {
			log.Infof("Deleting cluster member: %s", clusterID)
			// Deleting Service registry associated with controller
			c.serviceController.DeleteRegistry(clusterID)
			// Stop controller
			close(c.cs.rc[clusterID].ControlChannel)
			<-c.cs.rc[clusterID].ControlChannel
			// Deleting remote cluster entry from clusters store
			delete(c.cs.rc, clusterID)
		}
	}
	log.Infof("Number of clusters in the cluster store: %d", len(c.cs.rc))
}
