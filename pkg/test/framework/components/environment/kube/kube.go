//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package kube

import (
	"fmt"

	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

// Environment is the implementation of a kubernetes environment. It implements environment.Environment,
// and also hosts publicly accessible methods that are specific to cluster environment.
type Environment struct {
	id           resource.ID
	ctx          resource.Context
	KubeClusters []Cluster
	s            *Settings
}

var _ resource.Environment = &Environment{}

// New returns a new Kubernetes environment
func New(ctx resource.Context, s *Settings) (resource.Environment, error) {
	scopes.Framework.Infof("Test Framework Kubernetes environment Settings:\n%s", s)

	e := &Environment{
		ctx: ctx,
		s:   s,
	}
	e.id = ctx.TrackResource(e)

	clients, err := s.NewClients()
	if err != nil {
		return nil, err
	}

	e.KubeClusters = make([]Cluster, 0, len(clients))
	for i := range clients {
		client := clients[i]
		clusterIndex := resource.ClusterIndex(i)
		e.KubeClusters = append(e.KubeClusters, Cluster{
			networkName:    s.networkTopology[clusterIndex],
			filename:       s.KubeConfig[i],
			index:          clusterIndex,
			ExtendedClient: client,
		})
	}

	return e, nil
}

func (e *Environment) EnvironmentName() string {
	return "Kube"
}

func (e *Environment) IsMulticluster() bool {
	return len(e.KubeClusters) > 1
}

// IsMultinetwork returns true if there is more than one network name in networkTopology.
func (e *Environment) IsMultinetwork() bool {
	return len(e.ClustersByNetwork()) > 1
}

func (e *Environment) Clusters() resource.Clusters {
	out := make([]resource.Cluster, 0, len(e.KubeClusters))
	for _, c := range e.KubeClusters {
		out = append(out, c)
	}
	return out
}

func (e *Environment) ControlPlaneClusters(excludedClusters ...resource.Cluster) []resource.Cluster {
	out := make([]resource.Cluster, 0, len(e.KubeClusters))

	isExcluded := func(c resource.Cluster) bool {
		for _, excludedCluster := range excludedClusters {
			if c.Name() == excludedCluster.Name() {
				return true
			}
		}
		return false
	}

	for _, c := range e.KubeClusters {
		if !isExcluded(c) && e.IsControlPlaneCluster(c) {
			out = append(out, c)
		}
	}
	return out
}

// IsControlPlaneCluster returns true if the cluster uses its own control plane in the ControlPlaneTopology.
// We return if there is no mapping for the cluster, similar to the behavior of the istio.test.kube.controlPlaneTopology.
func (e *Environment) IsControlPlaneCluster(cluster resource.Cluster) bool {
	if controlPlaneIndex, ok := e.Settings().ControlPlaneTopology[cluster.Index()]; ok {
		return controlPlaneIndex == cluster.Index()
	}
	return true
}

// GetControlPlaneCluster returns the cluster running the control plane for the given cluster based on the ControlPlaneTopology.
// An error is returned if the given cluster isn't present in the topology, or the cluster in the topology isn't in KubeClusters.
func (e *Environment) GetControlPlaneCluster(cluster resource.Cluster) (resource.Cluster, error) {
	if controlPlaneIndex, ok := e.Settings().ControlPlaneTopology[cluster.Index()]; ok {
		if int(controlPlaneIndex) >= len(e.KubeClusters) {
			err := fmt.Errorf("control plane index %d out of range in %d configured clusters", controlPlaneIndex, len(e.KubeClusters))
			return nil, err
		}
		return e.KubeClusters[controlPlaneIndex], nil
	}
	return nil, fmt.Errorf("no control plane cluster found in topology for cluster %s", cluster.Name())
}

// ClustersByNetwork returns an inverse mapping of the network topolgoy to a slice of clusters in a given network.
func (e *Environment) ClustersByNetwork() map[string][]*Cluster {
	out := make(map[string][]*Cluster)
	for clusterIdx, networkName := range e.s.networkTopology {
		out[networkName] = append(out[networkName], &e.KubeClusters[clusterIdx])
	}
	return out
}

// ID implements resource.Instance
func (e *Environment) ID() resource.ID {
	return e.id
}

func (e *Environment) Settings() *Settings {
	return e.s.clone()
}
