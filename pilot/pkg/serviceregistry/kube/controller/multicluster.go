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

package controller

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/api/annotation"
	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/server"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pkg/backoff"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/kube/namespace"
	"istio.io/istio/pkg/webhooks"
)

const (
	// Name of the webhook config in the config - no need to change it.
	webhookName = "sidecar-injector.istio.io"
)

var _ multicluster.ClusterHandler = &Multicluster{}

type kubeController struct {
	*Controller
	workloadEntryController *serviceentry.Controller
}

// Multicluster structure holds the remote kube Controllers and multicluster specific attributes.
type Multicluster struct {
	// serverID of this pilot instance used for leader election
	serverID string

	// options to use when creating kube controllers
	opts Options

	// client for reading remote-secrets to initialize multicluster registries
	client  kubernetes.Interface
	s       server.Instance
	closing bool

	serviceEntryController *serviceentry.Controller
	configController       model.ConfigStoreController
	XDSUpdater             model.XDSUpdater

	m                     sync.Mutex // protects remoteKubeControllers
	remoteKubeControllers map[cluster.ID]*kubeController
	clusterLocal          model.ClusterLocalProvider

	startNsController bool
	caBundleWatcher   *keycertbundle.Watcher
	revision          string

	// secretNamespace where we get cluster-access secrets
	secretNamespace string
}

// NewMulticluster initializes data structure to store multicluster information
func NewMulticluster(
	serverID string,
	kc kubernetes.Interface,
	secretNamespace string,
	opts Options,
	serviceEntryController *serviceentry.Controller,
	configController model.ConfigStoreController,
	caBundleWatcher *keycertbundle.Watcher,
	revision string,
	startNsController bool,
	clusterLocal model.ClusterLocalProvider,
	s server.Instance,
) *Multicluster {
	remoteKubeController := make(map[cluster.ID]*kubeController)
	mc := &Multicluster{
		serverID:               serverID,
		opts:                   opts,
		serviceEntryController: serviceEntryController,
		configController:       configController,
		startNsController:      startNsController,
		caBundleWatcher:        caBundleWatcher,
		revision:               revision,
		XDSUpdater:             opts.XDSUpdater,
		remoteKubeControllers:  remoteKubeController,
		clusterLocal:           clusterLocal,
		secretNamespace:        secretNamespace,
		client:                 kc,
		s:                      s,
	}

	return mc
}

func (m *Multicluster) Run(stopCh <-chan struct{}) error {
	// Wait for server shutdown.
	<-stopCh
	return m.close()
}

func (m *Multicluster) close() (err error) {
	m.m.Lock()
	m.closing = true

	// Gather all the member clusters.
	var clusterIDs []cluster.ID
	for clusterID := range m.remoteKubeControllers {
		clusterIDs = append(clusterIDs, clusterID)
	}
	m.m.Unlock()

	// Remove all the clusters.
	g, _ := errgroup.WithContext(context.Background())
	for _, clusterID := range clusterIDs {
		clusterID := clusterID
		g.Go(func() error {
			return m.ClusterDeleted(clusterID)
		})
	}
	err = g.Wait()
	return
}

// ClusterAdded is passed to the secret controller as a callback to be called
// when a remote cluster is added.  This function needs to set up all the handlers
// to watch for resources being added, deleted or changed on remote clusters.
func (m *Multicluster) ClusterAdded(cluster *multicluster.Cluster, clusterStopCh <-chan struct{}) error {
	m.m.Lock()
	kubeController, kubeRegistry, options, configCluster, err := m.addCluster(cluster)
	if err != nil {
		m.m.Unlock()
		return err
	}
	m.m.Unlock()
	// clusterStopCh is a channel that will be closed when this cluster removed.
	return m.initializeCluster(cluster, kubeController, kubeRegistry, *options, configCluster, clusterStopCh)
}

// ClusterUpdated is passed to the secret controller as a callback to be called
// when a remote cluster is updated.
func (m *Multicluster) ClusterUpdated(cluster *multicluster.Cluster, stop <-chan struct{}) error {
	m.m.Lock()
	m.deleteCluster(cluster.ID)
	kubeController, kubeRegistry, options, configCluster, err := m.addCluster(cluster)
	if err != nil {
		m.m.Unlock()
		return err
	}
	m.m.Unlock()
	// clusterStopCh is a channel that will be closed when this cluster removed.
	return m.initializeCluster(cluster, kubeController, kubeRegistry, *options, configCluster, stop)
}

// ClusterDeleted is passed to the secret controller as a callback to be called
// when a remote cluster is deleted.  Also must clear the cache so remote resources
// are removed.
func (m *Multicluster) ClusterDeleted(clusterID cluster.ID) error {
	m.m.Lock()
	m.deleteCluster(clusterID)
	m.m.Unlock()
	if m.XDSUpdater != nil {
		m.XDSUpdater.ConfigUpdate(&model.PushRequest{Full: true, Reason: []model.TriggerReason{model.ClusterUpdate}})
	}
	return nil
}

// addCluster adds cluster related resources and updates internal structures.
// This is not thread safe.
func (m *Multicluster) addCluster(cluster *multicluster.Cluster) (*kubeController, *Controller, *Options, bool, error) {
	if m.closing {
		return nil, nil, nil, false, fmt.Errorf("failed adding member cluster %s: server shutting down", cluster.ID)
	}

	client := cluster.Client
	configCluster := m.opts.ClusterID == cluster.ID

	options := m.opts
	options.ClusterID = cluster.ID
	if !configCluster {
		options.SyncTimeout = features.RemoteClusterTimeout
	}
	// config cluster's DiscoveryNamespacesFilter is shared by both configController and serviceController
	// it is initiated in bootstrap initMulticluster function, pass to service controller to update it.
	// For other clusters, it should filter by its own cluster's namespace.
	if !configCluster {
		options.DiscoveryNamespacesFilter = nil
	}
	options.ConfigController = m.configController
	log.Infof("Initializing Kubernetes service registry %q", options.ClusterID)
	options.ConfigCluster = configCluster
	kubeRegistry := NewController(client, options)
	kubeController := &kubeController{
		Controller: kubeRegistry,
	}
	m.remoteKubeControllers[cluster.ID] = kubeController
	return kubeController, kubeRegistry, &options, configCluster, nil
}

// initializeCluster initializes the cluster by setting various handlers.
func (m *Multicluster) initializeCluster(cluster *multicluster.Cluster, kubeController *kubeController, kubeRegistry *Controller,
	options Options, configCluster bool, clusterStopCh <-chan struct{},
) error {
	client := cluster.Client

	if m.serviceEntryController != nil && features.EnableServiceEntrySelectPods {
		// Add an instance handler in the kubernetes registry to notify service entry store about pod events
		kubeRegistry.AppendWorkloadHandler(m.serviceEntryController.WorkloadInstanceHandler)
	}
	if m.configController != nil && features.EnableAmbientControllers {
		m.configController.RegisterEventHandler(gvk.AuthorizationPolicy, kubeRegistry.AuthorizationPolicyHandler)
		m.configController.RegisterEventHandler(gvk.PeerAuthentication, kubeRegistry.PeerAuthenticationHandler)
	}

	if configCluster && m.serviceEntryController != nil && features.EnableEnhancedResourceScoping {
		kubeRegistry.AppendNamespaceDiscoveryHandlers(m.serviceEntryController.NamespaceDiscoveryHandler)
	}

	// TODO implement deduping in aggregate registry to allow multiple k8s registries to handle WorkloadEntry
	if features.EnableK8SServiceSelectWorkloadEntries {
		if m.serviceEntryController != nil && configCluster {
			// Add an instance handler in the service entry store to notify kubernetes about workload entry events
			m.serviceEntryController.AppendServiceHandler(kubeRegistry.ServiceEntryHandler)
			m.serviceEntryController.AppendWorkloadHandler(kubeRegistry.WorkloadInstanceHandler)
		} else if features.WorkloadEntryCrossCluster {
			// TODO only do this for non-remotes, can't guarantee CRDs in remotes (depends on https://github.com/istio/istio/pull/29824)
			if configStore, err := createWleConfigStore(client, m.revision, options); err == nil {
				kubeController.workloadEntryController = serviceentry.NewWorkloadEntryController(
					configStore, options.XDSUpdater,
					serviceentry.WithClusterID(cluster.ID),
					serviceentry.WithNetworkIDCb(kubeRegistry.Network))
				if m.serviceEntryController != nil {
					m.serviceEntryController.AppendServiceHandler(kubeRegistry.ServiceEntryHandler)
				}
				// Services can select WorkloadEntry from the same cluster. We only duplicate the Service to configure kube-dns.
				kubeController.workloadEntryController.AppendWorkloadHandler(kubeRegistry.WorkloadInstanceHandler)
				// ServiceEntry selects WorkloadEntry from remote cluster
				kubeController.workloadEntryController.AppendWorkloadHandler(m.serviceEntryController.WorkloadInstanceHandler)
				if features.EnableEnhancedResourceScoping {
					kubeRegistry.AppendNamespaceDiscoveryHandlers(kubeController.workloadEntryController.NamespaceDiscoveryHandler)
				}
				m.opts.MeshServiceController.AddRegistryAndRun(kubeController.workloadEntryController, clusterStopCh)
				go configStore.Run(clusterStopCh)
			} else {
				return fmt.Errorf("failed creating config configStore for cluster %s: %v", cluster.ID, err)
			}
		}
	}

	// namespacecontroller requires discoverySelectors only if EnableEnhancedResourceScoping feature flag is set.
	var discoveryNamespacesFilter namespace.DiscoveryNamespacesFilter
	if features.EnableEnhancedResourceScoping {
		discoveryNamespacesFilter = kubeRegistry.opts.DiscoveryNamespacesFilter
	}

	// run after WorkloadHandler is added
	m.opts.MeshServiceController.AddRegistryAndRun(kubeRegistry, clusterStopCh)

	go func() {
		var shouldLead bool
		if !configCluster {
			shouldLead = m.checkShouldLead(client, options.SystemNamespace, clusterStopCh)
			log.Infof("should join leader-election for cluster %s: %t", cluster.ID, shouldLead)
		}
		if m.startNsController && (shouldLead || configCluster) {
			// Block server exit on graceful termination of the leader controller.
			m.s.RunComponentAsyncAndWait("namespace controller", func(_ <-chan struct{}) error {
				log.Infof("joining leader-election for %s in %s on cluster %s",
					leaderelection.NamespaceController, options.SystemNamespace, options.ClusterID)
				election := leaderelection.
					NewLeaderElectionMulticluster(options.SystemNamespace, m.serverID, leaderelection.NamespaceController, m.revision, !configCluster, client).
					AddRunFunction(func(leaderStop <-chan struct{}) {
						log.Infof("starting namespace controller for cluster %s", cluster.ID)
						nc := NewNamespaceController(client, m.caBundleWatcher, discoveryNamespacesFilter)
						// Start informers again. This fixes the case where informers for namespace do not start,
						// as we create them only after acquiring the leader lock
						// Note: stop here should be the overall pilot stop, NOT the leader election stop. We are
						// basically lazy loading the informer, if we stop it when we lose the lock we will never
						// recreate it again.
						client.RunAndWait(clusterStopCh)
						nc.Run(leaderStop)
					})
				election.Run(clusterStopCh)
				return nil
			})
		}
		// Set up injection webhook patching for remote clusters we are controlling.
		// The config cluster has this patching set up elsewhere. We may eventually want to move it here.
		// We can not use leader election for webhook patching because each revision needs to patch its own
		// webhook.
		if shouldLead && !configCluster && m.caBundleWatcher != nil {
			// Patch injection webhook cert
			// This requires RBAC permissions - a low-priv Istiod should not attempt to patch but rely on
			// operator or CI/CD
			if features.InjectionWebhookConfigName != "" {
				log.Infof("initializing injection webhook cert patcher for cluster %s", cluster.ID)
				patcher, err := webhooks.NewWebhookCertPatcher(client, m.revision, webhookName, m.caBundleWatcher)
				if err != nil {
					log.Errorf("could not initialize webhook cert patcher: %v", err)
				} else {
					go patcher.Run(clusterStopCh)
				}
			}
		}
	}()

	// setting up the serviceexport controller if and only if it is turned on in the meshconfig.
	if features.EnableMCSAutoExport {
		log.Infof("joining leader-election for %s in %s on cluster %s",
			leaderelection.ServiceExportController, options.SystemNamespace, options.ClusterID)
		// Block server exit on graceful termination of the leader controller.
		m.s.RunComponentAsyncAndWait("auto serviceexport controller", func(_ <-chan struct{}) error {
			leaderelection.
				NewLeaderElectionMulticluster(options.SystemNamespace, m.serverID, leaderelection.ServiceExportController, m.revision, !configCluster, client).
				AddRunFunction(func(leaderStop <-chan struct{}) {
					serviceExportController := newAutoServiceExportController(autoServiceExportOptions{
						Client:       client,
						ClusterID:    options.ClusterID,
						DomainSuffix: options.DomainSuffix,
						ClusterLocal: m.clusterLocal,
					})
					// Start informers again. This fixes the case where informers do not start,
					// as we create them only after acquiring the leader lock
					// Note: stop here should be the overall pilot stop, NOT the leader election stop. We are
					// basically lazy loading the informer, if we stop it when we lose the lock we will never
					// recreate it again.
					client.RunAndWait(clusterStopCh)
					serviceExportController.Run(leaderStop)
				}).Run(clusterStopCh)
			return nil
		})
	}

	return nil
}

// checkShouldLead returns true if the caller should attempt leader election for a remote cluster.
func (m *Multicluster) checkShouldLead(client kubelib.Client, systemNamespace string, stop <-chan struct{}) bool {
	var res bool
	if features.ExternalIstiod {
		b := backoff.NewExponentialBackOff(backoff.DefaultOption())
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			select {
			case <-stop:
				cancel()
			case <-ctx.Done():
			}
		}()
		defer cancel()
		_ = b.RetryWithContext(ctx, func() error {
			namespace, err := client.Kube().CoreV1().Namespaces().Get(context.TODO(), systemNamespace, metav1.GetOptions{})
			if err != nil {
				if errors.IsNotFound(err) {
					return nil
				}
				return err
			}
			// found same system namespace on the remote cluster so check if we are a selected istiod to lead
			istiodCluster, found := namespace.Annotations[annotation.TopologyControlPlaneClusters.Name]
			if found {
				localCluster := string(m.opts.ClusterID)
				for _, cluster := range strings.Split(istiodCluster, ",") {
					if cluster == "*" || cluster == localCluster {
						res = true
						return nil
					}
				}
			}
			return nil
		})
	}
	return res
}

// deleteCluster deletes cluster resources and does not trigger push.
// This call is not thread safe.
func (m *Multicluster) deleteCluster(clusterID cluster.ID) {
	m.opts.MeshServiceController.UnRegisterHandlersForCluster(clusterID)
	m.opts.MeshServiceController.DeleteRegistry(clusterID, provider.Kubernetes)
	kc, ok := m.remoteKubeControllers[clusterID]
	if !ok {
		log.Infof("cluster %s does not exist, maybe caused by invalid kubeconfig", clusterID)
		return
	}
	if kc.workloadEntryController != nil {
		m.opts.MeshServiceController.DeleteRegistry(clusterID, provider.External)
	}
	if err := kc.Cleanup(); err != nil {
		log.Warnf("failed cleaning up services in %s: %v", clusterID, err)
	}
	delete(m.remoteKubeControllers, clusterID)
}

func createWleConfigStore(client kubelib.Client, revision string, opts Options) (model.ConfigStoreController, error) {
	log.Infof("Creating WorkloadEntry only config store for %s", opts.ClusterID)
	workloadEntriesSchemas := collection.NewSchemasBuilder().
		MustAdd(collections.WorkloadEntry).
		Build()
	crdOpts := crdclient.Option{Revision: revision, DomainSuffix: opts.DomainSuffix, Identifier: "mc-workload-entry-controller"}
	return crdclient.NewForSchemas(client, crdOpts, workloadEntriesSchemas)
}
