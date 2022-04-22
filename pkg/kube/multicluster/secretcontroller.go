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
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/util/sets"
	"istio.io/pkg/log"
	"istio.io/pkg/monitoring"
)

const (
	MultiClusterSecretLabel = "istio/multiCluster"
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
	ClusterAdded(cluster *Cluster, stop <-chan struct{}) error
	ClusterUpdated(cluster *Cluster, stop <-chan struct{}) error
	ClusterDeleted(clusterID cluster.ID) error
}

// Controller is the controller implementation for Secret resources
type Controller struct {
	namespace          string
	localClusterID     cluster.ID
	localClusterClient kube.Client
	queue              controllers.Queue
	informer           cache.SharedIndexInformer

	cs *ClusterStore

	handlers []ClusterHandler

	once              sync.Once
	remoteSyncTimeout atomic.Bool
}

// Cluster defines cluster struct
type Cluster struct {
	// ID of the cluster.
	ID cluster.ID
	// SyncTimeout is marked after features.RemoteClusterTimeout.
	SyncTimeout *atomic.Bool
	// Client for accessing the cluster.
	Client kube.Client

	kubeConfigSha [sha256.Size]byte

	stop chan struct{}
	// initialSync is marked when RunAndWait completes
	initialSync *atomic.Bool
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
	clusters       sets.Set
}

// newClustersStore initializes data struct to store clusters information
func newClustersStore() *ClusterStore {
	return &ClusterStore{
		remoteClusters: make(map[string]map[cluster.ID]*Cluster),
		clusters:       sets.New(),
	}
}

func (c *ClusterStore) Store(secretKey string, clusterID cluster.ID, value *Cluster) {
	c.Lock()
	defer c.Unlock()
	if _, ok := c.remoteClusters[secretKey]; !ok {
		c.remoteClusters[secretKey] = make(map[cluster.ID]*Cluster)
	}
	c.remoteClusters[secretKey][clusterID] = value
	c.clusters.Insert(string(clusterID))
}

func (c *ClusterStore) Delete(secretKey string, clusterID cluster.ID) {
	c.Lock()
	defer c.Unlock()
	delete(c.remoteClusters[secretKey], clusterID)
	c.clusters.Delete(string(clusterID))
	if len(c.remoteClusters[secretKey]) == 0 {
		delete(c.remoteClusters, secretKey)
	}
}

func (c *ClusterStore) Get(secretKey string, clusterID cluster.ID) *Cluster {
	c.RLock()
	defer c.RUnlock()
	if _, ok := c.remoteClusters[secretKey]; !ok {
		return nil
	}
	return c.remoteClusters[secretKey][clusterID]
}

func (c *ClusterStore) Contains(clusterID cluster.ID) bool {
	c.RLock()
	defer c.RUnlock()
	return c.clusters.Contains(string(clusterID))
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

	controller := &Controller{
		namespace:          namespace,
		localClusterID:     localClusterID,
		localClusterClient: kubeclientset,
		cs:                 newClustersStore(),
		informer:           secretsInformer,
	}
	controller.queue = controllers.NewQueue("multicluster secret", controllers.WithReconciler(controller.processItem))

	secretsInformer.AddEventHandler(controllers.ObjectHandler(controller.queue.AddObject))
	return controller
}

// Run starts the controller until it receives a message over stopCh
func (c *Controller) Run(stopCh <-chan struct{}) error {
	// run handlers for the local cluster; do not store this *Cluster in the ClusterStore or give it a SyncTimeout
	// this is done outside the goroutine, we should block other Run/startFuncs until this is registered
	localCluster := &Cluster{Client: c.localClusterClient, ID: c.localClusterID}
	if err := c.handleAdd(localCluster, stopCh); err != nil {
		return fmt.Errorf("failed initializing local cluster %s: %v", c.localClusterID, err)
	}
	go func() {
		t0 := time.Now()
		log.Info("Starting multicluster remote secrets controller")

		go c.informer.Run(stopCh)

		if !kube.WaitForCacheSync(stopCh, c.informer.HasSynced) {
			log.Error("Failed to sync multicluster remote secrets controller cache")
			return
		}
		log.Infof("multicluster remote secrets controller cache synced in %v", time.Since(t0))
		if features.RemoteClusterTimeout != 0 {
			time.AfterFunc(features.RemoteClusterTimeout, func() {
				c.remoteSyncTimeout.Store(true)
			})
		}
		c.queue.Run(stopCh)
	}()
	return nil
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
	if !c.queue.HasSynced() {
		log.Debug("secret controller did not sync secrets presented at startup")
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

func (c *Controller) processItem(key types.NamespacedName) error {
	log.Infof("processing secret event for secret %s", key)
	obj, exists, err := c.informer.GetIndexer().GetByKey(key.String())
	if err != nil {
		return fmt.Errorf("error fetching object %s error: %v", key, err)
	}
	if exists {
		log.Debugf("secret %s exists in informer cache, processing it", key)
		c.addSecret(key, obj.(*corev1.Secret))
	} else {
		log.Debugf("secret %s does not exist in informer cache, deleting it", key)
		c.deleteSecret(key.String())
	}
	remoteClusters.Record(float64(c.cs.Len()))

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
	if err := sanitizeKubeConfig(*rawConfig, features.InsecureKubeConfigOptions); err != nil {
		return nil, fmt.Errorf("kubeconfig is not allowed: %v", err)
	}

	clientConfig := clientcmd.NewDefaultClientConfig(*rawConfig, &clientcmd.ConfigOverrides{})

	clients, err := kube.NewClient(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kube clients: %v", err)
	}
	return clients, nil
}

// sanitizeKubeConfig sanitizes a kubeconfig file to strip out insecure settings which may leak
// confidential materials.
// See https://github.com/kubernetes/kubectl/issues/697
func sanitizeKubeConfig(config api.Config, allowlist sets.Set) error {
	for k, auths := range config.AuthInfos {
		if ap := auths.AuthProvider; ap != nil {
			// We currently are importing 5 authenticators: gcp, azure, exec, and openstack
			switch ap.Name {
			case "oidc":
				// OIDC is safe as it doesn't read files or execute code.
				// create-remote-secret specifically supports OIDC so its probably important to not break this.
			default:
				if !allowlist.Contains(ap.Name) {
					// All the others - gcp, azure, exec, and openstack - are unsafe
					return fmt.Errorf("auth provider %s is not allowed", ap.Name)
				}
			}
		}
		if auths.ClientKey != "" && !allowlist.Contains("clientKey") {
			return fmt.Errorf("clientKey is not allowed")
		}
		if auths.ClientCertificate != "" && !allowlist.Contains("clientCertificate") {
			return fmt.Errorf("clientCertificate is not allowed")
		}
		if auths.TokenFile != "" && !allowlist.Contains("tokenFile") {
			return fmt.Errorf("tokenFile is not allowed")
		}
		if auths.Exec != nil && !allowlist.Contains("exec") {
			return fmt.Errorf("exec is not allowed")
		}
		// Reconstruct the AuthInfo so if a new field is added we will not include it without review
		config.AuthInfos[k] = &api.AuthInfo{
			// LocationOfOrigin: Not needed
			ClientCertificate:     auths.ClientCertificate,
			ClientCertificateData: auths.ClientCertificateData,
			ClientKey:             auths.ClientKey,
			ClientKeyData:         auths.ClientKeyData,
			Token:                 auths.Token,
			TokenFile:             auths.TokenFile,
			Impersonate:           auths.Impersonate,
			ImpersonateGroups:     auths.ImpersonateGroups,
			ImpersonateUserExtra:  auths.ImpersonateUserExtra,
			Username:              auths.Username,
			Password:              auths.Password,
			AuthProvider:          auths.AuthProvider, // Included because it is sanitized above
			Exec:                  auths.Exec,
			// Extensions: Not needed,
		}

		// Other relevant fields that are not acted on:
		// * Cluster.Server (and ProxyURL). This allows the user to send requests to arbitrary URLs, enabling potential SSRF attacks.
		//   However, we don't actually know what valid URLs are, so we cannot reasonably constrain this. Instead,
		//   we try to limit what confidential information could be exfiltrated (from AuthInfo). Additionally, the user cannot control
		//   the paths we send requests to, limiting potential attack scope.
		// * Cluster.CertificateAuthority. While this reads from files, the result is not attached to the request and is instead
		//   entirely local
	}
	return nil
}

func (c *Controller) createRemoteCluster(kubeConfig []byte, clusterID string) (*Cluster, error) {
	clients, err := BuildClientsFromConfig(kubeConfig)
	if err != nil {
		return nil, err
	}
	return &Cluster{
		ID:     cluster.ID(clusterID),
		Client: clients,
		stop:   make(chan struct{}),
		// for use inside the package, to close on cleanup
		initialSync:   atomic.NewBool(false),
		SyncTimeout:   &c.remoteSyncTimeout,
		kubeConfigSha: sha256.Sum256(kubeConfig),
	}, nil
}

func (c *Controller) addSecret(name types.NamespacedName, s *corev1.Secret) {
	secretKey := name.String()
	// First delete clusters
	existingClusters := c.cs.GetExistingClustersFor(secretKey)
	for _, existingCluster := range existingClusters {
		if _, ok := s.Data[string(existingCluster.ID)]; !ok {
			c.deleteCluster(secretKey, existingCluster.ID)
		}
	}

	for clusterID, kubeConfig := range s.Data {
		if cluster.ID(clusterID) == c.localClusterID {
			log.Infof("ignoring cluster %v from secret %v as it would overwrite the local cluster", clusterID, secretKey)
			continue
		}

		action, callback := "Adding", c.handleAdd
		if prev := c.cs.Get(secretKey, cluster.ID(clusterID)); prev != nil {
			action, callback = "Updating", c.handleUpdate
			// clusterID must be unique even across multiple secrets
			kubeConfigSha := sha256.Sum256(kubeConfig)
			if bytes.Equal(kubeConfigSha[:], prev.kubeConfigSha[:]) {
				log.Infof("skipping update of cluster_id=%v from secret=%v: (kubeconfig are identical)", clusterID, secretKey)
				continue
			}
		} else if c.cs.Contains(cluster.ID(clusterID)) {
			// if the cluster has been registered before by another secret, ignore the new one.
			log.Warnf("cluster %d from secret %s has already been registered", clusterID, secretKey)
			continue
		}
		log.Infof("%s cluster %v from secret %v", action, clusterID, secretKey)

		remoteCluster, err := c.createRemoteCluster(kubeConfig, clusterID)
		if err != nil {
			log.Errorf("%s cluster_id=%v from secret=%v: %v", action, clusterID, secretKey, err)
			continue
		}
		c.cs.Store(secretKey, remoteCluster.ID, remoteCluster)
		if err := callback(remoteCluster, remoteCluster.stop); err != nil {
			log.Errorf("%s cluster_id from secret=%v: %s %v", action, clusterID, secretKey, err)
			continue
		}
		log.Infof("finished callback for %s and starting to sync", clusterID)
		go remoteCluster.Run()
	}

	log.Infof("Number of remote clusters: %d", c.cs.Len())
}

func (c *Controller) deleteSecret(secretKey string) {
	for _, cluster := range c.cs.GetExistingClustersFor(secretKey) {
		if cluster.ID == c.localClusterID {
			log.Infof("ignoring delete cluster %v from secret %v as it would overwrite the local cluster", c.localClusterID, secretKey)
			continue
		}
		log.Infof("Deleting cluster_id=%v configured by secret=%v", cluster.ID, secretKey)
		close(cluster.stop)
		err := c.handleDelete(cluster.ID)
		if err != nil {
			log.Errorf("Error removing cluster_id=%v configured by secret=%v: %v",
				cluster.ID, secretKey, err)
		}
		c.cs.Delete(secretKey, cluster.ID)
	}

	log.Infof("Number of remote clusters: %d", c.cs.Len())
}

func (c *Controller) deleteCluster(secretKey string, clusterID cluster.ID) {
	c.cs.Lock()
	defer func() {
		c.cs.Unlock()
		log.Infof("Number of remote clusters: %d", c.cs.Len())
	}()
	log.Infof("Deleting cluster_id=%v configured by secret=%v", clusterID, secretKey)
	close(c.cs.remoteClusters[secretKey][clusterID].stop)
	err := c.handleDelete(clusterID)
	if err != nil {
		log.Errorf("Error removing cluster_id=%v configured by secret=%v: %v",
			clusterID, secretKey, err)
	}
	delete(c.cs.remoteClusters[secretKey], clusterID)
}

func (c *Controller) handleAdd(cluster *Cluster, stop <-chan struct{}) error {
	var errs *multierror.Error
	for _, handler := range c.handlers {
		errs = multierror.Append(errs, handler.ClusterAdded(cluster, stop))
	}
	return errs.ErrorOrNil()
}

func (c *Controller) handleUpdate(cluster *Cluster, stop <-chan struct{}) error {
	var errs *multierror.Error
	for _, handler := range c.handlers {
		errs = multierror.Append(errs, handler.ClusterUpdated(cluster, stop))
	}
	return errs.ErrorOrNil()
}

func (c *Controller) handleDelete(key cluster.ID) error {
	var errs *multierror.Error
	for _, handler := range c.handlers {
		errs = multierror.Append(errs, handler.ClusterDeleted(key))
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
