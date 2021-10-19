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

	"istio.io/istio/pilot/pkg/secrets"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/secretcontroller"
	"istio.io/pkg/log"
	"istio.io/pkg/monitoring"
)

// Multicluster structure holds the remote kube Controllers and multicluster specific attributes.
type Multicluster struct {
	remoteKubeControllers map[cluster.ID]*SecretsController
	m                     sync.Mutex // protects remoteKubeControllers
	localCluster          cluster.ID
}

var _ secrets.MulticlusterController = &Multicluster{}

var (
	clusterType = monitoring.MustCreateLabel("cluster_type")

	clustersCount = monitoring.NewGauge(
		"istiod_managed_clusters",
		"Number of clusters managed by istiod",
		monitoring.WithLabels(clusterType),
	)

	localClusters  = clustersCount.With(clusterType.Value("local"))
	remoteClusters = clustersCount.With(clusterType.Value("remote"))
)

func init() {
	monitoring.MustRegister(clustersCount)
}

func NewMulticluster(client kube.Client, localCluster cluster.ID) *Multicluster {
	m := &Multicluster{
		remoteKubeControllers: map[cluster.ID]*SecretsController{},
		localCluster:          localCluster,
	}
	// init gauges
	localClusters.Record(1.0)
	remoteClusters.Record(0.0)

	// Add the local cluster
	if err := m.AddCluster(localCluster, &secretcontroller.Cluster{Client: client}); err != nil {
		log.Errorf("failed initializing secretcontroller for %s: %v", localCluster, err)
	}

	return m
}

func (m *Multicluster) AddCluster(key cluster.ID, cluster *secretcontroller.Cluster) error {
	log.Infof("initializing Kubernetes credential reader for cluster %v", key)
	sc := NewSecretsController(cluster.Client, key)
	m.m.Lock()
	m.remoteKubeControllers[key] = sc
	remoteClusters.Record(float64(len(m.remoteKubeControllers) - 1))
	m.m.Unlock()
	return nil
}

func (m *Multicluster) UpdateCluster(key cluster.ID, cluster *secretcontroller.Cluster) error {
	if err := m.RemoveCluster(key); err != nil {
		return err
	}
	if err := m.AddCluster(key, cluster); err != nil {
		return err
	}
	return nil
}

func (m *Multicluster) RemoveCluster(key cluster.ID) error {
	m.m.Lock()
	delete(m.remoteKubeControllers, key)
	remoteClusters.Record(float64(len(m.remoteKubeControllers) - 1))
	m.m.Unlock()
	return nil
}

func (m *Multicluster) ForCluster(clusterID cluster.ID) (secrets.Controller, error) {
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
	for _, c := range a.controllers {
		c.AddEventHandler(f)
	}
}
