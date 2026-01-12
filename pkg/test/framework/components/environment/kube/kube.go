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

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/config"
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
	clusters, err := buildClusters(configs)
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

func buildClusters(configs []cluster.Config) (cluster.Clusters, error) {
	scopes.Framework.Infof("=== BEGIN: Building clusters ===")

	// use multierror to give as much detail as possible if the config is bad
	var errs error
	defer func() {
		if errs != nil {
			scopes.Framework.Infof("=== FAILED: Building clusters ===")
		}
	}()

	allClusters := make(cluster.Map)
	var clusters cluster.Clusters
	for i, cfg := range configs {
		c, err := buildCluster(cfg, allClusters)
		if err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed building cluster from config %d: %v", i, err))
			continue
		}
		if _, ok := allClusters[c.Name()]; ok {
			errs = multierror.Append(errs, fmt.Errorf("more than one cluster named %s", c.Name()))
			continue
		}
		allClusters[c.Name()] = c
		clusters = append(clusters, c)
	}
	if errs != nil {
		return nil, errs
	}

	// validate the topology has no open edges
	for _, c := range allClusters {
		if _, ok := allClusters[c.PrimaryName()]; !ok {
			errs = multierror.Append(errs, fmt.Errorf("primary %s for %s is not in the topology", c.PrimaryName(), c.Name()))
			continue
		}
		if _, ok := allClusters[c.ConfigName()]; !ok {
			errs = multierror.Append(errs, fmt.Errorf("config %s for %s is not in the topology", c.ConfigName(), c.Name()))
			continue
		}
	}
	if errs != nil {
		return nil, errs
	}

	for _, c := range clusters {
		scopes.Framework.Infof("Built Cluster:\n%s", c.String())
	}

	scopes.Framework.Infof("=== DONE: Building clusters ===")
	return clusters, errs
}

func buildCluster(cfg cluster.Config, allClusters cluster.Map) (cluster.Cluster, error) {
	cfg, err := validConfig(cfg)
	if err != nil {
		return nil, err
	}
	return kube.BuildKube(cfg, cluster.NewTopology(cfg, allClusters))
}

func validConfig(cfg cluster.Config) (cluster.Config, error) {
	if cfg.Name == "" {
		return cfg, fmt.Errorf("empty cluster name")
	}
	if cfg.PrimaryClusterName == "" {
		cfg.PrimaryClusterName = cfg.Name
	}
	if cfg.ConfigClusterName == "" {
		cfg.ConfigClusterName = cfg.PrimaryClusterName
	}
	if cfg.Meta == nil {
		cfg.Meta = config.Map{}
	}
	return cfg, nil
}
