package staticvm

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
)

func init() {
	cluster.RegisterFactory(cluster.StaticVM, build)
}

var _ echo.Cluster = &vmcluster{}

type vmcluster struct {
	kube.ExtendedClient
	cluster.Topology

	vms []echo.Config
}

func build(cfg cluster.Config, topology cluster.Topology) (cluster.Cluster, error) {
	vms, err := readInstances(cfg)
	if err != nil {
		return nil, err
	}
	return &vmcluster{
		Topology: topology,
		vms:      vms,
	}, nil
}

func readInstances(cfg cluster.Config) (out []echo.Config, errs error) {
	for i, deploymentMeta := range cfg.Meta.Slice("deployments") {
		vm, err := instanceFromMeta(deploymentMeta)
		if err != nil {
			errs = multierror.Append(errs, fmt.Errorf("reading deployment config %d of %s: %v", i, cfg.Name, err))
		}
		out = append(out, vm)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("static vm cluster has no deployments provided")
	}
	return
}

func instanceFromMeta(cfg cluster.ConfigMeta) (echo.Config, error) {
	svc := cfg.String("service")
	if svc == "" {
		return echo.Config{}, errors.New("service must not be empty")
	}
	ns := cfg.String("namespace")
	if ns == "" {
		return echo.Config{}, errors.New("namespace must not be empty")
	}

	var ips []string
	for _, meta := range cfg.Slice("instances") {
		ipStr := meta.String("ip")
		ip := net.ParseIP(ipStr)
		if len(ip) == 0 {
			return echo.Config{}, fmt.Errorf("failed parsing %q as IP address", ipStr)
		}
		ips = append(ips, ipStr)
	}

	return echo.Config{
		Namespace: nil,
		Service:   svc,
		// Will set the version of each subset if not provided
		Version:         cfg.String("version"),
		StaticAddresses: ips,
		// TODO support listing subsets
		Subsets: nil,
		// TODO support TLS for gRPC client
		TLSSettings: nil,
	}, nil
}

func (v vmcluster) CanDeploy(config echo.Config) (echo.Config, bool) {
	if !config.DeployAsVM {
		return echo.Config{}, false
	}
	for _, vm := range v.vms {
		if matchConfig(vm, config) {
			vmCfg := config.DeepCopy()
			vmCfg.StaticAddresses = vm.StaticAddresses
			return vmCfg, true
		}
	}
	return echo.Config{}, false
}

func matchConfig(vm, cfg echo.Config) bool {
	return vm.Service == cfg.Service && strings.HasPrefix(cfg.Namespace.Name(), vm.Namespace.Name())
}
