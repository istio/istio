//go:build integ
// +build integ

//  Copyright Istio Authors
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

package common

import (
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/resource"
)

var prom prometheus.Instance

const (
	Unknown = iota
	IPv4
	IPv6
)

type EchoDeployments struct {
	// Service Name
	ServiceName string
	// IPFamily of echo app only if single-stack IPv4 or IPv6
	IPFamily int
	// Namespace echo apps will be deployed
	Namespace namespace.Instance
	// Standard echo app to be used by tests
	EchoPod echo.Instance
}

type DeploymentConfig struct {
	Name           string
	Service        string
	IPFamily       string
	IPFamilyPolicy string
}

var deploymentConfigs = []DeploymentConfig{
	{Name: "ipv4", Service: "echo-ipv4", IPFamily: "IPv4", IPFamilyPolicy: "SingleStack"},
	{Name: "ipv6", Service: "echo-ipv6", IPFamily: "IPv6", IPFamilyPolicy: "SingleStack"},
	{Name: "dual-ipv4", Service: "echo-dual-ipv4", IPFamily: "IPv4, IPv6", IPFamilyPolicy: "RequireDualStack"},
	{Name: "dual-ipv6", Service: "echo-dual-ipv6", IPFamily: "IPv6, IPv4", IPFamilyPolicy: "RequireDualStack"},
}

var EchoPorts = []echo.Port{
	{Name: "http", Protocol: protocol.HTTP, ServicePort: 80, WorkloadPort: 18080},
	{Name: "grpc", Protocol: protocol.GRPC, ServicePort: 7070, WorkloadPort: 17070},
	{Name: "tcp", Protocol: protocol.TCP, ServicePort: 9090, WorkloadPort: 19090},
	{Name: "https", Protocol: protocol.HTTPS, ServicePort: 443, WorkloadPort: 18443, TLS: true},
	{Name: "tcp-server", Protocol: protocol.TCP, ServicePort: 9091, WorkloadPort: 16060, ServerFirst: true},
	{Name: "auto-tcp", Protocol: protocol.TCP, ServicePort: 9092, WorkloadPort: 19091},
	{Name: "auto-tcp-server", Protocol: protocol.TCP, ServicePort: 9093, WorkloadPort: 16061, ServerFirst: true},
	{Name: "auto-http", Protocol: protocol.HTTP, ServicePort: 81, WorkloadPort: 18081},
	{Name: "auto-grpc", Protocol: protocol.GRPC, ServicePort: 7071, WorkloadPort: 17071},
	{Name: "auto-https", Protocol: protocol.HTTPS, ServicePort: 9443, WorkloadPort: 19443},
	{Name: "http-instance", Protocol: protocol.HTTP, ServicePort: 82, WorkloadPort: 18082, InstanceIP: true},
	{Name: "http-localhost", Protocol: protocol.HTTP, ServicePort: 84, WorkloadPort: 18084, LocalhostIP: true},

	// Workload-only ports
	{Name: "tcp-wl-only", Protocol: protocol.TCP, ServicePort: echo.NoServicePort, WorkloadPort: 19092},
	{Name: "http-wl-only", Protocol: protocol.HTTP, ServicePort: echo.NoServicePort, WorkloadPort: 18083},
}

func SetupApps(ctx resource.Context, apps map[string]*EchoDeployments) error {
	var err error
	for _, deploy := range deploymentConfigs {
		apps[deploy.Name] = &EchoDeployments{}

		switch deploy.IPFamily {
		case "IPv4":
			apps[deploy.Name].IPFamily = IPv4
		case "IPv6":
			apps[deploy.Name].IPFamily = IPv6
		}

		apps[deploy.Name].ServiceName = deploy.Service

		apps[deploy.Name].Namespace, err = namespace.New(ctx, namespace.Config{
			Prefix: deploy.Name,
			Inject: true,
		})
		if err != nil {
			return err
		}

		builder := deployment.New(ctx).
			WithClusters(ctx.Clusters()...).
			With(&apps[deploy.Name].EchoPod, echo.Config{
				Service:        deploy.Service,
				Namespace:      apps[deploy.Name].Namespace,
				Ports:          EchoPorts,
				Subsets:        []echo.SubsetConfig{{}},
				IPFamilies:     deploy.IPFamily,
				IPFamilyPolicy: deploy.IPFamilyPolicy,
			})

		_, err := builder.Build()
		if err != nil {
			return err
		}
	}
	return nil
}

func SetupPrometheus(ctx resource.Context) (err error) {
	prom, err = prometheus.New(ctx, prometheus.Config{})
	return err
}
