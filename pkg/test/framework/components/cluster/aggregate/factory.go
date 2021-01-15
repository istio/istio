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

func (a *aggregateFactory) With(config ...cluster.Config) cluster.Factory {
	for _, c := range config {
		a.configs = append(a.configs, c)
	}
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
			a.allClusters[c.Name()] = c
			scopes.Framework.Infof("Built cluster:")
			scopes.Framework.Infof(c.String())
		}
		clusters = append(clusters, built...)
	}

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
