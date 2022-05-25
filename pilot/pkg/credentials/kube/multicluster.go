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

	"istio.io/istio/pilot/pkg/credentials"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/pkg/log"
)

type secretHandler func(name string, namespace string)

// Multicluster structure holds the remote kube Controllers and multicluster specific attributes.
type Multicluster struct {
	remoteKubeControllers map[cluster.ID]*CredentialsController
	m                     sync.Mutex // protects remoteKubeControllers
	configCluster         cluster.ID
	secretHandlers        []secretHandler
}

var _ credentials.MulticlusterController = &Multicluster{}

func NewMulticluster(configCluster cluster.ID) *Multicluster {
	m := &Multicluster{
		remoteKubeControllers: map[cluster.ID]*CredentialsController{},
		configCluster:         configCluster,
	}

	return m
}

func (m *Multicluster) ClusterAdded(cluster *multicluster.Cluster, _ <-chan struct{}) error {
	log.Infof("initializing Kubernetes credential reader for cluster %v", cluster.ID)
	sc := NewCredentialsController(cluster.Client, cluster.ID)
	m.m.Lock()
	m.remoteKubeControllers[cluster.ID] = sc
	for _, onCredential := range m.secretHandlers {
		sc.AddEventHandler(onCredential)
	}
	m.m.Unlock()
	return nil
}

func (m *Multicluster) ClusterUpdated(cluster *multicluster.Cluster, stop <-chan struct{}) error {
	if err := m.ClusterDeleted(cluster.ID); err != nil {
		return err
	}
	if err := m.ClusterAdded(cluster, stop); err != nil {
		return err
	}
	return nil
}

func (m *Multicluster) ClusterDeleted(key cluster.ID) error {
	m.m.Lock()
	delete(m.remoteKubeControllers, key)
	m.m.Unlock()
	return nil
}

func (m *Multicluster) ForCluster(clusterID cluster.ID) (credentials.Controller, error) {
	if _, f := m.remoteKubeControllers[clusterID]; !f {
		return nil, fmt.Errorf("cluster %v is not configured", clusterID)
	}
	agg := &AggregateController{}
	agg.controllers = []*CredentialsController{}
	agg.authController = m.remoteKubeControllers[clusterID]
	if clusterID != m.configCluster {
		// If the request cluster is not the config cluster, we will append it and use it for auth
		// This means we will prioritize the proxy cluster, then the config cluster for credential lookup
		// Authorization will always use the proxy cluster.
		agg.controllers = append(agg.controllers, m.remoteKubeControllers[clusterID])
	}
	agg.controllers = append(agg.controllers, m.remoteKubeControllers[m.configCluster])
	return agg, nil
}

func (m *Multicluster) AddSecretHandler(h secretHandler) {
	m.secretHandlers = append(m.secretHandlers, h)
	for _, c := range m.remoteKubeControllers {
		c.AddEventHandler(h)
	}
}

type AggregateController struct {
	// controllers to use to look up certs. Generally this will consistent of the primary (config) cluster
	// and a single remote cluster where the proxy resides
	controllers    []*CredentialsController
	authController *CredentialsController
}

var _ credentials.Controller = &AggregateController{}

func (a *AggregateController) GetKeyAndCert(name, namespace string) (key []byte, cert []byte, err error) {
	// Search through all clusters, find first non-empty result
	var firstError error
	for _, c := range a.controllers {
		k, c, err := c.GetKeyAndCert(name, namespace)
		if err != nil {
			if firstError == nil {
				firstError = err
			}
		} else {
			return k, c, nil
		}
	}
	return nil, nil, firstError
}

func (a *AggregateController) GetCaCert(name, namespace string) (cert []byte, err error) {
	// Search through all clusters, find first non-empty result
	var firstError error
	for _, c := range a.controllers {
		k, err := c.GetCaCert(name, namespace)
		if err != nil {
			if firstError == nil {
				firstError = err
			}
		} else {
			return k, nil
		}
	}
	return nil, firstError
}

func (a *AggregateController) Authorize(serviceAccount, namespace string) error {
	return a.authController.Authorize(serviceAccount, namespace)
}

func (a *AggregateController) AddEventHandler(f func(name string, namespace string)) {
	// no ops
}

func (a *AggregateController) GetDockerCredential(name, namespace string) ([]byte, error) {
	// Search through all clusters, find first non-empty result
	var firstError error
	for _, c := range a.controllers {
		k, err := c.GetDockerCredential(name, namespace)
		if err != nil {
			if firstError == nil {
				firstError = err
			}
		} else {
			return k, nil
		}
	}
	return nil, firstError
}
