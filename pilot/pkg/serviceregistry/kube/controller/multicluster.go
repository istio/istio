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

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/features"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/webhooks"

	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/secretcontroller"
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
	WatchedNamespaces string
	DomainSuffix      string
	ResyncPeriod      time.Duration
	serviceController *aggregate.Controller
	XDSUpdater        model.XDSUpdater
	metrics           model.Metrics

	m                     sync.Mutex // protects remoteKubeControllers
	remoteKubeControllers map[string]*kubeController
	networksWatcher       mesh.NetworksWatcher

	// fetchCaRoot maps the certificate name to the certificate
	fetchCaRoot      func() map[string]string
	caBundlePath     string
	secretNamespace  string
	secretController *secretcontroller.Controller
}

// NewMulticluster initializes data structure to store multicluster information
// It also starts the secret controller
func NewMulticluster(kc kubernetes.Interface, secretNamespace string, opts Options,
	serviceController *aggregate.Controller, xds model.XDSUpdater, networksWatcher mesh.NetworksWatcher) (*Multicluster, error) {

	remoteKubeController := make(map[string]*kubeController)
	if opts.ResyncPeriod == 0 {
		// make sure a resync time of 0 wasn't passed in.
		opts.ResyncPeriod = 30 * time.Second
		log.Info("Resync time was configured to 0, resetting to 30")
	}
	mc := &Multicluster{
		WatchedNamespaces:     opts.WatchedNamespaces,
		DomainSuffix:          opts.DomainSuffix,
		ResyncPeriod:          opts.ResyncPeriod,
		serviceController:     serviceController,
		XDSUpdater:            xds,
		remoteKubeControllers: remoteKubeController,
		networksWatcher:       networksWatcher,
		metrics:               opts.Metrics,
		fetchCaRoot:           opts.FetchCaRoot,
		caBundlePath:          opts.CABundlePath,
		secretNamespace:       secretNamespace,
	}
	mc.initSecretController(kc)

	return mc, nil
}

// AddMemberCluster is passed to the secret controller as a callback to be called
// when a remote cluster is added.  This function needs to set up all the handlers
// to watch for resources being added, deleted or changed on remote clusters.
func (m *Multicluster) AddMemberCluster(clients kubelib.Client, clusterID string) error {
	// stopCh to stop controller created here when cluster removed.
	stopCh := make(chan struct{})
	var remoteKubeController kubeController
	remoteKubeController.stopCh = stopCh
	m.m.Lock()
	options := Options{
		WatchedNamespaces: m.WatchedNamespaces,
		ResyncPeriod:      m.ResyncPeriod,
		DomainSuffix:      m.DomainSuffix,
		XDSUpdater:        m.XDSUpdater,
		ClusterID:         clusterID,
		NetworksWatcher:   m.networksWatcher,
		Metrics:           m.metrics,
	}
	kubectl := NewController(clients, options)

	remoteKubeController.Controller = kubectl
	m.serviceController.AddRegistry(kubectl)

	m.remoteKubeControllers[clusterID] = &remoteKubeController
	m.m.Unlock()

	// Only need to add service handler for kubernetes registry as `initRegistryEventHandlers`,
	// because when endpoints update `XDSUpdater.EDSUpdate` has already been called.
	_ = kubectl.AppendServiceHandler(func(svc *model.Service, ev model.Event) { m.updateHandler(svc) })

	go kubectl.Run(stopCh)
	webhookConfigName := strings.ReplaceAll(validationWebhookConfigNameTemplate, validationWebhookConfigNameTemplateVar, m.secretNamespace)
	if m.fetchCaRoot != nil {
		nc := NewNamespaceController(m.fetchCaRoot, clients)
		go nc.Run(stopCh)
		go webhooks.PatchCertLoop(features.InjectionWebhookConfigName.Get(), webhookName, m.caBundlePath, clients.Kube(), stopCh)
		valicationWebhookController := webhooks.CreateValidationWebhookController(clients, webhookConfigName,
			m.secretNamespace, m.caBundlePath, true)
		if valicationWebhookController != nil {
			go valicationWebhookController.Start(stopCh)
		}
	}

	clients.RunAndWait(stopCh)
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

func (m *Multicluster) initSecretController(kc kubernetes.Interface) {
	m.secretController = secretcontroller.StartSecretController(kc,
		m.AddMemberCluster,
		m.UpdateMemberCluster,
		m.DeleteMemberCluster,
		m.secretNamespace)
}

func (m *Multicluster) HasSynced() bool {
	return m.secretController.HasSynced()
}
