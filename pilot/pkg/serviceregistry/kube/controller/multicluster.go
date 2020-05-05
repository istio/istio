// Copyright 2018 Istio Authors
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
	"sync"
	"time"

	"istio.io/pkg/log"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube/secretcontroller"
)

type kubeController struct {
	rc     *Controller
	stopCh chan struct{}
}

// Multicluster structure holds the remote kube Controllers and multicluster specific attributes.
type Multicluster struct {
	WatchedNamespace  string
	DomainSuffix      string
	ResyncPeriod      time.Duration
	serviceController *aggregate.Controller
	XDSUpdater        model.XDSUpdater
	metrics           model.Metrics

	m                     sync.Mutex // protects remoteKubeControllers
	remoteKubeControllers map[string]*kubeController
	networksWatcher       mesh.NetworksWatcher

	// fetchCaRoot maps the certificate name to the certificate
	fetchCaRoot func() map[string]string
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
		WatchedNamespace:      opts.WatchedNamespace,
		DomainSuffix:          opts.DomainSuffix,
		ResyncPeriod:          opts.ResyncPeriod,
		serviceController:     serviceController,
		XDSUpdater:            xds,
		remoteKubeControllers: remoteKubeController,
		networksWatcher:       networksWatcher,
		metrics:               opts.Metrics,
		fetchCaRoot:           opts.FetchCaRoot,
	}

	_ = secretcontroller.StartSecretController(
		kc,
		mc.AddMemberCluster,
		mc.UpdateMemberCluster,
		mc.DeleteMemberCluster,
		secretNamespace)
	return mc, nil
}

// AddMemberCluster is passed to the secret controller as a callback to be called
// when a remote cluster is added.  This function needs to set up all the handlers
// to watch for resources being added, deleted or changed on remote clusters.
func (m *Multicluster) AddMemberCluster(clientset kubernetes.Interface, metadataClient metadata.Interface, clusterID string) error {
	// stopCh to stop controller created here when cluster removed.
	stopCh := make(chan struct{})
	var remoteKubeController kubeController
	remoteKubeController.stopCh = stopCh
	m.m.Lock()
	kubectl := NewController(clientset, metadataClient, Options{
		WatchedNamespace: m.WatchedNamespace,
		ResyncPeriod:     m.ResyncPeriod,
		DomainSuffix:     m.DomainSuffix,
		XDSUpdater:       m.XDSUpdater,
		ClusterID:        clusterID,
		NetworksWatcher:  m.networksWatcher,
		Metrics:          m.metrics,
	})

	remoteKubeController.rc = kubectl
	m.serviceController.AddRegistry(kubectl)

	m.remoteKubeControllers[clusterID] = &remoteKubeController
	m.m.Unlock()

	_ = kubectl.AppendServiceHandler(func(svc *model.Service, ev model.Event) { m.updateHandler(svc) })
	_ = kubectl.AppendInstanceHandler(func(si *model.ServiceInstance, ev model.Event) { m.updateHandler(si.Service) })
	go kubectl.Run(stopCh)
	opts := Options{
		ResyncPeriod: m.ResyncPeriod,
		DomainSuffix: m.DomainSuffix,
	}
	if m.fetchCaRoot != nil {
		nc := NewNamespaceController(m.fetchCaRoot, opts, clientset)
		go nc.Run(stopCh)
	}
	return nil
}

func (m *Multicluster) UpdateMemberCluster(clientset kubernetes.Interface, metadataClient metadata.Interface, clusterID string) error {
	if err := m.DeleteMemberCluster(clusterID); err != nil {
		return err
	}
	return m.AddMemberCluster(clientset, metadataClient, clusterID)
}

// DeleteMemberCluster is passed to the secret controller as a callback to be called
// when a remote cluster is deleted.  Also must clear the cache so remote resources
// are removed.
func (m *Multicluster) DeleteMemberCluster(clusterID string) error {

	m.m.Lock()
	defer m.m.Unlock()
	m.serviceController.DeleteRegistry(clusterID)
	if _, ok := m.remoteKubeControllers[clusterID]; !ok {
		log.Infof("cluster %s does not exist, maybe caused by invalid kubeconfig", clusterID)
		return nil
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
				Kind:      model.ServiceEntryKind,
				Name:      string(svc.Hostname),
				Namespace: svc.Attributes.Namespace,
			}: {}},
			Reason: []model.TriggerReason{model.UnknownTrigger},
		}
		m.XDSUpdater.ConfigUpdate(req)
	}
}

func (m *Multicluster) GetRemoteKubeClients() map[string]kubernetes.Interface {
	m.m.Lock()
	res := make(map[string]kubernetes.Interface)
	for k, v := range m.remoteKubeControllers {
		if v != nil && v.rc != nil {
			res[k] = v.rc.client
		}
	}
	m.m.Unlock()
	return res
}
