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
	"time"

	"golang.org/x/sync/errgroup"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/server"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/secretcontroller"
	"istio.io/istio/pkg/webhooks"
	"istio.io/pkg/log"
)

const (
	// Name of the webhook config in the config - no need to change it.
	webhookName = "sidecar-injector.istio.io"
)

var (
	validationWebhookConfigNameTemplateVar = "${namespace}"
	// These should be an invalid DNS-1123 label to ensure the user
	// doesn't specific a valid name that matches out template.
	validationWebhookConfigNameTemplate = "istiod-" + validationWebhookConfigNameTemplateVar
)

type kubeController struct {
	*Controller
	stopCh             chan struct{}
	workloadEntryStore *serviceentry.ServiceEntryStore
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

	serviceController *aggregate.Controller
	serviceEntryStore *serviceentry.ServiceEntryStore
	XDSUpdater        model.XDSUpdater

	m                     sync.Mutex // protects remoteKubeControllers
	remoteKubeControllers map[string]*kubeController
	networksWatcher       mesh.NetworksWatcher
	clusterLocal          model.ClusterLocalProvider

	// fetchCaRoot maps the certificate name to the certificate
	fetchCaRoot  func() map[string]string
	caBundlePath string
	revision     string

	// secretNamespace where we get cluster-access secrets
	secretNamespace  string
	secretController *secretcontroller.Controller
	syncInterval     time.Duration
}

// NewMulticluster initializes data structure to store multicluster information
// It also starts the secret controller
func NewMulticluster(
	serverID string,
	kc kubernetes.Interface,
	secretNamespace string,
	opts Options,
	serviceController *aggregate.Controller,
	serviceEntryStore *serviceentry.ServiceEntryStore,
	caBundlePath string,
	revision string,
	fetchCaRoot func() map[string]string,
	networksWatcher mesh.NetworksWatcher,
	clusterLocal model.ClusterLocalProvider,
	s server.Instance,
) *Multicluster {
	remoteKubeController := make(map[string]*kubeController)
	if opts.ResyncPeriod == 0 {
		// make sure a resync time of 0 wasn't passed in.
		opts.ResyncPeriod = 30 * time.Second
		log.Info("Resync time was configured to 0, resetting to 30")
	}
	mc := &Multicluster{
		serverID:              serverID,
		opts:                  opts,
		serviceController:     serviceController,
		serviceEntryStore:     serviceEntryStore,
		caBundlePath:          caBundlePath,
		revision:              revision,
		fetchCaRoot:           fetchCaRoot,
		XDSUpdater:            opts.XDSUpdater,
		remoteKubeControllers: remoteKubeController,
		networksWatcher:       networksWatcher,
		clusterLocal:          clusterLocal,
		secretNamespace:       secretNamespace,
		syncInterval:          opts.GetSyncInterval(),
		client:                kc,
		s:                     s,
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
	var clusterIDs []string
	for clusterID := range m.remoteKubeControllers {
		clusterIDs = append(clusterIDs, clusterID)
	}
	m.m.Unlock()

	// Remove all of the clusters.
	g, _ := errgroup.WithContext(context.Background())
	for _, clusterID := range clusterIDs {
		g.Go(func() error {
			return m.DeleteMemberCluster(clusterID)
		})
	}
	err = g.Wait()
	return
}

// AddMemberCluster is passed to the secret controller as a callback to be called
// when a remote cluster is added.  This function needs to set up all the handlers
// to watch for resources being added, deleted or changed on remote clusters.
func (m *Multicluster) AddMemberCluster(client kubelib.Client, clusterID string) error {
	m.m.Lock()

	if m.closing {
		m.m.Unlock()
		return fmt.Errorf("failed adding member cluster %s: server shutting down", clusterID)
	}

	// clusterStopCh is a channel that will be closed when this cluster removed.
	clusterStopCh := make(chan struct{})
	options := m.opts
	options.ClusterID = clusterID

	log.Infof("Initializing Kubernetes service registry %q", options.ClusterID)
	kubeRegistry := NewController(client, options)
	m.serviceController.AddRegistry(kubeRegistry)
	m.remoteKubeControllers[clusterID] = &kubeController{
		Controller: kubeRegistry,
		stopCh:     clusterStopCh,
	}
	// localCluster may also be the "config" cluster, in an external-istiod setup.
	localCluster := m.opts.ClusterID == clusterID

	m.m.Unlock()

	// Only need to add service handler for kubernetes registry as `initRegistryEventHandlers`,
	// because when endpoints update `XDSUpdater.EDSUpdate` has already been called.
	kubeRegistry.AppendServiceHandler(func(svc *model.Service, ev model.Event) { m.updateHandler(svc) })

	// TODO move instance cache out of registries
	if m.serviceEntryStore != nil && features.EnableServiceEntrySelectPods {
		// Add an instance handler in the kubernetes registry to notify service entry store about pod events
		kubeRegistry.AppendWorkloadHandler(m.serviceEntryStore.WorkloadInstanceHandler)
	}

	// TODO implement deduping in aggregate registry to allow multiple k8s registries to handle WorkloadEntry
	if features.EnableK8SServiceSelectWorkloadEntries {
		if m.serviceEntryStore != nil && localCluster {
			// Add an instance handler in the service entry store to notify kubernetes about workload entry events
			m.serviceEntryStore.AppendWorkloadHandler(kubeRegistry.WorkloadInstanceHandler)
		} else if features.WorkloadEntryCrossCluster {
			// TODO only do this for non-remotes, can't guarantee CRDs in remotes (depends on https://github.com/istio/istio/pull/29824)
			if configStore, err := createConfigStore(client, m.revision, options); err == nil {
				m.remoteKubeControllers[clusterID].workloadEntryStore = serviceentry.NewServiceDiscovery(
					configStore, model.MakeIstioStore(configStore), options.XDSUpdater, serviceentry.DisableServiceEntryProcessing())
				m.serviceController.AddRegistry(m.remoteKubeControllers[clusterID].workloadEntryStore)
				// Services can select WorkloadEntry from the same cluster. We only duplicate the Service to configure kube-dns.
				m.remoteKubeControllers[clusterID].workloadEntryStore.AppendWorkloadHandler(kubeRegistry.WorkloadInstanceHandler)
				go configStore.Run(clusterStopCh)
			} else {
				log.Errorf("failed creating config configStore for cluster %s: %v", clusterID, err)
			}
		}
	}

	// TODO only create namespace controller and cert patch for remote clusters (no way to tell currently)
	if m.serviceController.Running() {
		// if serviceController isn't running, it will start its members when it is started
		go kubeRegistry.Run(clusterStopCh)
	}
	if m.fetchCaRoot != nil && m.fetchCaRoot() != nil && (features.ExternalIstiod || localCluster) {
		// Block server exit on graceful termination of the leader controller.
		m.s.RunComponentAsyncAndWait(func(_ <-chan struct{}) error {
			log.Infof("joining leader-election for %s in %s on cluster %s",
				leaderelection.NamespaceController, options.SystemNamespace, options.ClusterID)
			leaderelection.
				NewLeaderElection(options.SystemNamespace, m.serverID, leaderelection.NamespaceController, client.Kube()).
				AddRunFunction(func(leaderStop <-chan struct{}) {
					log.Infof("starting namespace controller for cluster %s", clusterID)
					nc := NewNamespaceController(m.fetchCaRoot, client)
					// Start informers again. This fixes the case where informers for namespace do not start,
					// as we create them only after acquiring the leader lock
					// Note: stop here should be the overall pilot stop, NOT the leader election stop. We are
					// basically lazy loading the informer, if we stop it when we lose the lock we will never
					// recreate it again.
					client.RunAndWait(clusterStopCh)
					nc.Run(leaderStop)
				}).Run(clusterStopCh)
			return nil
		})
	}

	// The local cluster has this patching set-up elsewhere. We may eventually want to move it here.
	if features.ExternalIstiod && !localCluster {
		// Patch injection webhook cert
		// This requires RBAC permissions - a low-priv Istiod should not attempt to patch but rely on
		// operator or CI/CD
		if features.InjectionWebhookConfigName.Get() != "" && m.caBundlePath != "" {
			// TODO prevent istiods in primary clusters from trying to patch eachother. should we also leader-elect?
			log.Infof("initializing webhook cert patch for cluster %s", clusterID)
			patcher, err := webhooks.NewWebhookCertPatcher(client.Kube(), m.revision, webhookName, m.caBundlePath)
			if err != nil {
				log.Errorf("could not initialize webhook cert patcher: %v", err)
			} else {
				patcher.Run(clusterStopCh)
			}
		}
		// Patch validation webhook cert
		if m.caBundlePath != "" {
			webhookConfigName := strings.ReplaceAll(validationWebhookConfigNameTemplate, validationWebhookConfigNameTemplateVar, m.secretNamespace)
			validationWebhookController := webhooks.CreateValidationWebhookController(client, webhookConfigName,
				m.secretNamespace, m.caBundlePath)
			if validationWebhookController != nil {
				go validationWebhookController.Start(clusterStopCh)
			}
		}
	}

	// setting up the serviceexport controller if and only if it is turned on in the meshconfig.
	// TODO(nmittler): Need a better solution. Leader election doesn't take into account locality.
	if features.EnableMCSServiceExport {
		log.Infof("joining leader-election for %s in %s on cluster %s",
			leaderelection.ServiceExportController, options.SystemNamespace, options.ClusterID)
		// Block server exit on graceful termination of the leader controller.
		m.s.RunComponentAsyncAndWait(func(_ <-chan struct{}) error {
			leaderelection.
				NewLeaderElection(options.SystemNamespace, m.serverID, leaderelection.ServiceExportController, client.Kube()).
				AddRunFunction(func(leaderStop <-chan struct{}) {
					log.Infof("starting service export controller for cluster %s", clusterID)
					serviceExportController := NewServiceExportController(ServiceExportOptions{
						Client:       client,
						ClusterID:    m.opts.ClusterID,
						DomainSuffix: m.opts.DomainSuffix,
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

	client.RunAndWait(clusterStopCh)
	return nil
}

func (m *Multicluster) UpdateMemberCluster(clients kubelib.Client, clusterID string) error {
	if err := m.DeleteMemberCluster(clusterID); err != nil {
		return err
	}
	return m.AddMemberCluster(clients, clusterID)
}

// DeleteMemberCluster is passed to the secret controller as a callback to be called
// when a remote cluster is deleted.  Also must clear the cache so remote resources
// are removed.
func (m *Multicluster) DeleteMemberCluster(clusterID string) error {
	m.m.Lock()
	defer m.m.Unlock()
	m.serviceController.DeleteRegistry(clusterID, serviceregistry.Kubernetes)
	kc, ok := m.remoteKubeControllers[clusterID]
	if !ok {
		log.Infof("cluster %s does not exist, maybe caused by invalid kubeconfig", clusterID)
		return nil
	}
	if err := kc.Cleanup(); err != nil {
		log.Warnf("failed cleaning up services in %s: %v", clusterID, err)
	}
	if kc.workloadEntryStore != nil {
		m.serviceController.DeleteRegistry(clusterID, serviceregistry.External)
	}
	close(m.remoteKubeControllers[clusterID].stopCh)
	delete(m.remoteKubeControllers, clusterID)
	if m.XDSUpdater != nil {
		m.XDSUpdater.ConfigUpdate(&model.PushRequest{Full: true})
	}

	return nil
}

func createConfigStore(client kubelib.Client, revision string, opts Options) (model.ConfigStoreCache, error) {
	log.Infof("Creating WorkloadEntry only config store for %s", opts.ClusterID)
	workloadEntriesSchemas := collection.NewSchemasBuilder().
		MustAdd(collections.IstioNetworkingV1Alpha3Workloadentries).
		Build()
	return crdclient.NewForSchemas(client, revision, opts.DomainSuffix, workloadEntriesSchemas)
}

func (m *Multicluster) updateHandler(svc *model.Service) {
	if m.XDSUpdater != nil {
		req := &model.PushRequest{
			Full: true,
			ConfigsUpdated: map[model.ConfigKey]struct{}{{
				Kind:      gvk.ServiceEntry,
				Name:      string(svc.Hostname),
				Namespace: svc.Attributes.Namespace,
			}: {}},
			Reason: []model.TriggerReason{model.UnknownTrigger},
		}
		m.XDSUpdater.ConfigUpdate(req)
	}
}

func (m *Multicluster) GetRemoteKubeClient(clusterID string) kubernetes.Interface {
	m.m.Lock()
	defer m.m.Unlock()
	if c := m.remoteKubeControllers[clusterID]; c != nil {
		return c.client
	}
	return nil
}

func (m *Multicluster) InitSecretController(stop <-chan struct{}) {
	m.secretController = secretcontroller.StartSecretController(
		m.client, m.AddMemberCluster, m.UpdateMemberCluster, m.DeleteMemberCluster,
		m.secretNamespace, m.syncInterval, stop)
}

func (m *Multicluster) HasSynced() bool {
	return m.secretController.HasSynced()
}
