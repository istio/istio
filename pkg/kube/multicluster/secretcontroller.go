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
	"time"

	"github.com/hashicorp/go-multierror"
	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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
	// maxRetries is the number of times a multicluster secret will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the
	// sequence of delays between successive queuings of a service.
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries = 15
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
	namespace           string
	configClusterID     cluster.ID
	configClusterClient kube.Client
	queue               controllers.Queue
	informer            cache.SharedIndexInformer

	cs *ClusterStore

	handlers []ClusterHandler
}

// NewController returns a new secret controller
func NewController(kubeclientset kube.Client, namespace string, clusterID cluster.ID) *Controller {
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

		localKubeClient, err := kube.NewClient(kube.NewClientConfigForRestConfig(config))
		if err != nil {
			log.Errorf("Could not create a client to access local cluster API server: %v", err)
			return nil
		}
		log.Infof("Successfully created in cluster kubeclient at %s", localKubeClient.RESTConfig().Host)
		informerClient = localKubeClient
	}

	secretsInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
				opts.LabelSelector = MultiClusterSecretLabel + "=true"
				return informerClient.Kube().CoreV1().Secrets(namespace).List(context.TODO(), opts)
			},
			WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
				opts.LabelSelector = MultiClusterSecretLabel + "=true"
				return informerClient.Kube().CoreV1().Secrets(namespace).Watch(context.TODO(), opts)
			},
		},
		&corev1.Secret{}, 0, cache.Indexers{},
	)
	_ = secretsInformer.SetTransform(kube.StripUnusedFields)

	// init gauges
	localClusters.Record(1.0)
	remoteClusters.Record(0.0)

	controller := &Controller{
		namespace:           namespace,
		configClusterID:     clusterID,
		configClusterClient: kubeclientset,
		cs:                  newClustersStore(),
		informer:            secretsInformer,
	}

	controller.queue = controllers.NewQueue("multicluster secret",
		controllers.WithMaxAttempts(maxRetries),
		controllers.WithReconciler(controller.processItem))

	secretsInformer.AddEventHandler(controllers.ObjectHandler(controller.queue.AddObject))
	return controller
}

func (c *Controller) AddHandler(h ClusterHandler) {
	c.handlers = append(c.handlers, h)
}

// Run starts the controller until it receives a message over stopCh
func (c *Controller) Run(stopCh <-chan struct{}) error {
	// run handlers for the config cluster; do not store this *Cluster in the ClusterStore or give it a SyncTimeout
	// this is done outside the goroutine, we should block other Run/startFuncs until this is registered
	configCluster := &Cluster{Client: c.configClusterClient, ID: c.configClusterID}
	if err := c.handleAdd(configCluster, stopCh); err != nil {
		return fmt.Errorf("failed initializing primary cluster %s: %v", c.configClusterID, err)
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
		c.queue.Run(stopCh)
	}()
	return nil
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
	return synced
}

func (c *Controller) processItem(key types.NamespacedName) error {
	log.Infof("processing secret event for secret %s", key)
	obj, exists, err := c.informer.GetIndexer().GetByKey(key.String())
	if err != nil {
		return fmt.Errorf("error fetching object %s: %v", key, err)
	}
	if exists {
		log.Debugf("secret %s exists in informer cache, processing it", key)
		if err := c.addSecret(key, obj.(*corev1.Secret)); err != nil {
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
			c.deleteCluster(secretKey, existingCluster.ID)
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
		if err := callback(remoteCluster, remoteCluster.stop); err != nil {
			remoteCluster.Stop()
			logger.Errorf("%s cluster: initialize cluster failed: %v", action, err)
			c.cs.Delete(secretKey, remoteCluster.ID)
			err = fmt.Errorf("%s cluster_id=%s from secret=%v: %w", action, clusterID, secretKey, err)
			errs = multierror.Append(errs, err)
			continue
		}
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
		log.Infof("Deleting cluster_id=%v configured by secret=%v", cluster.ID, secretKey)
		cluster.Stop()
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
	c.cs.remoteClusters[secretKey][clusterID].Stop()
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
