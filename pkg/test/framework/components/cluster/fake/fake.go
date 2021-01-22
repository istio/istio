package fake

import (
	"fmt"
	"github.com/hashicorp/go-multierror"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
	"k8s.io/client-go/rest"
)

var _ resource.Cluster = Cluster{}

func init() {
	cluster.RegisterFactory(factory{})
}

type factory struct {
	configs []cluster.Config
}

func (f factory) Kind() cluster.Kind {
	return cluster.Fake
}

func (f factory) With(configs ...cluster.Config) cluster.Factory {
	return factory{configs: append(f.configs, configs...)}
}

func (f factory) Build(allClusters cluster.Map) (resource.Clusters, error) {
	var clusters resource.Clusters
	for _, cfg := range f.configs {
		clusters = append(clusters, &Cluster{
			Topology: cluster.Topology{
				ClusterName:        cfg.Name,
				Network:            cfg.Network,
				PrimaryClusterName: cfg.PrimaryClusterName,
				ConfigClusterName:  cfg.ConfigClusterName,
				AllClusters:        allClusters,
			},
		})
	}

	return clusters, nil
}

// Cluster used for testing.
type Cluster struct {
	kube.ExtendedClient

	cluster.Topology
}

func (m Cluster) String() string {
	panic("implement me")
}
