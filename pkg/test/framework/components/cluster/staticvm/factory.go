package staticvm

import (
	"fmt"
	"github.com/hashicorp/go-multierror"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/resource"
	"net"
)

func init() {
	cluster.RegisterFactory(factory{})
}

var _ cluster.Factory = factory{}

type factory struct {
	configs []cluster.Config
}

func (f factory) Kind() cluster.Kind {
	return cluster.StaticVM
}

func (f factory) With(configs ...cluster.Config) cluster.Factory {
	return factory{
		configs: append(f.configs, configs...),
	}
}

func (f factory) Build(allClusters cluster.Map) (resource.Clusters, error) {
	var errs error
	var out resource.Clusters
	for _, config := range f.configs {
		echos, err := extractEchoConfigs(config)
		if err != nil {
			errs = multierror.Append(errs, err)
			continue
		}
		out = append(out, &vmcluster{
			Topology: cluster.Topology{
				ClusterName:        config.Name,
				ClusterKind:        cluster.StaticVM,
				Network:            config.Network,
				PrimaryClusterName: config.PrimaryClusterName,
				ConfigClusterName:  config.ConfigClusterName,
				AllClusters:        allClusters,
			},
			vms: echos,
		})
	}
	if errs != nil {
		return nil, errs
	}
	return out, nil
}

// extractEchoConfigs reads the metadata from the config into echo.Config structs with some light validation.
func extractEchoConfigs(config cluster.Config) ([]echo.Config, error) {
	instances := config.Meta.Slice("instances")
	if len(instances) == 0 {
		return nil, fmt.Errorf("meta.instances was invalid or not provided for %s", config.Name)
	}
	var out []echo.Config
	for i, vm := range instances {
		name := vm.String("name")
		if name == "" {
			return nil, fmt.Errorf("missing name for vm %d in %s", i, config.Name)
		}
		namespace := vm.String("namespace")
		if namespace == "" {
			return nil, fmt.Errorf("missing namespace for vm %d in %s", i, config.Name)
		}
		address := vm.String("address")
		if namespace == "" {
			return nil, fmt.Errorf("missing address for vm %d in %s", i, config.Name)
		}
		if ip := net.ParseIP(address); len(ip) == 0 {
			return nil, fmt.Errorf("invalid ip for vm %d in %s", i, config.Name)
		}

		var ports []echo.Port
		portConfigs := vm.Slice("ports")
		hasGrpc := false
		for j, portConfig := range portConfigs {
			proto := protocol.Parse(portConfig.String("protocol"))
			if proto == protocol.Unsupported {
				return nil, fmt.Errorf("invalid protocol for port %d of %s in %s", j, name, config.Name)
			}
			if proto == protocol.GRPC {
				hasGrpc = true
			}
			num := portConfig.Int("port")
			if num == 0 {
				return nil, fmt.Errorf("missing number for port %d of %s in %s", j, name, config.Name)
			}
			ports = append(ports, echo.Port{
				Name:         portConfig.String("name"),
				Protocol:     proto,
				ServicePort:  num,
				InstancePort: num,
			})
		}
		if !hasGrpc {
			return nil, fmt.Errorf("vm config for %s in %s did not contain a grpc port required for echo interactions", name, config.Name)
		}

		out = append(out, echo.Config{
			Service:        name,
			Namespace:      fakeNamespace(namespace),
			DeployAsVM:     true,
			Ports:          ports,
			AutoRegisterVM: vm.Bool("autoRegister"),
			StaticAddress:  address,
		})
	}
	return out, nil
}
