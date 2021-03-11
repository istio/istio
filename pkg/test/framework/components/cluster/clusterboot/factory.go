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

package clusterboot

import (
	"fmt"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/framework/components/cluster"

	// imported to trigger registration
	_ "istio.io/istio/pkg/test/framework/components/cluster/asmvm"

	// imported to trigger registration
	_ "istio.io/istio/pkg/test/framework/components/cluster/kube"

	// imported to trigger registration
	_ "istio.io/istio/pkg/test/framework/components/cluster/staticvm"
	"istio.io/istio/pkg/test/framework/config"
	"istio.io/istio/pkg/test/scopes"
)

var _ cluster.Factory = factory{}

func NewFactory() cluster.Factory {
	return factory{}
}

type factory struct {
	configs []cluster.Config
}

func (a factory) Kind() cluster.Kind {
	return cluster.Aggregate
}

func (a factory) With(configs ...cluster.Config) cluster.Factory {
	return factory{configs: append(a.configs, configs...)}
}

func (a factory) Build() (cluster.Clusters, error) {
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
	for i, cfg := range a.configs {
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
		if primary, ok := allClusters[c.PrimaryName()]; !ok {
			errs = multierror.Append(errs, fmt.Errorf("primary %s for %s is not in the topology", c.PrimaryName(), c.Name()))
			continue
		} else if !validPrimaryOrConfig(primary) {
			errs = multierror.Append(errs, fmt.Errorf("primary %s for %s is of kind %s, primaries must be Kubernetes", primary.Name(), c.Name(), primary.Kind()))
			continue
		}
		if cfg, ok := allClusters[c.ConfigName()]; !ok {
			errs = multierror.Append(errs, fmt.Errorf("config %s for %s is not in the topology", c.ConfigName(), c.Name()))
			continue
		} else if !validPrimaryOrConfig(cfg) {
			errs = multierror.Append(errs, fmt.Errorf("config %s for %s is of kind %s, primaries must be Kubernetes", cfg.Name(), c.Name(), cfg.Kind()))
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

func validPrimaryOrConfig(c cluster.Cluster) bool {
	return c.Kind() == cluster.Kubernetes || c.Kind() == cluster.Fake
}

func buildCluster(cfg cluster.Config, allClusters cluster.Map) (cluster.Cluster, error) {
	cfg, err := validConfig(cfg)
	if err != nil {
		return nil, err
	}
	f, err := cluster.GetFactory(cfg)
	if err != nil {
		return nil, err
	}
	return f(cfg, cluster.NewTopology(cfg, allClusters))
}

func validConfig(cfg cluster.Config) (cluster.Config, error) {
	if cfg.Name == "" {
		return cfg, fmt.Errorf("empty cluster name")
	}
	if cfg.Kind == "" {
		return cfg, fmt.Errorf("unspecified Kind for %s", cfg.Name)
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
