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
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

const (
	ASvc             = "a"
	BSvc             = "b"
	CSvc             = "c"
	DSvc             = "d"
	MultiversionSvc  = "multiversion"
	VMSvc            = "vm"
	HeadlessSvc      = "headless"
	NakedSvc         = "naked"
	HeadlessNakedSvc = "headless-naked"

	// CallsPerCluster is used to ensure cross-cluster load balancing has a chance to work
	CallsPerCluster = 5
)

type EchoDeployments struct {
	// TODO: Consolidate the echo config and reduce/reuse echo instances (https://github.com/istio/istio/issues/28599)
	// Namespace1 is used as the default namespace for reachability tests and other tests which can reuse the same config for echo instances
	Namespace1 namespace.Instance
	// Namespace2 is used by most authorization test cases within authorization_test.go
	Namespace2 namespace.Instance
	// Namespace3 is used by TestAuthorization_Conditions and there is one C echo instance deployed
	Namespace3    namespace.Instance
	A, B, C, D    echo.Instances
	Multiversion  echo.Instances
	Headless      echo.Instances
	Naked         echo.Instances
	VM            echo.Instances
	HeadlessNaked echo.Instances
	All           echo.Instances
}

func EchoConfig(name string, ns namespace.Instance, headless bool, annos echo.Annotations) echo.Config {
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
			{
				Name:         "https",
				Protocol:     protocol.HTTPS,
				ServicePort:  443,
				InstancePort: 8443,
				TLS:          true,
			},
		},
		// Workload Ports needed by TestPassThroughFilterChain
		// The port 8085,8086,8087,8088,8089 will be defined only in the workload and not in the k8s service.
		WorkloadOnlyPorts: []echo.WorkloadPort{
			{
				Port:     8085,
				Protocol: protocol.HTTP,
			},
			{
				Port:     8086,
				Protocol: protocol.HTTP,
			},
			{
				Port:     8087,
				Protocol: protocol.TCP,
			},
			{
				Port:     8088,
				Protocol: protocol.TCP,
			},
			{
				Port:     8089,
				Protocol: protocol.HTTPS,
			},
		},
	}

	// for headless service with selector, the port and target port must be equal
	// Ref: https://kubernetes.io/docs/concepts/services-networking/service/#headless-services
	if headless {
		for i := range out.Ports {
			out.Ports[i].ServicePort = out.Ports[i].InstancePort
		}
	}
	return out
}

func SetupApps(ctx resource.Context, i istio.Instance, apps *EchoDeployments, buildVM bool) error {
	if ctx.Settings().SkipVM {
		buildVM = false
	}
	var err error
	apps.Namespace1, err = namespace.New(ctx, namespace.Config{
		Prefix: "test-ns1",
		Inject: true,
	})
	if err != nil {
		return err
	}
	apps.Namespace2, err = namespace.New(ctx, namespace.Config{
		Prefix: "test-ns2",
		Inject: true,
	})
	if err != nil {
		return err
	}
	apps.Namespace3, err = namespace.New(ctx, namespace.Config{
		Prefix: "test-ns3",
		Inject: true,
	})
	if err != nil {
		return err
	}

	builder := echoboot.NewBuilder(ctx).
		WithClusters(ctx.Clusters()...).
		WithConfig(EchoConfig(ASvc, apps.Namespace1, false, nil)).
		WithConfig(EchoConfig(BSvc, apps.Namespace1, false, nil)).
		WithConfig(EchoConfig(CSvc, apps.Namespace1, false, nil)).
		WithConfig(EchoConfig(DSvc, apps.Namespace1, false, nil)).
		WithConfig(func() echo.Config {
			// Multi-version specific setup
			multiVersionCfg := EchoConfig(MultiversionSvc, apps.Namespace1, false, nil)
			multiVersionCfg.Subsets = []echo.SubsetConfig{
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
			return multiVersionCfg
		}()).
		WithConfig(EchoConfig(NakedSvc, apps.Namespace1, false, echo.NewAnnotations().
			SetBool(echo.SidecarInject, false))).
		WithConfig(EchoConfig(BSvc, apps.Namespace2, false, nil)).
		WithConfig(EchoConfig(CSvc, apps.Namespace2, false, nil)).
		WithConfig(EchoConfig(CSvc, apps.Namespace3, false, nil)).
		WithConfig(func() echo.Config {
			// VM specific setup
			vmCfg := EchoConfig(VMSvc, apps.Namespace1, false, nil)
			// for test cases that have `buildVM` off, vm will function like a regular pod
			vmCfg.DeployAsVM = buildVM
			return vmCfg
		}()).
		WithConfig(EchoConfig(HeadlessSvc, apps.Namespace1, true, nil)).
		WithConfig(EchoConfig(HeadlessNakedSvc, apps.Namespace1, true, echo.NewAnnotations().
			SetBool(echo.SidecarInject, false)))

	echos, err := builder.Build()
	if err != nil {
		return err
	}
	apps.All = echos
	apps.A = echos.Match(echo.Service(ASvc))
	apps.B = echos.Match(echo.Service(BSvc))
	apps.C = echos.Match(echo.Service(CSvc))
	apps.D = echos.Match(echo.Service(DSvc))

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

func (apps *EchoDeployments) IsVM(i echo.Instance) bool {
	return apps.VM.Contains(i)
}

func WaitForConfig(ctx framework.TestContext, configs string, namespace namespace.Instance) {
	ik := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})
	if err := ik.WaitForConfigs(namespace.Name(), configs); err != nil {
		// Continue anyways, so we can assess the effectiveness of using `istioctl wait`
		ctx.Logf("warning: failed to wait for config: %v", err)
		// Get proxy status for additional debugging
		s, _, _ := ik.Invoke([]string{"ps"})
		ctx.Logf("proxy status: %v", s)
	}
}
