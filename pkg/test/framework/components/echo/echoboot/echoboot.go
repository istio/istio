package echoboot

import (
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/kube"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

// NewBuilder for Echo Instances.
func NewBuilder(ctx resource.Context, clusters ...resource.Cluster) Builder {
	return builder{
		kubeBuilder: kube.NewBuilder(ctx),
	}.WithClusters(clusters...)
}

// Builder is a superset of echo.Builder, which allows deploying the same echo configuration accross clusters.
type Builder interface {
	echo.Builder

	// WithClusters deploys to the given clusters all configs provided to With, that don't specify a cluster.
	// If WithClusters is called multiple times it will replace the set of clusters for which subsequent With
	// calls are applied to.
	WithClusters(...resource.Cluster) Builder
}

var _ Builder = builder{}

type builder struct {
	clusters    resource.Clusters
	kubeBuilder echo.Builder
}

// With adds a new Echo configuration to the Builder. When a cluster is provided in the Config, it will only be applied
// to that cluster, otherwise the Config is applied to all WithClusters. Once built, if being built for a sngle cluster,
// the instance pointer will be updated to point at the new Instance.
func (b builder) With(i *echo.Instance, cfg echo.Config) echo.Builder {
	if len(b.clusters) == 0 && cfg.Cluster == nil {
		scopes.Framework.Warnf("echo config %s has no cluster and builder has no WithClusters", cfg.Service)
		return b
	}

	// Per-config cluster override
	if cfg.Cluster != nil {
		b.kubeBuilder = b.kubeBuilder.With(i, cfg)
		return b
	}

	// WithClusters
	// only provide the ref for a single cluster
	var ref *echo.Instance
	if len(b.clusters) == 1 {
		ref = i
	}
	// provide a config for each cluster to the kubeBuilder
	for _, cluster := range b.clusters {
		// TODO add a mechanism to allow "claiming" a config, and not providing it to other clusters
		clusterCfg := cfg
		clusterCfg.Cluster = cluster
		b.kubeBuilder.With(ref, clusterCfg)
	}

	return b
}

// WithClusters will cause subsequent With calls to be applied to the given clusters.
func (b builder) WithClusters(clusters ...resource.Cluster) Builder {
	next := b
	next.clusters = clusters
	return next
}

func (b builder) Build() (echo.Instances, error) {
	return b.kubeBuilder.Build()
}

func (b builder) BuildOrFail(t test.Failer) echo.Instances {
	return b.kubeBuilder.BuildOrFail(t)
}
