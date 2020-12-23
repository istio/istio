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
	"fmt"
	"sync"
	"time"

	"istio.io/istio/pilot/pkg/secrets"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/secretcontroller"
	"istio.io/pkg/log"
)

// Multicluster structure holds the remote kube Controllers and multicluster specific attributes.
type Multicluster struct {
	remoteKubeControllers map[string]*SecretsController
	m                     sync.Mutex // protects remoteKubeControllers
	secretController      *secretcontroller.Controller
	localCluster          string
	stop                  chan struct{}
}

var _ secrets.MulticlusterController = &Multicluster{}

func NewMulticluster(client kube.Client, localCluster, secretNamespace string, stop chan struct{}) *Multicluster {
	m := &Multicluster{
		remoteKubeControllers: map[string]*SecretsController{},
		localCluster:          localCluster,
		stop:                  stop,
	}
	// Add the local cluster
	m.addMemberCluster(client, localCluster)
	sc := secretcontroller.StartSecretController(client,
		func(c kube.Client, k string) error { m.addMemberCluster(c, k); return nil },
		func(c kube.Client, k string) error { m.updateMemberCluster(c, k); return nil },
		func(k string) error { m.deleteMemberCluster(k); return nil },
		secretNamespace,
		time.Millisecond*100,
		stop)
	m.secretController = sc
	return m
}

func (m *Multicluster) addMemberCluster(clients kube.Client, key string) {
	log.Infof("initializing Kubernetes credential reader for cluster %v", key)
	sc := NewSecretsController(clients, key)
	m.m.Lock()
	m.remoteKubeControllers[key] = sc
	m.m.Unlock()
	clients.RunAndWait(m.stop)
}

func (m *Multicluster) updateMemberCluster(clients kube.Client, key string) {
	m.deleteMemberCluster(key)
	m.addMemberCluster(clients, key)
}

func (m *Multicluster) deleteMemberCluster(key string) {
	m.m.Lock()
	delete(m.remoteKubeControllers, key)
	m.m.Unlock()
}

func (m *Multicluster) ForCluster(clusterID string) (secrets.Controller, error) {
	if _, f := m.remoteKubeControllers[clusterID]; !f {
		return nil, fmt.Errorf("cluster %v is not configured", clusterID)
	}
	agg := &AggregateController{}
	agg.controllers = []*SecretsController{}

	if clusterID != m.localCluster {
		// If the request cluster is not the local cluster, we will append it and use it for auth
		// This means we will prioritize the proxy cluster, then the local cluster for credential lookup
		// Authorization will always use the proxy cluster.
		agg.controllers = append(agg.controllers, m.remoteKubeControllers[clusterID])
		agg.authController = m.remoteKubeControllers[clusterID]
	} else {
		agg.authController = m.remoteKubeControllers[m.localCluster]
	}
	agg.controllers = append(agg.controllers, m.remoteKubeControllers[m.localCluster])
	return agg, nil
}

func (m *Multicluster) AddEventHandler(f func(name string, namespace string)) {
	for _, c := range m.remoteKubeControllers {
		c.AddEventHandler(f)
	}
}

type AggregateController struct {
	// controllers to use to look up certs. Generally this will consistent of the local (config) cluster
	// and a single remote cluster where the proxy resides
	controllers    []*SecretsController
	authController *SecretsController
}

var _ secrets.Controller = &AggregateController{}

func (a *AggregateController) GetKeyAndCert(name, namespace string) (key []byte, cert []byte) {
	// Search through all clusters, find first non-empty result
	for _, c := range a.controllers {
		k, c := c.GetKeyAndCert(name, namespace)
		if k != nil && c != nil {
			return k, c
		}
	}
	return nil, nil
}

func (a *AggregateController) GetCaCert(name, namespace string) (cert []byte) {
	// Search through all clusters, find first non-empty result
	for _, c := range a.controllers {
		k := c.GetCaCert(name, namespace)
		if k != nil {
			return k
		}
	}
	return nil
}

func (a *AggregateController) Authorize(serviceAccount, namespace string) error {
	return a.authController.Authorize(serviceAccount, namespace)
}

func (a *AggregateController) AddEventHandler(f func(name string, namespace string)) {
	for _, c := range a.controllers {
		c.AddEventHandler(f)
	}
}
