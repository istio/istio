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

	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/server"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/multicluster"
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

	// Gather all of the member clusters.
	var clusterIDs []cluster.ID
	for clusterID := range m.remoteKubeControllers {
		clusterIDs = append(clusterIDs, clusterID)
	}
	m.m.Unlock()

	// Remove all of the clusters.
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
	kubeRegistry, options, configCluster, err := m.addCluster(cluster)
	if err != nil {
		m.m.Unlock()
		return err
	}
	m.m.Unlock()
	// clusterStopCh is a channel that will be closed when this cluster removed.
	return m.initializeCluster(cluster, kubeRegistry, *options, configCluster, clusterStopCh)
}

// ClusterUpdated is passed to the secret controller as a callback to be called
// when a remote cluster is updated.
func (m *Multicluster) ClusterUpdated(cluster *multicluster.Cluster, stop <-chan struct{}) error {
	m.m.Lock()
	m.deleteCluster(cluster.ID)
	kubeRegistry, options, configCluster, err := m.addCluster(cluster)
	if err != nil {
		m.m.Unlock()
		return err
	}
	m.m.Unlock()
	// clusterStopCh is a channel that will be closed when this cluster removed.
	return m.initializeCluster(cluster, kubeRegistry, *options, configCluster, stop)
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
func (m *Multicluster) addCluster(cluster *multicluster.Cluster) (*Controller, *Options, bool, error) {
	if m.closing {
		return nil, nil, false, fmt.Errorf("failed adding member cluster %s: server shutting down", cluster.ID)
	}

	client := cluster.Client
	configCluster := m.opts.ClusterID == cluster.ID

	options := m.opts
	options.ClusterID = cluster.ID
	// different clusters may have different k8s version, re-apply conditional default
	options.EndpointMode = DetectEndpointMode(client)
	if !configCluster {
		options.SyncTimeout = features.RemoteClusterTimeout
	}
	log.Infof("Initializing Kubernetes service registry %q", options.ClusterID)
	kubeRegistry := NewController(client, options)
	m.remoteKubeControllers[cluster.ID] = &kubeController{
		Controller: kubeRegistry,
	}
	return kubeRegistry, &options, configCluster, nil
}

// initializeCluster initializes the cluster by setting various handlers.
func (m *Multicluster) initializeCluster(cluster *multicluster.Cluster, kubeRegistry *Controller, options Options,
	configCluster bool, clusterStopCh <-chan struct{},
) error {
	client := cluster.Client

	if m.serviceEntryController != nil && features.EnableServiceEntrySelectPods {
		// Add an instance handler in the kubernetes registry to notify service entry store about pod events
		kubeRegistry.AppendWorkloadHandler(m.serviceEntryController.WorkloadInstanceHandler)
	}

	// TODO implement deduping in aggregate registry to allow multiple k8s registries to handle WorkloadEntry
	if m.serviceEntryController != nil && configCluster {
		// Add an instance handler in the service entry store to notify kubernetes about workload entry events
		m.serviceEntryController.AppendWorkloadHandler(kubeRegistry.WorkloadInstanceHandler)
	} else if features.WorkloadEntryCrossCluster {
		// TODO only do this for non-remotes, can't guarantee CRDs in remotes (depends on https://github.com/istio/istio/pull/29824)
		if configStore, err := createWleConfigStore(client, m.revision, options); err == nil {
			m.remoteKubeControllers[cluster.ID].workloadEntryController = serviceentry.NewWorkloadEntryController(
				configStore, model.MakeIstioStore(configStore), options.XDSUpdater,
				serviceentry.WithClusterID(cluster.ID),
				serviceentry.WithNetworkIDCb(kubeRegistry.Network))
			// Services can select WorkloadEntry from the same cluster. We only duplicate the Service to configure kube-dns.
			m.remoteKubeControllers[cluster.ID].workloadEntryController.AppendWorkloadHandler(kubeRegistry.WorkloadInstanceHandler)
			// ServiceEntry selects WorkloadEntry from remote cluster
			m.remoteKubeControllers[cluster.ID].workloadEntryController.AppendWorkloadHandler(m.serviceEntryController.WorkloadInstanceHandler)
			m.opts.MeshServiceController.AddRegistryAndRun(m.remoteKubeControllers[cluster.ID].workloadEntryController, clusterStopCh)
			go configStore.Run(clusterStopCh)
		} else {
			return fmt.Errorf("failed creating config configStore for cluster %s: %v", cluster.ID, err)
		}
	}

	// run after WorkloadHandler is added
	m.opts.MeshServiceController.AddRegistryAndRun(kubeRegistry, clusterStopCh)

	shouldLead := m.checkShouldLead(client, options.SystemNamespace)
	log.Infof("should join leader-election for cluster %s: %t", cluster.ID, shouldLead)

	if m.startNsController && (shouldLead || configCluster) {
		// Block server exit on graceful termination of the leader controller.
		m.s.RunComponentAsyncAndWait(func(_ <-chan struct{}) error {
			log.Infof("joining leader-election for %s in %s on cluster %s",
				leaderelection.NamespaceController, options.SystemNamespace, options.ClusterID)
			election := leaderelection.
				NewLeaderElectionMulticluster(options.SystemNamespace, m.serverID, leaderelection.NamespaceController, m.revision, !configCluster, client).
				AddRunFunction(func(leaderStop <-chan struct{}) {
					log.Infof("starting namespace controller for cluster %s", cluster.ID)
					nc := NewNamespaceController(client, m.caBundleWatcher)
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

	// setting up the serviceexport controller if and only if it is turned on in the meshconfig.
	if features.EnableMCSAutoExport {
		log.Infof("joining leader-election for %s in %s on cluster %s",
			leaderelection.ServiceExportController, options.SystemNamespace, options.ClusterID)
		// Block server exit on graceful termination of the leader controller.
		m.s.RunComponentAsyncAndWait(func(_ <-chan struct{}) error {
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

// Comma-separated list of clusters (or * for any) with an istiod that should
// attempt leader election for a remote cluster.
const istiodClusterAnnotation = "topology.istio.io/controlPlaneClusters" // TODO make proper API annotation.TopologyControlPlaneClusters.Name

// checkShouldLead returns true if the caller should attempt leader election for a remote cluster.
func (m *Multicluster) checkShouldLead(client kubelib.Client, systemNamespace string) bool {
	if features.ExternalIstiod {
		namespace, err := client.Kube().CoreV1().Namespaces().Get(context.TODO(), systemNamespace, metav1.GetOptions{})
		if err == nil {
			// found same system namespace on the remote cluster so check if we are a selected istiod to lead
			istiodCluster, found := namespace.Annotations[istiodClusterAnnotation]
			if found {
				localCluster := string(m.opts.ClusterID)
				for _, cluster := range strings.Split(istiodCluster, ",") {
					if cluster == "*" || cluster == localCluster {
						return true
					}
				}
			}
		} else if !errors.IsNotFound(err) {
			// TODO use a namespace informer to handle transient errors and to also allow dynamic updates
			log.Errorf("failed to access system namespace %s: %v", systemNamespace, err)
			// For now, fail open in case it's just a transient error. This may result in some unexpected error messages in istiod
			// logs and/or some unnecessary attempts at leader election, but a local istiod will always win in those cases.
			return true
		}
	}
	return false
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
		MustAdd(collections.IstioNetworkingV1Alpha3Workloadentries).
		Build()
	return crdclient.NewForSchemas(client, revision, opts.DomainSuffix, workloadEntriesSchemas)
}
