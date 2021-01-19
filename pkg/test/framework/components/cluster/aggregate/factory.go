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

var _ Factory = aggregateFactory{}

func NewFactory() Factory {
	return aggregateFactory{}
}

type aggregateFactory struct {
	configs []cluster.Config
}

func (a aggregateFactory) Kind() cluster.Kind {
	return cluster.Aggregate
}

func (a aggregateFactory) With(configs ...cluster.Config) cluster.Factory {
	return aggregateFactory{configs: append(a.configs, configs...)}
}

func (a aggregateFactory) Build(allClusters cluster.Map) (resource.Clusters, error) {
	scopes.Framework.Infof("=== BEGIN: Building clusters ===")

	factories := make(map[cluster.Kind]cluster.Factory)

	var errs error
	// distribute configs to their factories
	for _, origCfg := range a.configs {
		cfg, err := validConfig(origCfg)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}
		err = maybeCreateFactory(factories, cfg)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}
		factories[cfg.Kind] = factories[cfg.Kind].With(cfg)
	}

	// initialize the clusters
	var clusters resource.Clusters
	for kind, factory := range factories {
		scopes.Framework.Infof("Building %s clusters", kind)
		built, err := factory.Build(allClusters)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}
		for _, c := range built {
			if _, ok := allClusters[c.Name()]; ok {
				errs = multierror.Append(errs, fmt.Errorf("duplicate cluster name: %s", c.Name()))
				continue
			}
			allClusters[c.Name()] = c
			scopes.Framework.Infof(c.String())
		}
		clusters = append(clusters, built...)
	}
	if errs != nil {
		scopes.Framework.Infof("=== FAILED: Building clusters ===")
		return nil, errs
	}
	for n, c := range allClusters {
		scopes.Framework.Infof("Built Cluster: %s", n)
		scopes.Framework.Infof(c.String())
	}
	scopes.Framework.Infof("=== DONE: Building clusters ===")

	return clusters, errs
}

func (a aggregateFactory) BuildOrFail(t test.Failer) resource.Clusters {
	out, err := a.Build(map[string]resource.Cluster{})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// maybeCreateFactory initializes concrete factory implementations.
// If the given Kind is unsupported we err during the build step to allow collecting
// as much validation info as possible.
func maybeCreateFactory(factories map[cluster.Kind]cluster.Factory, config cluster.Config) error {
	switch config.Kind {
	case cluster.Kubernetes:
		if factories[cluster.Kubernetes] == nil {
			factories[cluster.Kubernetes] = kube.NewFactory()
		}
	default:
		return fmt.Errorf("unsupported cluster kind: %q", config.Kind)
	}
	return nil
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
