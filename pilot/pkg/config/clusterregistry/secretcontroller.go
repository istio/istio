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

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"

	"istio.io/istio/pilot/pkg/model"
	envoy "istio.io/istio/pilot/pkg/proxy/envoy/v2"
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
	clusterCreationCounts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "k8s_cluster_created_count",
		Help: "The number of kubernetes clusters added due to secret creation.",
	}, []string{})

	clusterDeletionCounts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "k8s_cluster_deleted_count",
		Help: "The number of kubernetes clusters deleted due to secret deletion.",
	}, []string{})
)

func init() {
	prometheus.MustRegister(clusterCreationCounts)
	prometheus.MustRegister(clusterDeletionCounts)
}

// Controller is the controller implementation for Secret resources
type Controller struct {
	kubeclientset     kubernetes.Interface
	cs                *ClusterStore
	queue             workqueue.RateLimitingInterface
	informer          cache.SharedIndexInformer
	watchedNamespace  string
	domainSufix       string
	resyncInterval    time.Duration
	serviceController *aggregate.Controller
	discoveryServer   *envoy.DiscoveryServer
}

// NewController returns a new secret controller
func NewController(
	kubeclientset kubernetes.Interface,
	namespace string,
	cs *ClusterStore,
	serviceController *aggregate.Controller,
	discoveryServer *envoy.DiscoveryServer,
	resyncInterval time.Duration,
	watchedNamespace string,
	domainSufix string) *Controller {

	secretsInformer := v1.NewFilteredSecretInformer(kubeclientset, namespace, 0, cache.Indexers{}, func(opts *meta_v1.ListOptions) {
		opts.LabelSelector = mcLabel + "=true"
	})

	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	controller := &Controller{
		kubeclientset:     kubeclientset,
		cs:                cs,
		informer:          secretsInformer,
		queue:             queue,
		watchedNamespace:  watchedNamespace,
		domainSufix:       domainSufix,
		resyncInterval:    resyncInterval,
		serviceController: serviceController,
		discoveryServer:   discoveryServer,
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

// Run starts the controller until it receives a message over stopCh
func (c *Controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	log.Info("Starting Secrets controller")
	go c.informer.Run(stopCh)

	// Wait for the caches to be synced before starting workers
	log.Info("Waiting for informer caches to sync")
	if !cache.WaitForCacheSync(stopCh, c.informer.HasSynced) {
		utilruntime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}

	wait.Until(c.runWorker, 5*time.Second, stopCh)
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
			client, _ := kube.CreateInterfaceFromClusterConfig(clientConfig)
			kubeController := kube.NewController(client, kube.ControllerOptions{
				WatchedNamespace: c.watchedNamespace,
				ResyncPeriod:     c.resyncInterval,
				DomainSuffix:     c.domainSufix,
			})
			stopCh := make(chan struct{})
			c.cs.rc[clusterID] = &RemoteCluster{
				FromSecret:     secretName,
				Controller:     kubeController,
				ControlChannel: stopCh,
			}
			c.serviceController.AddRegistry(
				aggregate.Registry{
					Name:             serviceregistry.KubernetesRegistry,
					ClusterID:        clusterID,
					ServiceDiscovery: kubeController,
					ServiceAccounts:  kubeController,
					Controller:       kubeController,
				})

			kubeController.AppendServiceHandler(func(*model.Service, model.Event) { c.discoveryServer.ClearCacheFunc()() })
			kubeController.AppendInstanceHandler(func(*model.ServiceInstance, model.Event) { c.discoveryServer.ClearCacheFunc()() })
			go kubeController.Run(stopCh)
			clusterCreationCounts.With(prometheus.Labels{}).Inc()
		} else {
			log.Infof("Cluster %s in the secret %s in namespace %s already exists",
				clusterID, secretName, s.ObjectMeta.Namespace)
		}
	}

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
			// Deleting remote cluster entry from clusters store
			delete(c.cs.rc, clusterID)
			clusterDeletionCounts.With(prometheus.Labels{}).Inc()
		}
	}
	log.Infof("Number of clusters in the cluster store: %d", len(c.cs.rc))
}
