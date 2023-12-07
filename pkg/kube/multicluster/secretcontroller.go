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
	filter "istio.io/istio/pkg/kube/namespace"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/monitoring"
)

const (
	MultiClusterSecretLabel = "istio/multiCluster"
)

var (
	clusterLabel = monitoring.CreateLabel("cluster")
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
)

type ClusterHandler interface {
	ClusterAdded(cluster *Cluster, stop <-chan struct{})
	ClusterUpdated(cluster *Cluster, stop <-chan struct{})
	ClusterDeleted(clusterID cluster.ID)
}

// Controller is the controller implementation for Secret resources
type Controller struct {
	namespace           string
	configClusterID     cluster.ID
	configClusterClient kube.Client
	queue               controllers.Queue
	secrets             kclient.Client[*corev1.Secret]
	configOverrides     []func(*rest.Config)

	namespaces kclient.Client[*corev1.Namespace]

	DiscoveryNamespacesFilter filter.DiscoveryNamespacesFilter
	cs                        *ClusterStore

	handlers []ClusterHandler
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
		config, err := rest.InClusterConfig()
		if err != nil {
			log.Errorf("Could not get istiod incluster configuration: %v", err)
			return nil
		}
		log.Info("Successfully retrieved incluster config.")

		for _, overwrite := range configOverrides {
			overwrite(config)
		}

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
		namespace:           namespace,
		configClusterID:     clusterID,
		configClusterClient: kubeclientset,
		cs:                  newClustersStore(),
		secrets:             secrets,
		configOverrides:     configOverrides,
	}

	namespaces := kclient.New[*corev1.Namespace](kubeclientset)
	controller.namespaces = namespaces
	controller.DiscoveryNamespacesFilter = filter.NewDiscoveryNamespacesFilter(namespaces, meshWatcher.Mesh().GetDiscoverySelectors())
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

func (c *Controller) AddHandler(h ClusterHandler) {
	c.handlers = append(c.handlers, h)
}

// Run starts the controller until it receives a message over stopCh
func (c *Controller) Run(stopCh <-chan struct{}) error {
	// Normally, we let informers start after all controllers. However, in this case we need namespaces to start and sync
	// first, so we have DiscoveryNamespacesFilter ready to go. This avoids processing objects that would be filtered during startup.
	c.namespaces.Start(stopCh)
	// Wait for namespace informer synced, which implies discovery filter is synced as well
	if !kube.WaitForCacheSync("namespace", stopCh, c.namespaces.HasSynced) {
		return fmt.Errorf("failed to sync namespaces")
	}
	// run handlers for the config cluster; do not store this *Cluster in the ClusterStore or give it a SyncTimeout
	// this is done outside the goroutine, we should block other Run/startFuncs until this is registered
	configCluster := &Cluster{Client: c.configClusterClient, ID: c.configClusterID}
	c.handleAdd(configCluster, stopCh)
	go func() {
		t0 := time.Now()
		log.Info("Starting multicluster remote secrets controller")
		// we need to start here when local cluster secret watcher enabled
		if features.LocalClusterSecretWatcher && features.ExternalIstiod {
			c.secrets.Start(stopCh)
		}
		if !kube.WaitForCacheSync("multicluster remote secrets", stopCh, c.secrets.HasSynced) {
			return
		}
		log.Infof("multicluster remote secrets controller cache synced in %v", time.Since(t0))
		c.queue.Run(stopCh)
	}()
	return nil
}

func (c *Controller) HasSynced() bool {
	if !c.queue.HasSynced() {
		log.Debug("secret controller did not sync secrets presented at startup")
		// we haven't finished processing the secrets that were present at startup
		return false
	}
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

// BuildClientsFromConfig creates kube.Clients from the provided kubeconfig. This is overridden for testing only
var BuildClientsFromConfig = func(kubeConfig []byte, clusterId cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
	restConfig, err := kube.NewRestConfigFromContext(kubeConfig, configOverrides...)
	if err != nil {
		return nil, err
	}

	clients, err := kube.NewClient(kube.NewClientConfigForRestConfig(restConfig), clusterId)
	if err != nil {
		return nil, fmt.Errorf("failed to create kube clients: %v", err)
	}
	if features.WorkloadEntryCrossCluster {
		clients = kube.EnableCrdWatcher(clients)
	}
	return clients, nil
}

func (c *Controller) createRemoteCluster(kubeConfig []byte, clusterID string) (*Cluster, error) {
	clients, err := BuildClientsFromConfig(kubeConfig, cluster.ID(clusterID), c.configOverrides...)
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

		action, callback := "Adding", c.handleAdd
		if prev := c.cs.Get(secretKey, cluster.ID(clusterID)); prev != nil {
			action, callback = "Updating", c.handleUpdate
			// clusterID must be unique even across multiple secrets
			kubeConfigSha := sha256.Sum256(kubeConfig)
			if bytes.Equal(kubeConfigSha[:], prev.kubeConfigSha[:]) {
				logger.Infof("skipping update (kubeconfig are identical)")
				continue
			}
			// stop previous remote cluster
			prev.Stop()
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
		callback(remoteCluster, remoteCluster.stop)
		logger.Infof("finished callback for cluster and starting to sync")
		c.cs.Store(secretKey, remoteCluster.ID, remoteCluster)
		go remoteCluster.Run()
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

	log.Infof("Number of remote clusters: %d", c.cs.Len())
}

func (c *Controller) handleAdd(cluster *Cluster, stop <-chan struct{}) {
	for _, handler := range c.handlers {
		handler.ClusterAdded(cluster, stop)
	}
}

func (c *Controller) handleUpdate(cluster *Cluster, stop <-chan struct{}) {
	for _, handler := range c.handlers {
		handler.ClusterUpdated(cluster, stop)
	}
}

func (c *Controller) handleDelete(key cluster.ID) {
	for _, handler := range c.handlers {
		handler.ClusterDeleted(key)
	}
}

// ListRemoteClusters provides debug info about connected remote clusters.
func (c *Controller) ListRemoteClusters() []cluster.DebugInfo {
	var out []cluster.DebugInfo
	for secretName, clusters := range c.cs.All() {
		for clusterID, c := range clusters {
			syncStatus := "syncing"
			if c.Closed() {
				syncStatus = "closed"
			} else if c.SyncDidTimeout() {
				syncStatus = "timeout"
			} else if c.HasSynced() {
				syncStatus = "synced"
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
		return remoteCluster.Client.Kube()
	}
	return nil
}
