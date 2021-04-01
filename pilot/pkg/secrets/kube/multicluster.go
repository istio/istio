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

	"istio.io/istio/pilot/pkg/features"

	"go.uber.org/atomic"

	"istio.io/istio/pilot/pkg/secrets"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/secretcontroller"
	"istio.io/pkg/log"
)

type controller struct {
	sc     *SecretsController
	stopCh chan struct{}
}

// Multicluster structure holds the remote kube Controllers and multicluster specific attributes.
type Multicluster struct {
	mu          sync.Mutex // protects controllers
	controllers map[string]*controller

	client            kube.Client
	secretController  *secretcontroller.Controller
	secretNamespace   string
	localCluster      string
	stop              <-chan struct{}
	remoteSyncTimeout *atomic.Bool
}

var _ secrets.MulticlusterController = &Multicluster{}

func NewMulticluster(client kube.Client, localCluster, secretNamespace string) *Multicluster {
	m := &Multicluster{
		client:            client,
		controllers:       map[string]*controller{},
		localCluster:      localCluster,
		secretNamespace:   secretNamespace,
		remoteSyncTimeout: atomic.NewBool(false),
	}

	return m
}

func (m *Multicluster) addMemberCluster(clients kube.Client, key string) {
	log.Infof("initializing Kubernetes credential reader for cluster %v", key)
	c := &controller{
		sc: NewSecretsController(clients, key),
	}
	m.mu.Lock()
	m.controllers[key] = c
	m.mu.Unlock()
	if m.localCluster != key {
		c.stopCh = make(chan struct{})
		go clients.RunAndWait(c.stopCh)
	} else {
		// RunAndWait in background to avoid blocking queue
		go clients.RunAndWait(m.stop)
	}
}

func (m *Multicluster) Run(stop <-chan struct{}) {
	m.stop = stop
	// Add the local cluster
	m.addMemberCluster(m.client, m.localCluster)
	sc := secretcontroller.StartSecretController(m.client,
		func(c kube.Client, k string) error { m.addMemberCluster(c, k); return nil },
		func(c kube.Client, k string) error { m.updateMemberCluster(c, k); return nil },
		func(k string) error { m.deleteMemberCluster(k); return nil },
		m.secretNamespace,
		time.Millisecond*100,
		stop)
	m.secretController = sc

	time.AfterFunc(features.RemoteClusterTimeout, func() {
		m.remoteSyncTimeout.Store(true)
	})

	<-stop
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, remote := range m.controllers {
		if remote.sc.clusterID != m.localCluster {
			close(remote.stopCh)
		}
	}
}

func (m *Multicluster) HasSynced() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.controllers {
		if c.sc.clusterID != m.localCluster && m.remoteSyncTimeout.Load() {
			continue
		}
		if !c.sc.HasSynced() {
			return false
		}
	}
	return true
}

func (m *Multicluster) updateMemberCluster(clients kube.Client, key string) {
	m.deleteMemberCluster(key)
	m.addMemberCluster(clients, key)
}

func (m *Multicluster) deleteMemberCluster(key string) {
	m.mu.Lock()
	delete(m.controllers, key)
	m.mu.Unlock()
}

func (m *Multicluster) ForCluster(clusterID string) (secrets.Controller, error) {
	if _, f := m.controllers[clusterID]; !f {
		return nil, fmt.Errorf("cluster %v is not configured", clusterID)
	}
	agg := &AggregateController{}
	agg.controllers = []*SecretsController{}

	if clusterID != m.localCluster {
		// If the request cluster is not the local cluster, we will append it and use it for auth
		// This means we will prioritize the proxy cluster, then the local cluster for credential lookup
		// Authorization will always use the proxy cluster.
		agg.controllers = append(agg.controllers, m.controllers[clusterID].sc)
		agg.authController = m.controllers[clusterID].sc
	} else {
		agg.authController = m.controllers[m.localCluster].sc
	}
	agg.controllers = append(agg.controllers, m.controllers[m.localCluster].sc)
	return agg, nil
}

func (m *Multicluster) AddEventHandler(f func(name string, namespace string)) {
	for _, c := range m.controllers {
		c.sc.AddEventHandler(f)
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
