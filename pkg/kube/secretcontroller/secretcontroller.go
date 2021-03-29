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
	"errors"
	"fmt"
	"reflect"
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
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"

	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

const (
	initialSyncSignal       = "INIT"
	MultiClusterSecretLabel = "istio/multiCluster"
	maxRetries              = 5
)

// MulticlusterHandler handles new clusters being added/removed from the control plane.
type MulticlusterHandler interface {
	// OnNewCluster prototype for the add secret callback function.
	OnNewCluster(clients kube.Client, stop <-chan struct{}, dataKey string) error

	// OnClusterUpdated prototype for the update secret callback function.
	OnClusterUpdated(clients kube.Client, stop <-chan struct{}, dataKey string) error

	// OnClusterRemoved prototype for the remove secret callback function.
	OnClusterRemoved(dataKey string) error
}

// Controller handles Secret resources that provide access to other cluster's API servers.
// MulticlusterControllers can be added to react to clusters being added/updated/removed.
type Controller struct {
	kubeclientset kubernetes.Interface
	namespace     string
	cs            *clusterStore
	queue         workqueue.RateLimitingInterface
	informer      cache.SharedIndexInformer
	controllers   []MulticlusterHandler

	syncInterval time.Duration

	initialSync atomic.Bool
}

// remoteCluster defines cluster struct
type remoteCluster struct {
	secretName    string
	clients       kube.Client
	kubeConfigSha [sha256.Size]byte
	stop          chan struct{}
}

// clusterStore is a collection of clusters
type clusterStore struct {
	remoteClusters map[string]*remoteCluster
}

// newClustersStore initializes data struct to store clusters information
func newClustersStore() *clusterStore {
	remoteClusters := make(map[string]*remoteCluster)
	return &clusterStore{
		remoteClusters: remoteClusters,
	}
}

// NewController returns a new secret controller
func NewController(kubeclientset kubernetes.Interface, namespace string, syncInterval time.Duration) *Controller {
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
		kubeclientset: kubeclientset,
		namespace:     namespace,
		cs:            newClustersStore(),
		informer:      secretsInformer,
		queue:         queue,
		syncInterval:  syncInterval,
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

func (c *Controller) AddHandler(mc MulticlusterHandler) {
	c.controllers = append(c.controllers, mc)
}

// Run starts the controller until it receives a message over stopCh
func (c *Controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	log.Info("Starting Secrets controller")

	go c.informer.Run(stopCh)

	// Wait for the caches to be synced before starting workers
	if !kube.WaitForCacheSyncInterval(stopCh, c.syncInterval, c.informer.HasSynced) {
		return
	}
	// all secret events before this signal must be processed before we're marked "ready"
	c.queue.Add(initialSyncSignal)

	go wait.Until(c.runWorker, 5*time.Second, stopCh)
	<-stopCh
	c.close()
}

func (c *Controller) close() {
	for _, cluster := range c.cs.remoteClusters {
		close(cluster.stop)
	}
}

func (c *Controller) HasSynced() bool {
	return c.initialSync.Load()
}

func (c *Controller) runWorker() {
	for c.processNextItem() {
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
	if secretName == initialSyncSignal {
		c.initialSync.Store(true)
		return nil
	}

	obj, exists, err := c.informer.GetIndexer().GetByKey(secretName)
	if err != nil {
		return fmt.Errorf("error fetching object %s error: %v", secretName, err)
	}

	if exists {
		c.addMemberCluster(secretName, obj.(*corev1.Secret))
	} else {
		c.deleteMemberCluster(secretName)
	}

	return nil
}

// BuildClientsFromConfig creates kube.Clients from the provided kubeconfig. This is overiden for testing only
var BuildClientsFromConfig = func(kubeConfig []byte) (kube.Client, error) {
	if len(kubeConfig) == 0 {
		return nil, errors.New("kubeconfig is empty")
	}

	rawConfig, err := clientcmd.Load(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("kubeconfig cannot be loaded: %v", err)
	}

	if err := clientcmd.Validate(*rawConfig); err != nil {
		return nil, fmt.Errorf("kubeconfig is not valid: %v", err)
	}

	clientConfig := clientcmd.NewDefaultClientConfig(*rawConfig, &clientcmd.ConfigOverrides{})

	clients, err := kube.NewClient(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kube clients: %v", err)
	}
	return clients, nil
}

func createRemoteCluster(kubeConfig []byte, secretName string) (*remoteCluster, error) {
	clients, err := BuildClientsFromConfig(kubeConfig)
	if err != nil {
		return nil, err
	}
	return &remoteCluster{
		secretName:    secretName,
		clients:       clients,
		stop:          make(chan struct{}),
		kubeConfigSha: sha256.Sum256(kubeConfig),
	}, nil
}

func (c *Controller) addMemberCluster(secretName string, s *corev1.Secret) {
	for clusterID, kubeConfig := range s.Data {
		// clusterID must be unique even across multiple secrets
		var (
			remoteCluster *remoteCluster
			err           error
		)

		if prev, ok := c.cs.remoteClusters[clusterID]; !ok {
			log.Infof("Adding cluster_id=%v from secret=%v", clusterID, secretName)

			remoteCluster, err = createRemoteCluster(kubeConfig, secretName)
			if err != nil {
				log.Errorf("Failed to add remote cluster from secret=%v for cluster_id=%v: %v",
					secretName, clusterID, err)
				continue
			}

			c.cs.remoteClusters[clusterID] = remoteCluster
			for _, handler := range c.controllers {
				if err := handler.OnNewCluster(remoteCluster.clients, remoteCluster.stop, clusterID); err != nil {
					log.Errorf("Error creating cluster_id=%s from secret %v: %v",
						clusterID, secretName, err)
				}
			}
		} else {
			if prev.secretName != secretName {
				log.Errorf("ClusterID reused in two different secrets: %v and %v. ClusterID "+
					"must be unique across all secrets", prev.secretName, secretName)
				continue
			}

			kubeConfigSha := sha256.Sum256(kubeConfig)
			if bytes.Equal(kubeConfigSha[:], prev.kubeConfigSha[:]) {
				log.Infof("Updating cluster_id=%v from secret=%v: (kubeconfig are identical)", clusterID, secretName)
			} else {
				log.Infof("Updating cluster %v from secret %v", clusterID, secretName)

				remoteCluster, err = createRemoteCluster(kubeConfig, secretName)
				if err != nil {
					log.Errorf("Error updating cluster_id=%v from secret=%v: %v",
						clusterID, secretName, err)
					continue
				}
				c.cs.remoteClusters[clusterID] = remoteCluster
				for _, handler := range c.controllers {
					if err := handler.OnClusterUpdated(remoteCluster.clients, remoteCluster.stop, clusterID); err != nil {
						log.Errorf("Error updating cluster_id from secret=%v: %s %v",
							clusterID, secretName, err)
					}
				}
			}
		}
		if remoteCluster != nil {
			go remoteCluster.clients.RunAndWait(remoteCluster.stop)
		}
	}

	log.Infof("Number of remote clusters: %d", len(c.cs.remoteClusters))
}

func (c *Controller) deleteMemberCluster(secretName string) {
	for clusterID, cluster := range c.cs.remoteClusters {
		if cluster.secretName == secretName {
			log.Infof("Deleting cluster_id=%v configured by secret=%v", clusterID, secretName)
			for _, handler := range c.controllers {
				if err := handler.OnClusterRemoved(clusterID); err != nil {
					log.Errorf("Error removing cluster_id=%v configured by secret=%v: %s %v",
						clusterID, secretName, err)
				}
			}
			close(cluster.stop)
			delete(c.cs.remoteClusters, clusterID)
		}
	}
	log.Infof("Number of remote clusters: %d", len(c.cs.remoteClusters))
}
