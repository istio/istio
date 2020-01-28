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

	"k8s.io/client-go/kubernetes"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/resource"
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

	m                     sync.Mutex // protects remoteKubeControllers
	remoteKubeControllers map[string]*kubeController
	networksWatcher       mesh.NetworksWatcher
}

// NewMulticluster initializes data structure to store multicluster information
// It also starts the secret controller
func NewMulticluster(kc kubernetes.Interface, secretNamespace string,
	watchedNamespace string, domainSuffix string, resyncPeriod time.Duration,
	serviceController *aggregate.Controller, xds model.XDSUpdater, networksWatcher mesh.NetworksWatcher) (*Multicluster, error) {

	remoteKubeController := make(map[string]*kubeController)
	if resyncPeriod == 0 {
		// make sure a resync time of 0 wasn't passed in.
		resyncPeriod = 30 * time.Second
		log.Info("Resync time was configured to 0, resetting to 30")
	}
	mc := &Multicluster{
		WatchedNamespace:      watchedNamespace,
		DomainSuffix:          domainSuffix,
		ResyncPeriod:          resyncPeriod,
		serviceController:     serviceController,
		XDSUpdater:            xds,
		remoteKubeControllers: remoteKubeController,
		networksWatcher:       networksWatcher,
	}

	err := secretcontroller.StartSecretController(kc,
		mc.AddMemberCluster,
		mc.DeleteMemberCluster,
		secretNamespace)
	return mc, err
}

// AddMemberCluster is passed to the secret controller as a callback to be called
// when a remote cluster is added.  This function needs to set up all the handlers
// to watch for resources being added, deleted or changed on remote clusters.
func (m *Multicluster) AddMemberCluster(clientset kubernetes.Interface, clusterID string) error {
	// stopCh to stop controller created here when cluster removed.
	stopCh := make(chan struct{})
	var remoteKubeController kubeController
	remoteKubeController.stopCh = stopCh
	m.m.Lock()
	kubectl := NewController(clientset, Options{
		WatchedNamespace: m.WatchedNamespace,
		ResyncPeriod:     m.ResyncPeriod,
		DomainSuffix:     m.DomainSuffix,
		XDSUpdater:       m.XDSUpdater,
		ClusterID:        clusterID,
		NetworksWatcher:  m.networksWatcher,
	})

	remoteKubeController.rc = kubectl
	m.serviceController.AddRegistry(kubectl)

	m.remoteKubeControllers[clusterID] = &remoteKubeController
	m.m.Unlock()

	_ = kubectl.AppendServiceHandler(func(*model.Service, model.Event) { m.updateHandler() })
	_ = kubectl.AppendInstanceHandler(func(*model.ServiceInstance, model.Event) { m.updateHandler() })
	go kubectl.Run(stopCh)
	return nil
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

func (m *Multicluster) updateHandler() {
	if m.XDSUpdater != nil {
		req := &model.PushRequest{
			Full:               true,
			ConfigTypesUpdated: map[resource.GroupVersionKind]struct{}{collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind(): {}},
			Reason:             []model.TriggerReason{model.UnknownTrigger},
		}
		m.XDSUpdater.ConfigUpdate(req)
	}
}
