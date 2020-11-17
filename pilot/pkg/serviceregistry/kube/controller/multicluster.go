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
	"strings"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/serviceentry"
	"istio.io/istio/pkg/config/mesh"
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
	stopCh chan struct{}
}

// Multicluster structure holds the remote kube Controllers and multicluster specific attributes.
type Multicluster struct {
	opts Options

	client kubernetes.Interface

	serviceController *aggregate.Controller
	serviceEntryStore *serviceentry.ServiceEntryStore
	XDSUpdater        model.XDSUpdater

	m                     sync.Mutex // protects remoteKubeControllers
	remoteKubeControllers map[string]*kubeController
	networksWatcher       mesh.NetworksWatcher

	// fetchCaRoot maps the certificate name to the certificate
	fetchCaRoot  func() map[string]string
	caBundlePath string

	// secretNamespace where we get cluster-access secrets
	secretNamespace  string
	secretController *secretcontroller.Controller
	syncInterval     time.Duration
}

// NewMulticluster initializes data structure to store multicluster information
// It also starts the secret controller
func NewMulticluster(
	kc kubernetes.Interface,
	secretNamespace string,
	opts Options,
	serviceController *aggregate.Controller,
	serviceEntryStore *serviceentry.ServiceEntryStore,
	caBundlePath string,
	fetchCaRoot func() map[string]string,
	networksWatcher mesh.NetworksWatcher,
) *Multicluster {
	remoteKubeController := make(map[string]*kubeController)
	if opts.ResyncPeriod == 0 {
		// make sure a resync time of 0 wasn't passed in.
		opts.ResyncPeriod = 30 * time.Second
		log.Info("Resync time was configured to 0, resetting to 30")
	}
	mc := &Multicluster{
		opts:                  opts,
		serviceController:     serviceController,
		serviceEntryStore:     serviceEntryStore,
		caBundlePath:          caBundlePath,
		fetchCaRoot:           fetchCaRoot,
		XDSUpdater:            opts.XDSUpdater,
		remoteKubeControllers: remoteKubeController,
		networksWatcher:       networksWatcher,
		secretNamespace:       secretNamespace,
		syncInterval:          opts.GetSyncInterval(),
		client:                kc,
	}

	return mc
}

// AddMemberCluster is passed to the secret controller as a callback to be called
// when a remote cluster is added.  This function needs to set up all the handlers
// to watch for resources being added, deleted or changed on remote clusters.
func (m *Multicluster) AddMemberCluster(client kubelib.Client, clusterID string) error {
	// stopCh to stop controller created here when cluster removed.
	stopCh := make(chan struct{})
	var remoteKubeController kubeController
	remoteKubeController.stopCh = stopCh
	m.m.Lock()
	options := m.opts
	options.ClusterID = clusterID
	log.Infof("Initializing Kubernetes service registry %q", options.ClusterID)
	kubeRegistry := NewController(client, options)

	remoteKubeController.Controller = kubeRegistry
	m.serviceController.AddRegistry(kubeRegistry)

	m.remoteKubeControllers[clusterID] = &remoteKubeController
	m.m.Unlock()

	// Only need to add service handler for kubernetes registry as `initRegistryEventHandlers`,
	// because when endpoints update `XDSUpdater.EDSUpdate` has already been called.
	kubeRegistry.AppendServiceHandler(func(svc *model.Service, ev model.Event) { m.updateHandler(svc) })

	// TODO move instance cache out of registries
	if m.serviceEntryStore != nil && features.EnableServiceEntrySelectPods {
		// Add an instance handler in the kubernetes registry to notify service entry store about pod events
		kubeRegistry.AppendWorkloadHandler(m.serviceEntryStore.WorkloadInstanceHandler)
	}

	if m.serviceEntryStore != nil && features.EnableK8SServiceSelectWorkloadEntries {
		// Add an instance handler in the service entry store to notify kubernetes about workload entry events
		m.serviceEntryStore.AppendWorkloadHandler(kubeRegistry.WorkloadInstanceHandler)
	}

	// TODO only create namespace controller and cert patch for remote clusters (no way to tell currently)
	go kubeRegistry.Run(stopCh)
	if m.fetchCaRoot != nil {
		log.Infof("initializing namespace controller for cluster %s", clusterID)
		// TODO remove initNamespaceController (and probably need leader election here? how will that work with multi-primary?)
		nc := NewNamespaceController(m.fetchCaRoot, client)
		go nc.Run(stopCh)
	}

	webhookConfigName := strings.ReplaceAll(validationWebhookConfigNameTemplate, validationWebhookConfigNameTemplateVar, m.secretNamespace)
	if m.caBundlePath != "" {
		// TODO remove the patch loop init from initSidecarInjector (does this need leader elect? how well does it work with multi-primary?)
		log.Infof("initializing webhook cert patch for cluster %s", clusterID)
		go webhooks.PatchCertLoop(features.InjectionWebhookConfigName.Get(), webhookName, m.caBundlePath, client.Kube(), stopCh)
		validationWebhookController := webhooks.CreateValidationWebhookController(client, webhookConfigName,
			m.secretNamespace, m.caBundlePath, true)
		if validationWebhookController != nil {
			go validationWebhookController.Start(stopCh)
		}
	}

	client.RunAndWait(stopCh)
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
	m.serviceController.DeleteRegistry(clusterID)
	kc, ok := m.remoteKubeControllers[clusterID]
	if !ok {
		log.Infof("cluster %s does not exist, maybe caused by invalid kubeconfig", clusterID)
		return nil
	}
	if err := kc.Cleanup(); err != nil {
		log.Warnf("failed cleaning up services in %s: %v", clusterID, err)
	}
	close(m.remoteKubeControllers[clusterID].stopCh)
	delete(m.remoteKubeControllers, clusterID)
	if m.XDSUpdater != nil {
		m.XDSUpdater.ConfigUpdate(&model.PushRequest{Full: true})
	}

	return nil
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

func (m *Multicluster) InitSecretController() {
	m.secretController = secretcontroller.StartSecretController(m.client,
		m.AddMemberCluster,
		m.UpdateMemberCluster,
		m.DeleteMemberCluster,
		m.secretNamespace,
		m.syncInterval)
}

func (m *Multicluster) HasSynced() bool {
	return m.secretController.HasSynced()
}
