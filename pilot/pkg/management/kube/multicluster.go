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

package kube

import (
	"context"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/annotation"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/keycertbundle"
	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pilot/pkg/server"
	"istio.io/istio/pkg/cluster"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/webhooks"
)

const (
	// Name of the webhook config in the config - no need to change it.
	webhookName = "sidecar-injector.istio.io"
)

// Options stores the configurable attributes of a Controller.
type Options struct {
	SystemNamespace string

	// ClusterID identifies the remote cluster in a multicluster env.
	ClusterID cluster.ID
}

type MultiCluster struct {
	// serverID of this pilot instance used for leader election
	serverID string

	// options to use when creating kube controllers
	opts Options

	s server.Instance

	startNsController bool
	caBundleWatcher   *keycertbundle.Watcher
	revision          string
}

func NewMulticluster(
	serverID string,
	opts Options,
	caBundleWatcher *keycertbundle.Watcher,
	revision string,
	startNsController bool,
	s server.Instance) *MultiCluster {
	return &MultiCluster{
		serverID:          serverID,
		opts:              opts,
		startNsController: startNsController,
		caBundleWatcher:   caBundleWatcher,
		revision:          revision,
		s:                 s,
	}
}

func (m *MultiCluster) ClusterAdded(cluster *multicluster.Cluster, clusterStopCh <-chan struct{}) error {
	client := cluster.Client
	options := m.opts
	configCluster := m.opts.ClusterID == cluster.ID

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

	return nil
}

func (m *MultiCluster) ClusterUpdated(cluster *multicluster.Cluster, clusterStopCh <-chan struct{}) error {
	if err := m.ClusterDeleted(cluster.ID); err != nil {
		return err
	}
	return m.ClusterAdded(cluster, clusterStopCh)
}

func (m *MultiCluster) ClusterDeleted(clusterID cluster.ID) error {
	// do nothing because of the clusterStopCh closed by caller
	return nil
}

// checkShouldLead returns true if the caller should attempt leader election for a remote cluster.
func (m *MultiCluster) checkShouldLead(client kubelib.Client, systemNamespace string) bool {
	if features.ExternalIstiod {
		namespace, err := client.Kube().CoreV1().Namespaces().Get(context.TODO(), systemNamespace, metav1.GetOptions{})
		if err == nil {
			// found same system namespace on the remote cluster so check if we are a selected istiod to lead
			istiodCluster, found := namespace.Annotations[annotation.TopologyControlPlaneClusters.Name]
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
