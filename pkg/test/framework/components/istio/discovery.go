package istio

import (
	"fmt"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource"
)

// ServiceDiscovery returns the service discovery client for the given cluster or returns an error
func (i *operatorComponent) ServiceDiscovery(cluster resource.Cluster) (pilot.Instance, error) {
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

// ServiceDiscoveryOrFail returns the service discovery client for the given cluster or fails the test if
// it is not found.
func (i *operatorComponent) ServiceDiscoveryOrFail(f test.Failer, cluster resource.Cluster) pilot.Instance {
	d, err := i.ServiceDiscovery(cluster)
	if err != nil {
		f.Fatal(err)
	}
	return d
}

func (i *operatorComponent) initDiscovery() error {
	// TODO merge environment and kube environment or at least move ControlPlaneCluster to main interface
	env := i.ctx.Environment().(*kube.Environment)
	i.discovery = make([]pilot.Instance, len(i.ctx.Clusters()))
	for _, c := range env.KubeClusters {
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
}
