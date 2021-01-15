package kube

import (
	"fmt"
	"github.com/hashicorp/go-multierror"
	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
	"k8s.io/client-go/rest"
)

// NewFactory creates a new kube Cluster factory, using a slice-pointer that will be filled
// with every possibly-related cluster, to allow checking topology info (network, primary, config).
func NewFactory(allClusters map[string]resource.Cluster) cluster.Factory {
	return &factory{allClusters: allClusters}
}

type factory struct {
	allClusters map[string]resource.Cluster
	configs     []cluster.Config
}

func (f *factory) Kind() cluster.Kind {
	return cluster.Kubernetes
}

func (f *factory) With(configs ...cluster.Config) cluster.Factory {
	for _, c := range configs {
		f.configs = append(f.configs, c)
	}
	return f
}

func (f *factory) Build() (resource.Clusters, error) {
	var errs error

	var clusters resource.Clusters
	for _, origCfg := range f.configs {
		cfg, err := validConfig(origCfg)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}
		kubeconfigPath := cfg.Meta["kubeconfig"]
		client, err := buildClient(kubeconfigPath)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}
		clusters = append(clusters, &Cluster{
			filename:       kubeconfigPath,
			ExtendedClient: client,
			Topology: cluster.NewTopology(
				cfg.Name,
				cfg.Network,
				cfg.ControlPlaneClusterName,
				cfg.ConfigClusterName,
				f.allClusters,
			),
		})
	}

	return clusters, nil
}

func validConfig(cfg cluster.Config) (cluster.Config, error) {
	// only include kube-specific validation here
	if cfg.Meta == nil || cfg.Meta["kubeconfig"] == "" {
		return cfg, fmt.Errorf("missing meta.kubeconfig for %s", cfg.Name)
	}
	return cfg, nil
}

func buildClient(kubeconfig string) (istioKube.ExtendedClient, error) {
	rc, err := istioKube.DefaultRestConfig(kubeconfig, "", func(config *rest.Config) {
		config.QPS = 200
		config.Burst = 400
	})
	if err != nil {
		return nil, err
	}
	return istioKube.NewExtendedClient(istioKube.NewClientConfigForRestConfig(rc), "")
}
