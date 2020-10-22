// +build integ
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

package util

import (
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

const (
	ASvc             = "a"
	BSvc             = "b"
	MultiversionSvc  = "multiversion"
	VMSvc            = "vm"
	HeadlessSvc      = "headless"
	NakedSvc         = "naked"
	HeadlessNakedSvc = "headless-naked"

	// CallsPerCluster is used to ensure cross-cluster load balancing has a chance to work
	CallsPerCluster = 5
)

type EchoDeployments struct {
	Namespace     namespace.Instance
	A, B          echo.Instances
	Multiversion  echo.Instances
	Headless      echo.Instances
	Naked         echo.Instances
	VM            echo.Instances
	HeadlessNaked echo.Instances
	All           echo.Instances
}

func EchoConfig(name string, ns namespace.Instance, headless bool, annos echo.Annotations, cluster resource.Cluster) echo.Config {
	out := echo.Config{
		Service:        name,
		Namespace:      ns,
		ServiceAccount: true,
		Headless:       headless,
		Subsets: []echo.SubsetConfig{
			{
				Version:     "v1",
				Annotations: annos,
			},
		},
		Ports: []echo.Port{
			{
				Name:     "http",
				Protocol: protocol.HTTP,
				// We use a port > 1024 to not require root
				InstancePort: 8090,
			},
			{
				Name:     "tcp",
				Protocol: protocol.TCP,
			},
			{
				Name:     "grpc",
				Protocol: protocol.GRPC,
			},
		},
		Cluster: cluster,
	}

	// for headless service with selector, the port and target port must be equal
	// Ref: https://kubernetes.io/docs/concepts/services-networking/service/#headless-services
	if headless {
		out.Ports[0].ServicePort = 8090
	}
	return out
}

func SetupApps(ctx resource.Context, i istio.Instance, apps *EchoDeployments, buildVM bool) error {
	var err error
	apps.Namespace, err = namespace.New(ctx, namespace.Config{
		Prefix: "reachability",
		Inject: true,
	})
	if err != nil {
		return err
	}
	builder := echoboot.NewBuilder(ctx)
	for _, cluster := range ctx.Clusters() {
		// Multi-version specific setup
		cfg := EchoConfig(MultiversionSvc, apps.Namespace, false, nil, cluster)
		cfg.Subsets = []echo.SubsetConfig{
			// Istio deployment, with sidecar.
			{
				Version: "vistio",
			},
			// Legacy deployment subset, does not have sidecar injected.
			{
				Version:     "vlegacy",
				Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
			},
		}
		builder.
			With(nil, EchoConfig(ASvc, apps.Namespace, false, nil, cluster)).
			With(nil, EchoConfig(BSvc, apps.Namespace, false, nil, cluster)).
			With(nil, cfg).
			With(nil, EchoConfig(NakedSvc, apps.Namespace, false, echo.NewAnnotations().
				SetBool(echo.SidecarInject, false), cluster))
	}
	for _, c := range ctx.Clusters().ByNetwork() {
		// VM specific setup
		vmCfg := EchoConfig(VMSvc, apps.Namespace, false, nil, c[0])
		// for test cases that have `buildVM` off, vm will function like a regular pod
		vmCfg.DeployAsVM = buildVM
		builder.With(nil, vmCfg)
		builder.With(nil, EchoConfig(HeadlessSvc, apps.Namespace, true, nil, c[0]))
		builder.With(nil, EchoConfig(HeadlessNakedSvc, apps.Namespace, true, echo.NewAnnotations().
			SetBool(echo.SidecarInject, false), c[0]))
	}
	echos, err := builder.Build()
	if err != nil {
		return err
	}
	apps.All = echos
	apps.A = echos.Match(echo.Service(ASvc))
	apps.B = echos.Match(echo.Service(BSvc))
	apps.Multiversion = echos.Match(echo.Service(MultiversionSvc))
	apps.Headless = echos.Match(echo.Service(HeadlessSvc))
	apps.Naked = echos.Match(echo.Service(NakedSvc))
	apps.VM = echos.Match(echo.Service(VMSvc))
	apps.HeadlessNaked = echos.Match(echo.Service(HeadlessNakedSvc))

	return nil
}

func (apps *EchoDeployments) IsNaked(i echo.Instance) bool {
	return apps.HeadlessNaked.Contains(i) || apps.Naked.Contains(i)
}

func (apps *EchoDeployments) IsHeadless(i echo.Instance) bool {
	return apps.HeadlessNaked.Contains(i) || apps.Headless.Contains(i)
}
