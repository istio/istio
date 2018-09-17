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

package clusterregistry

import (
	"time"

	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/kube/secretcontroller"
	"istio.io/istio/pkg/log"
)

type kubeController struct {
	rc     *kube.Controller
	stopCh chan struct{}
}

// Multicluster structure holds the remote kube Controllers and multicluster specific attributes.
type Multicluster struct {
	WatchedNamespace  string
	DomainSuffix      string
	ResyncPeriod      time.Duration
	serviceController *aggregate.Controller
	ClearCache        func()
	rkc               map[string]*kubeController
}

// NewMulticluster initializes data structure to store multicluster information
// It also starts the secret controller
func NewMulticluster(kc kubernetes.Interface, secretNamespace string,
	wns string, ds string, rp time.Duration,
	sc *aggregate.Controller, cc func()) (*Multicluster, error) {

	rkc := make(map[string]*kubeController)
	if rp == 0 {
		// make sure a resync time of 0 wasn't passed in.
		rp = 30 * time.Second
		log.Info("Resync time was configured to 0, resetting to 30")
	}
	mc := &Multicluster{

		WatchedNamespace:  wns,
		DomainSuffix:      ds,
		ResyncPeriod:      rp,
		serviceController: sc,
		ClearCache:        cc,
		rkc:               rkc,
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

	//stopCh to stop controller created here when cluster removed.
	stopCh := make(chan struct{})
	var remoteKubeController kubeController
	remoteKubeController.stopCh = stopCh
	kubectl := kube.NewController(clientset, kube.ControllerOptions{
		WatchedNamespace: m.WatchedNamespace,
		ResyncPeriod:     m.ResyncPeriod,
		DomainSuffix:     m.DomainSuffix,
	})

	remoteKubeController.rc = kubectl
	m.serviceController.AddRegistry(
		aggregate.Registry{
			Name:             serviceregistry.KubernetesRegistry,
			ClusterID:        clusterID,
			ServiceDiscovery: kubectl,
			ServiceAccounts:  kubectl,
			Controller:       kubectl,
		})

	m.rkc[clusterID] = &remoteKubeController
	_ = kubectl.AppendServiceHandler(func(*model.Service, model.Event) { m.ClearCache() })
	_ = kubectl.AppendInstanceHandler(func(*model.ServiceInstance, model.Event) { m.ClearCache() })
	go kubectl.Run(stopCh)

	return nil
}

// DeleteMemberCluster is passed to the secret controller as a callback to be called
// when a remote cluster is deleted.  Also must clear the cache so remote resources
// are removed.
func (m *Multicluster) DeleteMemberCluster(clusterID string) error {

	m.serviceController.DeleteRegistry(clusterID)
	close(m.rkc[clusterID].stopCh)
	delete(m.rkc, clusterID)
	m.ClearCache()

	return nil
}
