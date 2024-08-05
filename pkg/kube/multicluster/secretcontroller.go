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
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	"istio.io/istio/pilot/pkg/features"
	alifeatures "istio.io/istio/pkg/ali/features"
	"istio.io/istio/pkg/ali/global"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	filter "istio.io/istio/pkg/kube/namespace"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/util/sets"
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
	ClusterUpdatedInNeed(cluster *Cluster)
	ClusterDeleted(clusterID cluster.ID)
	HasSynced() bool
}

// Controller is the controller implementation for Secret resources
type Controller struct {
	namespace           string
	configClusterID     cluster.ID
	configClusterClient kube.Client
	queue               controllers.Queue
	secrets             kclient.Client[*corev1.Secret]

	namespaces kclient.Client[*corev1.Namespace]

	DiscoveryNamespacesFilter filter.DiscoveryNamespacesFilter
	cs                        *ClusterStore

	handlers []ClusterHandler

	mutex sync.Mutex
}

// NewController returns a new secret controller
func NewController(kubeclientset kube.Client, namespace string, clusterID cluster.ID, meshWatcher mesh.Watcher) *Controller {
	labels := MultiClusterSecretLabel + "=true"
	if alifeatures.WatchResourcesByLabelForPrimaryCluster != "" {
		labels += ", " + alifeatures.WatchResourcesByLabelForPrimaryCluster
	}

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
		LabelSelector: labels,
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
	for _, handler := range c.handlers {
		global.CacheSyncs = append(global.CacheSyncs, handler.HasSynced)
	}

	// run handlers for the local cluster; do not store this *Cluster in the ClusterStore or give it a SyncTimeout
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
	c.mutex.Lock()
	defer c.mutex.Unlock()

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
var BuildClientsFromConfig = func(kubeConfig []byte, clusterId cluster.ID) (kube.Client, error) {
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

	clients, err := kube.NewClient(clientConfig, clusterId)
	if err != nil {
		return nil, fmt.Errorf("failed to create kube clients: %v", err)
	}
	if features.WorkloadEntryCrossCluster {
		clients = kube.EnableCrdWatcher(clients)
	}
	return clients, nil
}

// sanitizeKubeConfig sanitizes a kubeconfig file to strip out insecure settings which may leak
// confidential materials.
// See https://github.com/kubernetes/kubectl/issues/697
func sanitizeKubeConfig(config api.Config, allowlist sets.String) error {
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
	clusterInfo := ConvertToClusterInfo(clusterID)
	clients, err := BuildClientsFromConfig(kubeConfig, cluster.ID(clusterID))
	if err != nil {
		return nil, err
	}
	return &Cluster{
		ID:     cluster.ID(clusterInfo.ClusterID),
		Client: clients,
		stop:   make(chan struct{}),
		// for use inside the package, to close on cleanup
		initialSync:        atomic.NewBool(false),
		initialSyncTimeout: atomic.NewBool(false),
		kubeConfigSha:      sha256.Sum256(kubeConfig),
		ClusterInfo:        clusterInfo,
		RawKubeConfig:      kubeConfig,
		RawClusterID:       clusterID,
	}, nil
}

func (c *Controller) addSecret(name types.NamespacedName, s *corev1.Secret) error {
	clusterInfoMapping := map[string]ClusterInfo{}
	kubeConfigMapping := map[string][]byte{}
	for key, kubeConfig := range s.Data {
		clusterInfo := ConvertToClusterInfo(key)
		clusterInfoMapping[key] = clusterInfo
		kubeConfigMapping[clusterInfo.ClusterID] = kubeConfig
	}

	secretKey := name.String()
	// First delete clusters
	existingClusters := c.cs.GetExistingClustersFor(secretKey)
	for _, existingCluster := range existingClusters {
		if _, ok := kubeConfigMapping[string(existingCluster.ID)]; !ok {
			c.deleteCluster(secretKey, existingCluster)
		}
	}

	var errs *multierror.Error
	for clusterKey, kubeConfig := range s.Data {
		clusterInfo := clusterInfoMapping[clusterKey]
		if cluster.ID(clusterInfo.ClusterID) == c.configClusterID {
			log.Infof("ignoring cluster %v from secret %v as it would overwrite the config cluster", clusterInfo.ClusterID, secretKey)
			continue
		}

		action, callback := "Adding", c.handleAdd
		if prev := c.cs.Get(secretKey, cluster.ID(clusterInfo.ClusterID)); prev != nil {
			action, callback = "Updating", c.handleUpdate
			// clusterID must be unique even across multiple secrets
			kubeConfigSha := sha256.Sum256(kubeConfig)
			if bytes.Equal(kubeConfigSha[:], prev.kubeConfigSha[:]) {
				if !prev.ClusterInfo.Equal(clusterInfo) {
					log.Infof("ClusterID %s has no changes, but the ingress extra options %s has changes", clusterInfo.ClusterID, clusterKey)
				} else {
					if prev.ClusterInfo.EnableIngressStatus != clusterInfo.EnableIngressStatus {
						log.Infof("ClusterID %s has no changes, but the ingress status %s has changes", clusterInfo.ClusterID, clusterKey)
						prev.ClusterInfo = clusterInfo
						c.handleUpdateInNeed(prev)
					}
					log.Infof("skipping update of cluster_id=%v from secret=%v: (kubeconfig are identical)", clusterInfo.ClusterID, secretKey)
					continue
				}
			}

			global.BlockPush()

			// stop previous remote cluster
			prev.Stop()
		} else if c.cs.Contains(cluster.ID(clusterInfo.ClusterID)) {
			// if the cluster has been registered before by another secret, ignore the new one.
			log.Warnf("cluster %d from secret %s has already been registered", clusterInfo.ClusterID, secretKey)
			continue
		}
		log.Infof("%s cluster %v cluster key %v from secret %v", action, clusterInfo.ClusterID, clusterKey, secretKey)

		remoteCluster, err := c.createRemoteCluster(kubeConfig, clusterKey)
		if err != nil {
			log.Errorf("%s cluster_id=%v from secret=%v: %v", action, clusterInfo.ClusterID, secretKey, err)
			continue
		}
		callback(remoteCluster, remoteCluster.stop)
		log.Infof("finished callback for %s and starting to sync", clusterInfo.ClusterID)
		c.cs.Store(secretKey, remoteCluster.ID, remoteCluster)

		if action == "Updating" {
			go global.TriggerPush(remoteCluster.stop)
		}
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

func (c *Controller) handleUpdateInNeed(cluster *Cluster) {
	for _, handler := range c.handlers {
		handler.ClusterUpdatedInNeed(cluster)
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

// added by ingress
func (c *Controller) UpdateCluster(clusterID cluster.ID) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	secretKey, prev := c.cs.GetIndexByID(clusterID)
	if prev == nil {
		log.Infof("cluster %s not found, no need to update", clusterID)
		return
	}

	remoteCluster, err := c.createRemoteCluster(prev.RawKubeConfig, prev.RawClusterID)
	if err != nil {
		log.Errorf("updating cluster_id=%v from v1beta to v1 fail, err: %v", clusterID, err)
		return
	}

	prev.Stop()
	global.BlockPush()
	defer func() {
		go global.TriggerPush(remoteCluster.stop)
	}()

	c.cs.Store(secretKey, remoteCluster.ID, remoteCluster)
	c.handleUpdate(remoteCluster, remoteCluster.stop)

	log.Infof("updating cluster_id=%v from v1beta to v1 success", clusterID)
	go remoteCluster.Run()
}
