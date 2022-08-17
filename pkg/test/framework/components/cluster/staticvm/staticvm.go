// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package staticvm

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"k8s.io/apimachinery/pkg/version"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/config"
	"istio.io/istio/pkg/test/scopes"
)

func init() {
	cluster.RegisterFactory(cluster.StaticVM, build)
}

var _ echo.Cluster = &vmcluster{}

type vmcluster struct {
	kube.CLIClient
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

func readInstances(cfg cluster.Config) ([]echo.Config, error) {
	var out []echo.Config
	deployments := cfg.Meta.Slice("deployments")
	for i, deploymentMeta := range deployments {
		vm, err := instanceFromMeta(deploymentMeta)
		if err != nil {
			scopes.Framework.Errorf("failed reading deployment config %d of %s: %v", i, cfg.Name, err)
		}
		out = append(out, vm)
	}
	if len(out) == 0 || len(out) != len(deployments) {
		return nil, fmt.Errorf("static vm cluster %s has no deployments provided", cfg.Name)
	}
	return out, nil
}

func instanceFromMeta(cfg config.Map) (echo.Config, error) {
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
		publicIPStr := meta.String("ip")
		privateIPStr := meta.String("instanceIP")
		ip := net.ParseIP(publicIPStr)
		if len(ip) == 0 {
			return echo.Config{}, fmt.Errorf("failed parsing %q as IP address", publicIPStr)
		}
		ip = net.ParseIP(privateIPStr)
		if len(ip) == 0 {
			return echo.Config{}, fmt.Errorf("failed parsing %q as IP address", privateIPStr)
		}
		ips = append(ips, publicIPStr+":"+privateIPStr)
	}
	if len(ips) == 0 {
		return echo.Config{}, fmt.Errorf("%s has no IPs", svc)
	}

	return echo.Config{
		Namespace: namespace.Static(ns),
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

func (v vmcluster) GetKubernetesVersion() (*version.Info, error) {
	return nil, nil
}

func matchConfig(vm, cfg echo.Config) bool {
	return vm.Service == cfg.Service && strings.HasPrefix(cfg.Namespace.Name(), vm.Namespace.Name())
}
