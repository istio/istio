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
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/annotation"
	"istio.io/istio/pilot/pkg/config/kube/clustertrustbundle"
	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/server"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pkg/backoff"
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

type kubeController struct {
	MeshServiceController *aggregate.Controller
	*Controller
	workloadEntryController *serviceentry.Controller
	stop                    chan struct{}
}

func (k *kubeController) Close() {
	close(k.stop)
	clusterID := k.Controller.clusterID
	k.MeshServiceController.UnRegisterHandlersForCluster(clusterID)
	k.MeshServiceController.DeleteRegistry(clusterID, provider.Kubernetes)
	if k.workloadEntryController != nil {
		k.MeshServiceController.DeleteRegistry(clusterID, provider.External)
	}
	if err := k.Controller.Cleanup(); err != nil {
		log.Warnf("failed cleaning up services in %s: %v", clusterID, err)
	}
	if k.opts.XDSUpdater != nil {
		k.opts.XDSUpdater.ConfigUpdate(&model.PushRequest{Full: true, Reason: model.NewReasonStats(model.ClusterUpdate), Forced: true})
	}
	k.Controller.shutdownInformerHandlers()
}

// Multicluster structure holds the remote kube Controllers and multicluster specific attributes.
type Multicluster struct {
	// serverID of this pilot instance used for leader election
	serverID string

	// options to use when creating kube controllers
	opts Options

	s server.Instance

	serviceEntryController *serviceentry.Controller

	clusterLocal model.ClusterLocalProvider

	distributeCACert bool
	caBundleWatcher  *keycertbundle.Watcher
	revision         string

	component *multicluster.Component[*kubeController]
}

// NewMulticluster initializes data structure to store multicluster information
func NewMulticluster(
	serverID string,
	opts Options,
	serviceEntryController *serviceentry.Controller,
	caBundleWatcher *keycertbundle.Watcher,
	revision string,
	distributeCACert bool,
	clusterLocal model.ClusterLocalProvider,
	s server.Instance,
	controller *multicluster.Controller,
) *Multicluster {
	mc := &Multicluster{
		serverID:               serverID,
		opts:                   opts,
		serviceEntryController: serviceEntryController,
		distributeCACert:       distributeCACert,
		caBundleWatcher:        caBundleWatcher,
		revision:               revision,
		clusterLocal:           clusterLocal,
		s:                      s,
	}
	mc.component = multicluster.BuildMultiClusterComponent(controller, func(cluster *multicluster.Cluster) *kubeController {
		stop := make(chan struct{})
		client := cluster.Client
		configCluster := opts.ClusterID == cluster.ID

		options := opts
		options.ClusterID = cluster.ID
		if !configCluster {
			options.SyncTimeout = features.RemoteClusterTimeout
		}
		log.Infof("Initializing Kubernetes service registry %q", options.ClusterID)
		options.ConfigCluster = configCluster
		kubeRegistry := NewController(client, options)
		kubeController := &kubeController{
			MeshServiceController: opts.MeshServiceController,
			Controller:            kubeRegistry,
			stop:                  stop,
		}
		mc.initializeCluster(cluster, kubeController, kubeRegistry, options, configCluster, stop)
		return kubeController
	})

	return mc
}

// initializeCluster initializes the cluster by setting various handlers.
func (m *Multicluster) initializeCluster(cluster *multicluster.Cluster, kubeController *kubeController, kubeRegistry *Controller,
	options Options, configCluster bool, clusterStopCh <-chan struct{},
) {
	client := cluster.Client

	if m.serviceEntryController != nil && features.EnableServiceEntrySelectPods {
		// Add an instance handler in the kubernetes registry to notify service entry store about pod events
		kubeRegistry.AppendWorkloadHandler(m.serviceEntryController.WorkloadInstanceHandler)
	}

	if configCluster && m.serviceEntryController != nil {
		kubeRegistry.AppendNamespaceDiscoveryHandlers(m.serviceEntryController.NamespaceDiscoveryHandler)
	}

	// TODO implement deduping in aggregate registry to allow multiple k8s registries to handle WorkloadEntry
	if features.EnableK8SServiceSelectWorkloadEntries {
		if m.serviceEntryController != nil && configCluster {
			// Add an instance handler in the service entry store to notify kubernetes about workload entry events
			m.serviceEntryController.AppendWorkloadHandler(kubeRegistry.WorkloadInstanceHandler)
		} else if features.WorkloadEntryCrossCluster {
			// TODO only do this for non-remotes, can't guarantee CRDs in remotes (depends on https://github.com/istio/istio/pull/29824)
			configStore := createWleConfigStore(client, m.revision, options)
			kubeController.workloadEntryController = serviceentry.NewWorkloadEntryController(
				configStore, options.XDSUpdater,
				m.opts.MeshWatcher,
				serviceentry.WithClusterID(cluster.ID),
				serviceentry.WithNetworkIDCb(kubeRegistry.Network))
			// Services can select WorkloadEntry from the same cluster. We only duplicate the Service to configure kube-dns.
			kubeController.workloadEntryController.AppendWorkloadHandler(kubeRegistry.WorkloadInstanceHandler)
			// ServiceEntry selects WorkloadEntry from remote cluster
			kubeController.workloadEntryController.AppendWorkloadHandler(m.serviceEntryController.WorkloadInstanceHandler)
			kubeRegistry.AppendNamespaceDiscoveryHandlers(kubeController.workloadEntryController.NamespaceDiscoveryHandler)
			m.opts.MeshServiceController.AddRegistryAndRun(kubeController.workloadEntryController, clusterStopCh)
			go configStore.Run(clusterStopCh)
		}
	}

	// run after WorkloadHandler is added
	m.opts.MeshServiceController.AddRegistryAndRun(kubeRegistry, clusterStopCh)

	go func() {
		var shouldLead bool
		if !configCluster {
			shouldLead = m.checkShouldLead(client, options.SystemNamespace, clusterStopCh)
			log.Infof("should join leader-election for cluster %s: %t", cluster.ID, shouldLead)
		}

		if m.distributeCACert && (shouldLead || configCluster) {
			if features.EnableClusterTrustBundles {
				// Block server exit on graceful termination of the leader controller.
				m.s.RunComponentAsyncAndWait("clustertrustbundle controller", func(_ <-chan struct{}) error {
					log.Infof("joining leader-election for %s in %s on cluster %s",
						leaderelection.ClusterTrustBundleController, options.SystemNamespace, options.ClusterID)
					election := leaderelection.
						NewLeaderElectionMulticluster(options.SystemNamespace, m.serverID, leaderelection.NamespaceController, m.revision, !configCluster, client).
						AddRunFunction(func(leaderStop <-chan struct{}) {
							log.Infof("starting clustertrustbundle controller for cluster %s", cluster.ID)
							c := clustertrustbundle.NewController(client, m.caBundleWatcher)
							client.RunAndWait(clusterStopCh)
							c.Run(leaderStop)
						})
					election.Run(clusterStopCh)
					return nil
				})
			} else {
				// Block server exit on graceful termination of the leader controller.
				m.s.RunComponentAsyncAndWait("namespace controller", func(_ <-chan struct{}) error {
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

func createWleConfigStore(client kubelib.Client, revision string, opts Options) model.ConfigStoreController {
	log.Infof("Creating WorkloadEntry only config store for %s", opts.ClusterID)
	workloadEntriesSchemas := collection.NewSchemasBuilder().
		MustAdd(collections.WorkloadEntry).
		Build()
	crdOpts := crdclient.Option{Revision: revision, DomainSuffix: opts.DomainSuffix, Identifier: "mc-workload-entry-controller"}
	return crdclient.NewForSchemas(client, crdOpts, workloadEntriesSchemas)
}
