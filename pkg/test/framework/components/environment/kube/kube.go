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
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/cluster/clusterboot"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

// Environment is the implementation of a kubernetes environment. It implements environment.Environment,
// and also hosts publicly accessible methods that are specific to cluster environment.
type Environment struct {
	id       resource.ID
	ctx      resource.Context
	clusters []cluster.Cluster
	s        *Settings
}

var _ resource.Environment = &Environment{}

// New returns a new Kubernetes environment
func New(ctx resource.Context, s *Settings) (env resource.Environment, err error) {
	defer func() {
		if err != nil && !ctx.Settings().CIMode {
			scopes.Framework.Infof(`
There was an error while setting up the Kubernetes test environment.
Check the test framework wiki for details on running the tests locally:
    https://github.com/istio/istio/wiki/Istio-Test-Framework
`)
		}
	}()
	scopes.Framework.Infof("Test Framework Kubernetes environment Settings:\n%s", s)
	e := &Environment{
		ctx: ctx,
		s:   s,
	}
	e.id = ctx.TrackResource(e)

	configs, err := s.clusterConfigs()
	if err != nil {
		return nil, err
	}
	clusters, err := clusterboot.NewFactory().With(configs...).Build()
	if err != nil {
		return nil, err
	}
	e.clusters = clusters

	return e, nil
}

func (e *Environment) EnvironmentName() string {
	return "Kube"
}

func (e *Environment) IsMultiCluster() bool {
	return len(e.clusters) > 1
}

// IsMultinetwork returns true if there is more than one network name in networkTopology.
func (e *Environment) IsMultiNetwork() bool {
	return len(e.ClustersByNetwork()) > 1
}

func (e *Environment) AllClusters() cluster.Clusters {
	out := make([]cluster.Cluster, 0, len(e.clusters))
	out = append(out, e.clusters...)
	return out
}

func (e *Environment) Clusters() cluster.Clusters {
	return e.AllClusters().MeshClusters()
}

// ClustersByNetwork returns an inverse mapping of the network topolgoy to a slice of clusters in a given network.
func (e *Environment) ClustersByNetwork() map[string][]cluster.Cluster {
	out := make(map[string][]cluster.Cluster)
	for _, c := range e.Clusters() {
		out[c.NetworkName()] = append(out[c.NetworkName()], c)
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
