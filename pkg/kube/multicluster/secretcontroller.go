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
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/hashicorp/go-multierror"
	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

const (
	MultiClusterSecretLabel = "istio/multiCluster"
)

var (
	clusterLabel = monitoring.CreateLabel("cluster")
	statusLabel  = monitoring.CreateLabel("status")
	timeouts     = monitoring.NewSum(
		"remote_cluster_sync_timeouts_total",
		"Number of times remote clusters took too long to sync, causing slow startup that excludes remote clusters.",
	)

	clusterType = monitoring.CreateLabel("cluster_type")

	clustersCount = monitoring.NewGauge(
		"istiod_managed_clusters",
		"Number of clusters managed by istiod",
	)

	localClusters  = clustersCount.With(clusterType.Value("local"))
	remoteClusters = clustersCount.With(clusterType.Value("remote"))

	remoteClusterSyncState = monitoring.NewGauge(
		"istiod_remote_cluster_sync_status",
		"Current synchronization state of remote clusters managed by istiod. "+
			"One sample per cluster and state; a value of 1 indicates the cluster is in that state.",
	)
)

type handler interface {
	clusterAdded(cluster *Cluster) ComponentConstraint
	clusterUpdated(cluster *Cluster) ComponentConstraint
	clusterDeleted(clusterID cluster.ID)
	HasSynced() bool
}

// ClientBuilder builds a new kube.Client from a kubeconfig. Mocked out for testing
type ClientBuilder = func(kubeConfig []byte, clusterId cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error)

// Controller is the controller implementation for Secret resources
type Controller struct {
	namespace            string
	configClusterID      cluster.ID
	configCluster        *Cluster
	configClusterSyncers []ComponentConstraint

	ClientBuilder ClientBuilder

	queue           controllers.Queue
	secrets         kclient.Client[*corev1.Secret]
	configOverrides []func(*rest.Config)

	cs *ClusterStore

	meshWatcher    mesh.Watcher
	handlers       []handler
	metricInterval time.Duration
}

// NewController returns a new secret controller
func NewController(kubeclientset kube.Client, namespace string, clusterID cluster.ID,
	meshWatcher mesh.Watcher, configOverrides ...func(*rest.Config),
) *Controller {
	informerClient := kubeclientset

	// When these two are set to true, Istiod will be watching the namespace in which
	// Istiod is running on the external cluster. Use the inCluster credentials to
	// create a kubeclientset
	if features.LocalClusterSecretWatcher && features.ExternalIstiod {
		config, err := kube.InClusterConfig(configOverrides...)
		if err != nil {
			log.Errorf("Could not get istiod incluster configuration: %v", err)
			return nil
		}
		log.Info("Successfully retrieved incluster config.")

		localKubeClient, err := kube.NewClient(kube.NewClientConfigForRestConfig(config), clusterID)
		if err != nil {
			log.Errorf("Could not create a client to access local cluster API server: %v", err)
			return nil
		}
		log.Infof("Successfully created in cluster kubeclient at %s", localKubeClient.RESTConfig().Host)
		informerClient = localKubeClient
	}

	secrets := kclient.NewFiltered[*corev1.Secret](informerClient, kclient.Filter{
		Namespace:     namespace,
		LabelSelector: MultiClusterSecretLabel + "=true",
	})

	// init gauges
	localClusters.Record(1.0)
	remoteClusters.Record(0.0)

	controller := &Controller{
		ClientBuilder:   DefaultBuildClientsFromConfig,
		namespace:       namespace,
		configClusterID: clusterID,
		configCluster:   &Cluster{Client: kubeclientset, ID: clusterID},
		cs:              newClustersStore(),
		secrets:         secrets,
		configOverrides: configOverrides,
		meshWatcher:     meshWatcher,
		metricInterval:  15 * time.Second,
	}

	// Queue does NOT retry. The only error that can occur is if the kubeconfig is
	// malformed. This is a static analysis that cannot be resolved by retry. Actual
	// connectivity issues would result in HasSynced returning false rather than an
	// error. In this case, things will be retried automatically (via informers or
	// others), and the time is capped by RemoteClusterTimeout).
	controller.queue = controllers.NewQueue("multicluster secret",
		controllers.WithReconciler(controller.processItem))

	secrets.AddEventHandler(controllers.ObjectHandler(controller.queue.AddObject))
	return controller
}

type ComponentBuilder interface {
	registerHandler(h handler)
}

// BuildMultiClusterComponent constructs a new multicluster component. For each cluster, the constructor will be called.
// If the cluster is removed, the T.Close() method will be called.
// Constructors MUST not do blocking IO; they will block other operations.
// During a cluster update, a new component is constructed before the old one is removed for seamless migration.
func BuildMultiClusterComponent[T ComponentConstraint](c ComponentBuilder, constructor func(cluster *Cluster) T) *Component[T] {
	comp := &Component[T]{
		constructor: constructor,
		clusters:    make(map[cluster.ID]T),
	}
	c.registerHandler(comp)
	return comp
}

func (c *Controller) registerHandler(h handler) {
	// Intentionally no lock. The controller today requires that handlers are registered before execution and not in parallel.
	c.handlers = append(c.handlers, h)
}

// Run starts the controller until it receives a message over stopCh
func (c *Controller) Run(stopCh <-chan struct{}) error {
	// run handlers for the config cluster; do not store this *Cluster in the ClusterStore or give it a SyncTimeout
	// this is done outside the goroutine, we should block other Run/startFuncs until this is registered
	c.configClusterSyncers = c.handleAdd(c.configCluster)
	go func() {
		t0 := time.Now()
		log.Info("Starting multicluster remote secrets controller")
		// we need to start here when local cluster secret watcher enabled
		if features.LocalClusterSecretWatcher && features.ExternalIstiod {
			c.secrets.Start(stopCh)
		}
		if !kube.WaitForCacheSync("multicluster remote secrets", stopCh, c.secrets.HasSynced) {
			c.queue.ShutDownEarly()
			return
		}
		log.Infof("multicluster remote secrets controller cache synced in %v", time.Since(t0))
		c.queue.Run(stopCh)
		c.handleDelete(c.configClusterID)
	}()
	go c.runClusterSyncMetricsUpdater(stopCh)
	return nil
}

func (c *Controller) runClusterSyncMetricsUpdater(stopCh <-chan struct{}) {
	ticker := time.NewTicker(c.metricInterval)
	defer ticker.Stop()

	// Track previously seen clusters so we can clear metrics for removed ones if desired.
	lastSeen := map[cluster.ID]struct{}{}

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
		}

		currentSeen := map[cluster.ID]struct{}{}

		// Remote clusters from secrets
		for _, clusters := range c.cs.All() {
			for id, rc := range clusters {
				status := rc.SyncStatus()
				currentSeen[id] = struct{}{}

				c.recordClusterSyncState(id, status)
			}
		}

		// Clear metrics for clusters that disappeared.
		for id := range lastSeen {
			if _, ok := currentSeen[id]; !ok {
				c.clearClusterSyncState(id)
			}
		}

		lastSeen = currentSeen
	}
}

func (c *Controller) recordClusterSyncState(clusterID cluster.ID, status string) {
	for _, s := range []string{
		SyncStatusSynced,
		SyncStatusSyncing,
		SyncStatusTimeout,
		SyncStatusClosed,
	} {
		v := 0.0
		if s == status {
			v = 1.0
		}
		remoteClusterSyncState.With(
			clusterLabel.Value(string(clusterID)),
			clusterType.Value("remote"),
			statusLabel.Value(s),
		).Record(v)
	}
}

func (c *Controller) clearClusterSyncState(clusterID cluster.ID) {
	for _, s := range []string{
		SyncStatusSynced,
		SyncStatusSyncing,
		SyncStatusTimeout,
		SyncStatusClosed,
	} {
		remoteClusterSyncState.With(
			clusterLabel.Value(string(clusterID)),
			clusterType.Value("remote"),
			statusLabel.Value(s),
		).Record(0.0)
	}
}

func (c *Controller) HasSynced() bool {
	if !c.queue.HasSynced() {
		log.Debug("secret controller did not sync secrets presented at startup")
		// we haven't finished processing the secrets that were present at startup
		return false
	}
	// Check all config cluster components are synced
	// c.ConfigClusterHandler.HasSynced does not work; config cluster is handle specially
	if !kube.AllSynced(c.configClusterSyncers) {
		return false
	}
	// Check all remote clusters are synced (or timed out)
	return c.cs.HasSynced()
}

func (c *Controller) processItem(key types.NamespacedName) error {
	log.Infof("processing secret event for secret %s", key)
	scrt := c.secrets.Get(key.Name, key.Namespace)
	if scrt != nil {
		log.Debugf("secret %s exists in informer cache, processing it", key)
		if err := c.addSecret(key, scrt); err != nil {
			return fmt.Errorf("error adding secret %s: %v", key, err)
		}
	} else {
		log.Debugf("secret %s does not exist in informer cache, deleting it", key)
		c.deleteSecret(key.String())
	}
	remoteClusters.Record(float64(c.cs.Len()))

	return nil
}

// DefaultBuildClientsFromConfig creates kube.Clients from the provided kubeconfig. This is overridden for testing only
func DefaultBuildClientsFromConfig(kubeConfig []byte, clusterID cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
	restConfig, err := kube.NewUntrustedRestConfig(kubeConfig, configOverrides...)
	if err != nil {
		return nil, err
	}

	clients, err := kube.NewClient(kube.NewClientConfigForRestConfig(restConfig), clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to create kube clients: %v", err)
	}

	// We need to read remote gateways in ambient multicluster mode
	if features.WorkloadEntryCrossCluster || features.EnableAmbientMultiNetwork {
		clients = kube.EnableCrdWatcher(clients)
	}

	return clients, nil
}

func (c *Controller) createRemoteCluster(kubeConfig []byte, clusterID string) (*Cluster, error) {
	clients, err := c.ClientBuilder(kubeConfig, cluster.ID(clusterID), c.configOverrides...)
	if err != nil {
		return nil, err
	}
	return &Cluster{
		ID:     cluster.ID(clusterID),
		Client: clients,
		stop:   make(chan struct{}),
		// for use inside the package, to close on cleanup
		initialSync:        atomic.NewBool(false),
		initialSyncTimeout: atomic.NewBool(false),
		kubeConfigSha:      sha256.Sum256(kubeConfig),
	}, nil
}

func (c *Controller) addSecret(name types.NamespacedName, s *corev1.Secret) error {
	secretKey := name.String()
	// First delete clusters
	existingClusters := c.cs.GetExistingClustersFor(secretKey)
	for _, existingCluster := range existingClusters {
		if _, ok := s.Data[string(existingCluster.ID)]; !ok {
			c.deleteCluster(secretKey, existingCluster)
		}
	}

	var errs *multierror.Error
	for clusterID, kubeConfig := range s.Data {
		logger := log.WithLabels("cluster", clusterID, "secret", secretKey)
		if cluster.ID(clusterID) == c.configClusterID {
			logger.Infof("ignoring cluster as it would overwrite the config cluster")
			continue
		}

		action := Add
		if prev := c.cs.Get(secretKey, cluster.ID(clusterID)); prev != nil {
			action = Update
			// clusterID must be unique even across multiple secrets
			kubeConfigSha := sha256.Sum256(kubeConfig)
			if bytes.Equal(kubeConfigSha[:], prev.kubeConfigSha[:]) {
				logger.Infof("skipping update (kubeconfig are identical)")
				continue
			}
			// stop previous remote cluster
			prev.Stop()
			prev.Client.Shutdown() // Shutdown all of the informers so that the goroutines won't leak
		} else if c.cs.Contains(cluster.ID(clusterID)) {
			// if the cluster has been registered before by another secret, ignore the new one.
			logger.Warnf("cluster has already been registered")
			continue
		}
		logger.Infof("%s cluster", action)

		remoteCluster, err := c.createRemoteCluster(kubeConfig, clusterID)
		if err != nil {
			logger.Errorf("%s cluster: create remote cluster failed: %v", action, err)
			errs = multierror.Append(errs, err)
			continue
		}
		// We run cluster async so we do not block, as this requires actually connecting to the cluster and loading configuration.
		c.cs.Store(secretKey, remoteCluster.ID, remoteCluster)
		go func() {
			remoteCluster.Run(c.meshWatcher, c.handlers, action)
		}()
	}

	log.Infof("Number of remote clusters: %d", c.cs.Len())
	return errs.ErrorOrNil()
}

func (c *Controller) deleteSecret(secretKey string) {
	for _, cluster := range c.cs.GetExistingClustersFor(secretKey) {
		if cluster.ID == c.configClusterID {
			log.Infof("ignoring delete cluster %v from secret %v as it would overwrite the config cluster", c.configClusterID, secretKey)
			continue
		}

		c.deleteCluster(secretKey, cluster)
	}

	log.Infof("Number of remote clusters: %d", c.cs.Len())
}

func (c *Controller) deleteCluster(secretKey string, cluster *Cluster) {
	log.Infof("Deleting cluster_id=%v configured by secret=%v", cluster.ID, secretKey)
	cluster.Stop()
	c.handleDelete(cluster.ID)
	c.cs.Delete(secretKey, cluster.ID)
	cluster.Client.Shutdown() // Shutdown all of the informers so that the goroutines won't leak

	log.Infof("Number of remote clusters: %d", c.cs.Len())
}

func (c *Controller) handleAdd(cluster *Cluster) []ComponentConstraint {
	syncers := make([]ComponentConstraint, 0, len(c.handlers))
	for _, handler := range c.handlers {
		syncers = append(syncers, handler.clusterAdded(cluster))
	}
	return syncers
}

func (c *Controller) handleDelete(key cluster.ID) {
	for _, handler := range c.handlers {
		handler.clusterDeleted(key)
	}
}

// ListRemoteClusters provides debug info about connected remote clusters.
func (c *Controller) ListRemoteClusters() []cluster.DebugInfo {
	// Start with just the config cluster
	configCluster := SyncStatusSyncing
	if kube.AllSynced(c.configClusterSyncers) {
		configCluster = SyncStatusSynced
	}
	out := []cluster.DebugInfo{{
		ID:         c.configClusterID,
		SyncStatus: configCluster,
	}}
	// Append each cluster derived from secrets
	for secretName, clusters := range c.cs.All() {
		for clusterID, c := range clusters {
			out = append(out, cluster.DebugInfo{
				ID:         clusterID,
				SecretName: secretName,
				SyncStatus: c.SyncStatus(),
			})
		}
	}
	return out
}

func (c *Controller) GetRemoteKubeClient(clusterID cluster.ID) kubernetes.Interface {
	if remoteCluster := c.cs.GetByID(clusterID); remoteCluster != nil {
		return remoteCluster.Client.Kube()
	}
	return nil
}

func (c *Controller) ListClusters() []cluster.ID {
	return slices.Map(sets.SortedList(c.cs.clusters), func(e string) cluster.ID {
		return cluster.ID(e)
	})
}
