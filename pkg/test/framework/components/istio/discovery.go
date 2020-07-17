package istio

import (
	"fmt"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource"
)

// Discovery returns the service discovery client for the given cluster or returns an error
func (i *operatorComponent) Discovery(cluster resource.Cluster) (pilot.Instance, error) {
	// lazy init since it requires some k8s calls, port-forwarding and building grpc clients
	if len(i.discovery) == 0 {
		if err := i.initDiscovery(); err != nil {
			return nil, err
		}
	}

	if d := i.discovery[cluster.Index()]; d != nil {
		return d, nil
	}

	return nil, fmt.Errorf("no service discovery client for %s", cluster.Name())
}

// DiscoveryOrFail returns the service discovery client for the given cluster or fails the test if
// it is not found.
func (i *operatorComponent) DiscoveryOrFail(f test.Failer, cluster resource.Cluster) pilot.Instance {
	d, err := i.Discovery(cluster)
	if err != nil {
		f.Fatal(err)
	}
	return d
}

func (i *operatorComponent) initDiscovery() error {
	env := i.ctx.Environment()
	i.discovery = make([]pilot.Instance, len(i.ctx.Clusters()))
	for _, c := range env.Clusters() {
		cp, err := env.GetControlPlaneCluster(c)
		if err != nil {
			return err
		}
		if inst := i.discovery[cp.Index()]; inst == nil {
			i.discovery[cp.Index()], err = pilot.New(i.ctx, pilot.Config{Cluster: cp})
			if err != nil {
				return fmt.Errorf("error creating pilot instance for cluster %d: %v", cp.Index(), err)
			}
		}
		i.discovery[c.Index()] = i.discovery[cp.Index()]
	}
	return nil
}
