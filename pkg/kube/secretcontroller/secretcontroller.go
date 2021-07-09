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

package secretcontroller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"sync"
	"time"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/secretcontroller/remotecluster"
	"istio.io/pkg/log"
	"istio.io/pkg/monitoring"
)

const (
	initialSyncSignal       = "INIT"
	MultiClusterSecretLabel = "istio/multiCluster"
	maxRetries              = 5
)

func init() {
	monitoring.MustRegister(timeouts)
}

var timeouts = monitoring.NewSum(
	"remote_cluster_sync_timeouts_total",
	"Number of times remote clusters took too long to sync, causing slow startup that excludes remote clusters.",
)

// newClientCallback prototype for the add secret callback function.
type newClientCallback func(clusterID cluster.ID, cluster *remotecluster.Cluster) error

// removeClientCallback prototype for the remove secret callback function.
type removeClientCallback func(clusterID cluster.ID) error

// Controller is the controller implementation for Secret resources
type Controller struct {
	namespace string
	queue     workqueue.RateLimitingInterface
	informer  cache.SharedIndexInformer

	cs *remotecluster.Store

	addCallback    newClientCallback
	updateCallback newClientCallback
	removeCallback removeClientCallback

	once              sync.Once
	syncInterval      time.Duration
	initialSync       atomic.Bool
	remoteSyncTimeout atomic.Bool
}

// NewController returns a new secret controller
func NewController(
	kubeclientset kubernetes.Interface,
	namespace string,
	addCallback newClientCallback,
	updateCallback newClientCallback,
	removeCallback removeClientCallback) *Controller {
	secretsInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
				opts.LabelSelector = MultiClusterSecretLabel + "=true"
				return kubeclientset.CoreV1().Secrets(namespace).List(context.TODO(), opts)
			},
			WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
				opts.LabelSelector = MultiClusterSecretLabel + "=true"
				return kubeclientset.CoreV1().Secrets(namespace).Watch(context.TODO(), opts)
			},
		},
		&corev1.Secret{}, 0, cache.Indexers{},
	)

	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	controller := &Controller{
		namespace:      namespace,
		cs:             remotecluster.NewStore(),
		informer:       secretsInformer,
		queue:          queue,
		addCallback:    addCallback,
		updateCallback: updateCallback,
		removeCallback: removeCallback,
	}

	secretsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				log.Infof("Processing add: %s", key)
				queue.Add(key)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if oldObj == newObj || reflect.DeepEqual(oldObj, newObj) {
				return
			}

			key, err := cache.MetaNamespaceKeyFunc(newObj)
			if err == nil {
				log.Infof("Processing update: %s", key)
				queue.Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				log.Infof("Processing delete: %s", key)
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

	t0 := time.Now()
	log.Info("Starting Secrets controller")

	go c.informer.Run(stopCh)

	if !kube.WaitForCacheSyncInterval(stopCh, c.syncInterval, c.informer.HasSynced) {
		log.Error("Failed to sync secret controller cache")
		return
	}
	log.Infof("Secret controller cache synced in %v", time.Since(t0))
	// all secret events before this signal must be processed before we're marked "ready"
	c.queue.Add(initialSyncSignal)
	if features.RemoteClusterTimeout != 0 {
		time.AfterFunc(features.RemoteClusterTimeout, func() {
			c.remoteSyncTimeout.Store(true)
		})
	}
	go wait.Until(c.runWorker, 5*time.Second, stopCh)
	<-stopCh
	c.close()
}

func (c *Controller) close() {
	c.cs.Lock()
	defer c.cs.Unlock()
	for _, clusterMap := range c.cs.RemoteClusters {
		for _, cluster := range clusterMap {
			close(cluster.Stop)
		}
	}
}

func (c *Controller) hasSynced() bool {
	if !c.initialSync.Load() {
		log.Debug("secret controller did not syncup secrets presented at startup")
		// we haven't finished processing the secrets that were present at startup
		return false
	}
	c.cs.RLock()
	defer c.cs.RUnlock()
	for _, clusterMap := range c.cs.RemoteClusters {
		for _, cluster := range clusterMap {
			if !cluster.HasSynced() {
				log.Debugf("remote cluster %s registered informers have not been synced up yet", cluster.ClusterID)
				return false
			}
		}
	}

	return true
}

func (c *Controller) HasSynced() bool {
	synced := c.hasSynced()
	if synced {
		log.Info("all remote clusters have been synced")
		return true
	}
	if c.remoteSyncTimeout.Load() {
		c.once.Do(func() {
			log.Errorf("remote clusters failed to sync after %v", features.RemoteClusterTimeout)
			timeouts.Increment()
		})
		return true
	}

	return synced
}

func (c *Controller) Store() *remotecluster.Store {
	return c.cs
}

// StartSecretController creates the secret controller.
func StartSecretController(
	kubeclientset kubernetes.Interface,
	addCallback newClientCallback, updateCallback newClientCallback,
	removeCallback removeClientCallback,
	namespace string,
	syncInterval time.Duration,
	stop <-chan struct{},
) *Controller {
	controller := NewController(kubeclientset, namespace, addCallback, updateCallback, removeCallback)
	controller.syncInterval = syncInterval

	go controller.Run(stop)

	return controller
}

func (c *Controller) runWorker() {
	for c.processNextItem() {
	}
}

func (c *Controller) processNextItem() bool {
	key, quit := c.queue.Get()
	if quit {
		log.Info("secret controller queue is shutting down, so returning")
		return false
	}
	log.Infof("secret controller got event from queue for secret %s", key)
	defer c.queue.Done(key)

	err := c.processItem(key.(string))
	if err == nil {
		log.Debugf("secret controller finished processing secret %s", key)
		// No error, reset the ratelimit counters
		c.queue.Forget(key)
	} else if c.queue.NumRequeues(key) < maxRetries {
		log.Errorf("Error processing %s (will retry): %v", key, err)
		c.queue.AddRateLimited(key)
	} else {
		log.Errorf("Error processing %s (giving up): %v", key, err)
		c.queue.Forget(key)
	}

	return true
}

func (c *Controller) processItem(key string) error {
	if key == initialSyncSignal {
		log.Info("secret controller initial sync done")
		c.initialSync.Store(true)
		return nil
	}
	log.Infof("processing secret event for secret %s", key)
	obj, exists, err := c.informer.GetIndexer().GetByKey(key)
	if err != nil {
		return fmt.Errorf("error fetching object %s error: %v", key, err)
	}
	if exists {
		log.Debugf("secret %s exists in informer cache, processing it", key)
		c.addSecret(key, obj.(*corev1.Secret))
	} else {
		log.Debugf("secret %s does not exist in informer cache, deleting it", key)
		c.deleteSecret(key)
	}

	return nil
}

func (c *Controller) addSecret(secretKey string, s *corev1.Secret) {
	// First delete clusters
	existingClusters := c.cs.GetExistingClustersFor(secretKey)
	for _, existingCluster := range existingClusters {
		if _, ok := s.Data[existingCluster.ClusterID]; !ok {
			c.deleteMemberCluster(secretKey, cluster.ID(existingCluster.ClusterID))
		}
	}

	for clusterID, kubeConfig := range s.Data {
		action, callback := "Adding", c.addCallback
		if prev := c.cs.Get(secretKey, cluster.ID(clusterID)); prev != nil {
			action, callback = "Updating", c.updateCallback
			// clusterID must be unique even across multiple secrets
			// TODO： warning
			kubeConfigSha := sha256.Sum256(kubeConfig)
			if bytes.Equal(kubeConfigSha[:], prev.KubeConfigSha[:]) {
				log.Infof("skipping update of cluster_id=%v from secret=%v: (kubeconfig are identical)", clusterID, secretKey)
				continue
			}
		}
		log.Infof("%s cluster %v from secret %v", action, clusterID, secretKey)

		remoteCluster, err := remotecluster.New(kubeConfig, clusterID, &c.remoteSyncTimeout)
		if err != nil {
			log.Errorf("%s cluster_id=%v from secret=%v: %v", action, clusterID, secretKey, err)
			continue
		}
		c.cs.Insert(secretKey, cluster.ID(clusterID), remoteCluster)
		if err := callback(cluster.ID(clusterID), remoteCluster); err != nil {
			log.Errorf("%s cluster_id from secret=%v: %s %v", action, clusterID, secretKey, err)
			continue
		}
		log.Infof("finished callback for %s and starting to sync", clusterID)
		go remoteCluster.Run()
	}

	log.Infof("Number of remote clusters: %d", c.cs.Len())
}

func (c *Controller) deleteSecret(secretKey string) {
	c.cs.Lock()
	defer func() {
		c.cs.Unlock()
		log.Infof("Number of remote clusters: %d", c.cs.Len())
	}()
	for clusterID, cluster := range c.cs.RemoteClusters[secretKey] {
		log.Infof("Deleting cluster_id=%v configured by secret=%v", clusterID, secretKey)
		err := c.removeCallback(clusterID)
		if err != nil {
			log.Errorf("Error removing cluster_id=%v configured by secret=%v: %v",
				clusterID, secretKey, err)
		}
		close(cluster.Stop)
		delete(c.cs.RemoteClusters, secretKey)
	}
}

func (c *Controller) deleteMemberCluster(secretKey string, clusterID cluster.ID) {
	c.cs.Lock()
	defer func() {
		c.cs.Unlock()
		log.Infof("Number of remote clusters: %d", c.cs.Len())
	}()
	log.Infof("Deleting cluster_id=%v configured by secret=%v", clusterID, secretKey)
	err := c.removeCallback(clusterID)
	if err != nil {
		log.Errorf("Error removing cluster_id=%v configured by secret=%v: %v",
			clusterID, secretKey, err)
	}
	close(c.cs.RemoteClusters[secretKey][clusterID].Stop)
	delete(c.cs.RemoteClusters[secretKey], clusterID)
}
