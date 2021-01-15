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

package aggregate

import (
	"fmt"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

// Factory creates a "root" factory, capable of building clusters of any type.
// This aggregate factory should be used rather than manually creating other kinds of cluster.Factory.
type Factory interface {
	cluster.Factory
	BuildOrFail(f test.Failer) resource.Clusters
}

var _ Factory = &aggregateFactory{}

func NewFactory() Factory {
	return &aggregateFactory{
		factories:   make(map[cluster.Kind]cluster.Factory),
		allClusters: map[string]resource.Cluster{},
	}
}

type aggregateFactory struct {
	allClusters map[string]resource.Cluster
	configs     []cluster.Config
	factories   map[cluster.Kind]cluster.Factory
}

func (a *aggregateFactory) Kind() cluster.Kind {
	return cluster.Aggregate
}

func (a *aggregateFactory) With(configs ...cluster.Config) cluster.Factory {
	a.configs = append(a.configs, configs...)
	for _, config := range a.configs {
		a.maybeCreateFactory(config)
	}
	return a
}

func (a *aggregateFactory) Build() (resource.Clusters, error) {
	scopes.Framework.Infof("=== BEGIN: Building clusters ===")

	var errs error
	// distribute configs to their factories
	for i, origCfg := range a.configs {
		cfg, err := validConfig(origCfg)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}
		factory, ok := a.factories[origCfg.Kind]
		if !ok {
			errs = multierror.Append(errs, fmt.Errorf("invalid kind for %s (item %d): %s", cfg.Name, i, cfg.Kind))
			continue
		}
		factory.With(cfg)
	}

	// initialize the clusters
	var clusters resource.Clusters
	for kind, factory := range a.factories {
		scopes.Framework.Infof("Building %s clusters", kind)
		built, err := factory.Build()
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}
		for _, c := range built {
			if _, ok := a.allClusters[c.Name()]; ok {
				errs = multierror.Append(errs, fmt.Errorf("duplicate cluster name: %s", c.Name()))
				continue
			}
			a.allClusters[c.Name()] = c
			scopes.Framework.Infof(c.String())
		}
		clusters = append(clusters, built...)
	}
	if errs != nil {
		scopes.Framework.Infof("=== FAILED: Building clusters ===")
		return nil, errs
	}
	for n, c := range a.allClusters {
		scopes.Framework.Infof("Built Cluster: %s", n)
		scopes.Framework.Infof(c.String())
	}
	scopes.Framework.Infof("=== DONE: Building clusters ===")

	return clusters, errs
}

func (a *aggregateFactory) BuildOrFail(t test.Failer) resource.Clusters {
	out, err := a.Build()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func validConfig(cfg cluster.Config) (cluster.Config, error) {
	if cfg.Name == "" {
		return cfg, fmt.Errorf("empty cluster name")
	}
	if cfg.Kind == "" {
		return cfg, fmt.Errorf("unspecified Kind for %s", cfg.Name)
	}
	if cfg.ControlPlaneClusterName == "" {
		cfg.ControlPlaneClusterName = cfg.Name
	}
	if cfg.ConfigClusterName == "" {
		cfg.ConfigClusterName = cfg.Name
	}
	return cfg, nil
}

// maybeCreateFactory initializes concrete factory implementations.
// If the given Kind is unsupported we err during the build step to allow collecting
// as much validation info as possible.
func (a *aggregateFactory) maybeCreateFactory(config cluster.Config) {
	switch config.Kind {
	case cluster.Kubernetes:
		if a.factories[cluster.Kubernetes] == nil {
			a.factories[cluster.Kubernetes] = kube.NewFactory(a.allClusters)
		}
	}
}
