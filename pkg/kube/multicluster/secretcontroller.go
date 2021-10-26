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

package multicluster

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
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

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube"
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
	monitoring.MustRegister(clustersCount)
}

var (
	timeouts = monitoring.NewSum(
		"remote_cluster_sync_timeouts_total",
		"Number of times remote clusters took too long to sync, causing slow startup that excludes remote clusters.",
	)

	clusterType = monitoring.MustCreateLabel("cluster_type")

	clustersCount = monitoring.NewGauge(
		"istiod_managed_clusters",
		"Number of clusters managed by istiod",
		monitoring.WithLabels(clusterType),
	)

	localClusters  = clustersCount.With(clusterType.Value("local"))
	remoteClusters = clustersCount.With(clusterType.Value("remote"))
)

type ClusterHandler interface {
	AddCluster(cluster *Cluster) error
	UpdateCluster(cluster *Cluster) error
	RemoveCluster(clusterID cluster.ID) error
}

// Controller is the controller implementation for Secret resources
type Controller struct {
	namespace          string
	localClusterID     cluster.ID
	localClusterClient kube.Client
	queue              workqueue.RateLimitingInterface
	informer           cache.SharedIndexInformer

	cs *ClusterStore

	handlers []ClusterHandler

	once              sync.Once
	syncInterval      time.Duration
	initialSync       atomic.Bool
	remoteSyncTimeout atomic.Bool
}

// Cluster defines cluster struct
type Cluster struct {
	ID            cluster.ID
	kubeConfigSha [sha256.Size]byte

	// Client for accessing the cluster.
	Client kube.Client

	stop chan struct{}

	// initialSync is marked when RunAndWait completes
	initialSync *atomic.Bool
	// SyncTimeout is marked after features.RemoteClusterTimeout
	SyncTimeout *atomic.Bool
}

// Stop channel which is closed when the cluster is removed or the Controller that created the client is stopped.
// Client.RunAndWait is called using this channel.
func (r *Cluster) Stop() <-chan struct{} {
	return r.stop
}

func (c *Controller) AddHandler(h ClusterHandler) {
	log.Infof("handling remote clusters in %T", h)
	c.handlers = append(c.handlers, h)
}

// Run starts the cluster's informers and waits for caches to sync. Once caches are synced, we mark the cluster synced.
// This should be called after each of the handlers have registered informers, and should be run in a goroutine.
func (r *Cluster) Run() {
	r.Client.RunAndWait(r.Stop())
	r.initialSync.Store(true)
}

func (r *Cluster) HasSynced() bool {
	return r.initialSync.Load() || r.SyncTimeout.Load()
}

func (r *Cluster) SyncDidTimeout() bool {
	return r.SyncTimeout.Load() && !r.HasSynced()
}

// ClusterStore is a collection of clusters
type ClusterStore struct {
	sync.RWMutex
	// keyed by secret key(ns/name)->clusterID
	remoteClusters map[string]map[cluster.ID]*Cluster
}

// newClustersStore initializes data struct to store clusters information
func newClustersStore() *ClusterStore {
	return &ClusterStore{
		remoteClusters: make(map[string]map[cluster.ID]*Cluster),
	}
}

func (c *ClusterStore) Store(secretKey string, clusterID cluster.ID, value *Cluster) {
	c.Lock()
	defer c.Unlock()
	if _, ok := c.remoteClusters[secretKey]; !ok {
		c.remoteClusters[secretKey] = make(map[cluster.ID]*Cluster)
	}
	c.remoteClusters[secretKey][clusterID] = value
}

func (c *ClusterStore) Get(secretKey string, clusterID cluster.ID) *Cluster {
	c.RLock()
	defer c.RUnlock()
	if _, ok := c.remoteClusters[secretKey]; !ok {
		return nil
	}
	return c.remoteClusters[secretKey][clusterID]
}

func (c *ClusterStore) GetByID(clusterID cluster.ID) *Cluster {
	c.RLock()
	defer c.RUnlock()
	for _, clusters := range c.remoteClusters {
		c, ok := clusters[clusterID]
		if ok {
			return c
		}
	}
	return nil
}

// All returns a copy of the current remote clusters.
func (c *ClusterStore) All() map[string]map[cluster.ID]*Cluster {
	if c == nil {
		return nil
	}
	c.RLock()
	defer c.RUnlock()
	out := make(map[string]map[cluster.ID]*Cluster, len(c.remoteClusters))
	for secret, clusters := range c.remoteClusters {
		out[secret] = make(map[cluster.ID]*Cluster, len(clusters))
		for cid, c := range clusters {
			outCluster := *c
			out[secret][cid] = &outCluster
		}
	}
	return out
}

// GetExistingClustersFor return existing clusters registered for the given secret
func (c *ClusterStore) GetExistingClustersFor(secretKey string) []*Cluster {
	c.RLock()
	defer c.RUnlock()
	out := make([]*Cluster, 0, len(c.remoteClusters[secretKey]))
	for _, cluster := range c.remoteClusters[secretKey] {
		out = append(out, cluster)
	}
	return out
}

func (c *ClusterStore) Len() int {
	c.Lock()
	defer c.Unlock()
	out := 0
	for _, clusterMap := range c.remoteClusters {
		out += len(clusterMap)
	}
	return out
}

// NewController returns a new secret controller
func NewController(kubeclientset kube.Client, namespace string, localClusterID cluster.ID) *Controller {
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

	// init gauges
	localClusters.Record(1.0)
	remoteClusters.Record(0.0)

	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	controller := &Controller{
		namespace:          namespace,
		localClusterID:     localClusterID,
		localClusterClient: kubeclientset,
		cs:                 newClustersStore(),
		informer:           secretsInformer,
		queue:              queue,
		syncInterval:       100 * time.Millisecond,
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
func (c *Controller) Run(stopCh <-chan struct{}) error {
	// run handlers for the local cluster; do not store this *Cluster in the ClusterStore or give it a SyncTimeout
	// this is done outside the goroutine, we should block other Run/startFuncs until this is registered
	localCluster := &Cluster{Client: c.localClusterClient, ID: c.localClusterID, stop: writableStop(stopCh)}
	if err := c.addCallback(localCluster); err != nil {
		return fmt.Errorf("failed initializing local cluster %s: %v", c.localClusterID, err)
	}
	go func() {
		defer utilruntime.HandleCrash()
		defer c.queue.ShutDown()

		t0 := time.Now()
		log.Info("Starting multicluster remote secrets controller")

		go c.informer.Run(stopCh)

		if !kube.WaitForCacheSyncInterval(stopCh, c.syncInterval, c.informer.HasSynced) {
			log.Error("Failed to sync multicluster remote secrets controller cache")
			return
		}
		log.Infof("multicluster remote secrets controller cache synced in %v", time.Since(t0))
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
	}()
	return nil
}

// writableStop converts a read-only chan to a writable one.
// nothing should be written to the output, it just allows passing
// the channel where the types mismatch.
// The returned channel will be closed when the original is written to/closed.
func writableStop(original <-chan struct{}) chan struct{} {
	out := make(chan struct{})
	go func() {
		<-original
		close(out)
	}()
	return out
}

func (c *Controller) close() {
	c.cs.Lock()
	defer c.cs.Unlock()
	for _, clusterMap := range c.cs.remoteClusters {
		for _, cluster := range clusterMap {
			close(cluster.stop)
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
	for _, clusterMap := range c.cs.remoteClusters {
		for _, cluster := range clusterMap {
			if !cluster.HasSynced() {
				log.Debugf("remote cluster %s registered informers have not been synced up yet", cluster.ID)
				return false
			}
		}
	}

	return true
}

func (c *Controller) HasSynced() bool {
	synced := c.hasSynced()
	if synced {
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
	remoteClusters.Record(float64(c.cs.Len()))

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

// BuildClientsFromConfig creates kube.Clients from the provided kubeconfig. This is overridden for testing only
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

func (c *Controller) createRemoteCluster(kubeConfig []byte, clusterID string) (*Cluster, error) {
	clients, err := BuildClientsFromConfig(kubeConfig)
	if err != nil {
		return nil, err
	}
	stop := make(chan struct{})
	return &Cluster{
		ID:     cluster.ID(clusterID),
		Client: clients,
		stop:   stop,
		// for use inside the package, to close on cleanup
		initialSync:   atomic.NewBool(false),
		SyncTimeout:   &c.remoteSyncTimeout,
		kubeConfigSha: sha256.Sum256(kubeConfig),
	}, nil
}

func (c *Controller) addSecret(secretKey string, s *corev1.Secret) {
	// First delete clusters
	existingClusters := c.cs.GetExistingClustersFor(secretKey)
	for _, existingCluster := range existingClusters {
		if _, ok := s.Data[string(existingCluster.ID)]; !ok {
			c.deleteMemberCluster(secretKey, existingCluster.ID)
		}
	}

	for clusterID, kubeConfig := range s.Data {
		action, callback := "Adding", c.addCallback
		if prev := c.cs.Get(secretKey, cluster.ID(clusterID)); prev != nil {
			action, callback = "Updating", c.updateCallback
			// clusterID must be unique even across multiple secrets
			// TODO： warning
			kubeConfigSha := sha256.Sum256(kubeConfig)
			if bytes.Equal(kubeConfigSha[:], prev.kubeConfigSha[:]) {
				log.Infof("skipping update of cluster_id=%v from secret=%v: (kubeconfig are identical)", clusterID, secretKey)
				continue
			}
		}
		if cluster.ID(clusterID) == c.localClusterID {
			log.Infof("ignoring %s cluster %v from secret %v as it would overwrite the local cluster", action, clusterID, secretKey)
			continue
		}
		log.Infof("%s cluster %v from secret %v", action, clusterID, secretKey)

		remoteCluster, err := c.createRemoteCluster(kubeConfig, clusterID)
		if err != nil {
			log.Errorf("%s cluster_id=%v from secret=%v: %v", action, clusterID, secretKey, err)
			continue
		}
		c.cs.Store(secretKey, remoteCluster.ID, remoteCluster)
		if err := callback(remoteCluster); err != nil {
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
	for clusterID, cluster := range c.cs.remoteClusters[secretKey] {
		if clusterID == c.localClusterID {
			log.Infof("ignoring delete cluster %v from secret %v as it would overwrite the local cluster", clusterID, secretKey)
			continue
		}
		log.Infof("Deleting cluster_id=%v configured by secret=%v", clusterID, secretKey)
		err := c.removeCallback(clusterID)
		if err != nil {
			log.Errorf("Error removing cluster_id=%v configured by secret=%v: %v",
				clusterID, secretKey, err)
		}
		close(cluster.stop)
		delete(c.cs.remoteClusters[secretKey], clusterID)
	}
	delete(c.cs.remoteClusters, secretKey)
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
	close(c.cs.remoteClusters[secretKey][clusterID].stop)
	delete(c.cs.remoteClusters[secretKey], clusterID)
}

func (c *Controller) addCallback(cluster *Cluster) error {
	var errs *multierror.Error
	for _, handler := range c.handlers {
		errs = multierror.Append(errs, handler.AddCluster(cluster))
	}
	return errs.ErrorOrNil()
}

func (c *Controller) updateCallback(cluster *Cluster) error {
	var errs *multierror.Error
	for _, handler := range c.handlers {
		errs = multierror.Append(errs, handler.UpdateCluster(cluster))
	}
	return errs.ErrorOrNil()
}

func (c *Controller) removeCallback(key cluster.ID) error {
	var errs *multierror.Error
	for _, handler := range c.handlers {
		errs = multierror.Append(errs, handler.RemoveCluster(key))
	}
	return errs.ErrorOrNil()
}

// ListRemoteClusters provides debug info about connected remote clusters.
func (c *Controller) ListRemoteClusters() []cluster.DebugInfo {
	var out []cluster.DebugInfo
	for secretName, clusters := range c.cs.All() {
		for clusterID, c := range clusters {
			syncStatus := "syncing"
			if c.HasSynced() {
				syncStatus = "synced"
			} else if c.SyncDidTimeout() {
				syncStatus = "timeout"
			}

			out = append(out, cluster.DebugInfo{
				ID:         clusterID,
				SecretName: secretName,
				SyncStatus: syncStatus,
			})
		}
	}
	return out
}

func (c *Controller) GetRemoteKubeClient(clusterID cluster.ID) kubernetes.Interface {
	if remoteCluster := c.cs.GetByID(clusterID); remoteCluster != nil {
		return remoteCluster.Client
	}
	return nil
}
