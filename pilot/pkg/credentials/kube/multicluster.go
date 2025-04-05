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

	"istio.io/istio/pilot/pkg/credentials"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/multicluster"
)

// Multicluster structure holds the remote kube Controllers and multicluster specific attributes.
type Multicluster struct {
	configCluster  cluster.ID
	secretHandlers []func(k kind.Kind, name string, namespace string)
	component      *multicluster.Component[*CredentialsController]
}

var _ credentials.MulticlusterController = &Multicluster{}

func NewMulticluster(configCluster cluster.ID, controller multicluster.ComponentBuilder) *Multicluster {
	m := &Multicluster{
		configCluster: configCluster,
	}

	m.component = multicluster.BuildMultiClusterComponent(controller, func(cluster *multicluster.Cluster) *CredentialsController {
		return NewCredentialsController(cluster.Client, m.secretHandlers)
	})
	return m
}

func (m *Multicluster) ForCluster(clusterID cluster.ID) (credentials.Controller, error) {
	cc := m.component.ForCluster(clusterID)
	if cc == nil {
		return nil, fmt.Errorf("cluster %v is not configured", clusterID)
	}
	agg := &AggregateController{}
	agg.controllers = []*CredentialsController{}
	agg.authController = *cc
	if clusterID != m.configCluster {
		// If the request cluster is not the config cluster, we will append it and use it for auth
		// This means we will prioritize the proxy cluster, then the config cluster for credential lookup
		// Authorization will always use the proxy cluster.
		agg.controllers = append(agg.controllers, *cc)
	}
	if cc := m.component.ForCluster(m.configCluster); cc != nil {
		agg.controllers = append(agg.controllers, *cc)
	}
	return agg, nil
}

func (m *Multicluster) AddSecretHandler(h func(k kind.Kind, name string, namespace string)) {
	// Intentionally no lock. The controller today requires that handlers are registered before execution and not in parallel.
	m.secretHandlers = append(m.secretHandlers, h)
}

type AggregateController struct {
	// controllers to use to look up certs. Generally this will consistent of the primary (config) cluster
	// and a single remote cluster where the proxy resides
	controllers    []*CredentialsController
	authController *CredentialsController
}

var _ credentials.Controller = &AggregateController{}

func (a *AggregateController) GetCertInfo(name, namespace string) (certInfo *credentials.CertInfo, err error) {
	// Search through all clusters, find first non-empty result
	var firstError error
	for _, c := range a.controllers {
		certInfo, err := c.GetCertInfo(name, namespace)
		if err != nil {
			if firstError == nil {
				firstError = err
			}
		} else {
			return certInfo, nil
		}
	}
	return nil, firstError
}

func (a *AggregateController) GetCaCert(name, namespace string) (certInfo *credentials.CertInfo, err error) {
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

func (a *AggregateController) GetConfigMapCaCert(name, namespace string) (certInfo *credentials.CertInfo, err error) {
	// Search through all clusters, find first non-empty result
	var firstError error
	for _, c := range a.controllers {
		k, err := c.GetConfigMapCaCert(name, namespace)
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
