//go:build integ

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
	"fmt"
	"os"
	"path"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

const (
	ASvc             = "a"
	BSvc             = "b"
	CSvc             = "c"
	DSvc             = "d"
	ESvc             = "e"
	MultiversionSvc  = "multiversion"
	VMSvc            = "vm"
	HeadlessSvc      = "headless"
	NakedSvc         = "naked"
	HeadlessNakedSvc = "headless-naked"
	ExternalSvc      = "external"
)

type EchoDeployments struct {
	// TODO: Consolidate the echo config and reduce/reuse echo instances (https://github.com/istio/istio/issues/28599)
	// Namespace1 is used as the default namespace for reachability tests and other tests which can reuse the same config for echo instances
	Namespace1 namespace.Instance
	// Namespace2 is used by most authorization test cases within authorization_test.go
	Namespace2 namespace.Instance
	// Namespace3 is used by TestAuthorization_Conditions and there is one C echo instance deployed
	Namespace3    namespace.Instance
	A, B, C, D, E echo.Instances
	Multiversion  echo.Instances
	Headless      echo.Instances
	Naked         echo.Instances
	VM            echo.Instances
	HeadlessNaked echo.Instances
	All           echo.Instances
	External      echo.Instances
}

func EchoConfig(name string, headless bool, annos map[string]string) echo.Config {
	out := echo.Config{
		Service:        name,
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
				WorkloadPort: 8090,
				ServicePort:  8095,
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
				WorkloadPort: 8443,
				TLS:          true,
			},
			{
				Name:         "http-8091",
				Protocol:     protocol.HTTP,
				WorkloadPort: 8091,
			},
			{
				Name:         "http-8092",
				Protocol:     protocol.HTTP,
				WorkloadPort: 8092,
			},
			{
				Name:         "tcp-8093",
				Protocol:     protocol.TCP,
				WorkloadPort: 8093,
			},
			{
				Name:         "tcp-8094",
				Protocol:     protocol.TCP,
				WorkloadPort: 8094,
			},
			// Workload Ports needed by TestPassThroughFilterChain
			// The port 8084-8089 will be defined only in the workload and not in the k8s service.
			{
				Name:         "tcp-8085",
				ServicePort:  echo.NoServicePort,
				WorkloadPort: 8085,
				Protocol:     protocol.HTTP,
			},
			{
				Name:         "tcp-8086",
				ServicePort:  echo.NoServicePort,
				WorkloadPort: 8086,
				Protocol:     protocol.HTTP,
			},
			{
				Name:         "tcp-8087",
				ServicePort:  echo.NoServicePort,
				WorkloadPort: 8087,
				Protocol:     protocol.TCP,
			},
			{
				Name:         "tcp-8088",
				ServicePort:  echo.NoServicePort,
				WorkloadPort: 8088,
				Protocol:     protocol.TCP,
			},
			{
				Name:         "tcp-8089",
				ServicePort:  echo.NoServicePort,
				WorkloadPort: 8089,
				Protocol:     protocol.HTTPS,
				TLS:          true,
			},
			{
				Name:         "tcp-8084",
				ServicePort:  echo.NoServicePort,
				WorkloadPort: 8084,
				Protocol:     protocol.HTTPS,
				TLS:          true,
			},
		},
	}

	// for headless service with selector, the port and target port must be equal
	// Ref: https://kubernetes.io/docs/concepts/services-networking/service/#headless-services
	if headless {
		for i := range out.Ports {
			out.Ports[i].ServicePort = out.Ports[i].WorkloadPort
		}
	}
	return out
}

func mustReadCert(f string) string {
	b, err := os.ReadFile(path.Join(env.IstioSrc, "tests/testdata/certs/dns", f))
	if err != nil {
		panic(fmt.Sprintf("failed to read %v: %v", f, err))
	}
	return string(b)
}

func SetupApps(ctx resource.Context, customCfg *[]echo.Config, buildVM bool) error {
	if ctx.Settings().Skip(echo.VM) {
		buildVM = false
	}

	var customConfig []echo.Config
	a := EchoConfig(ASvc, false, nil)
	b := EchoConfig(BSvc, false, nil)
	c := EchoConfig(CSvc, false, nil)
	d := EchoConfig(DSvc, false, nil)
	e := EchoConfig(ESvc, false, nil)
	multiversionCfg := func() echo.Config {
		// Multi-version specific setup
		multiVersionCfg := EchoConfig(MultiversionSvc, false, nil)
		multiVersionCfg.Subsets = []echo.SubsetConfig{
			// Istio deployment, with sidecar.
			{
				Version: "vistio",
			},
			// Legacy deployment subset, does not have sidecar injected.
			{
				Version:     "vlegacy",
				Annotations: map[string]string{annotation.SidecarInject.Name: "false"},
			},
		}
		return multiVersionCfg
	}()

	nakedSvc := EchoConfig(NakedSvc, false, map[string]string{annotation.SidecarInject.Name: "false"})

	vmCfg := func() echo.Config {
		// VM specific setup
		vmCfg := EchoConfig(VMSvc, false, nil)
		// for test cases that have `buildVM` off, vm will function like a regular pod
		vmCfg.DeployAsVM = buildVM
		return vmCfg
	}()

	externalSvc := echo.Config{
		Service: ExternalSvc,
		// Namespace: appsNamespace,
		Ports: []echo.Port{
			{
				// Plain HTTP port only used to route request to egress gateway
				Name:         "http",
				Protocol:     protocol.HTTP,
				ServicePort:  80,
				WorkloadPort: 8080,
			},
			{
				// HTTPS port
				Name:         "https",
				Protocol:     protocol.HTTPS,
				ServicePort:  443,
				WorkloadPort: 8443,
				TLS:          true,
			},
		},
		// Set up TLS certs on the server. This will make the server listen with these credentials.
		TLSSettings: &common.TLSSettings{
			// Echo has these test certs baked into the docker image
			RootCert:   mustReadCert("root-cert.pem"),
			ClientCert: mustReadCert("cert-chain.pem"),
			Key:        mustReadCert("key.pem"),
			// Override hostname to match the SAN in the cert we are using
			Hostname: "server.default.svc",
		},
		Subsets: []echo.SubsetConfig{{
			Version:     "v1",
			Annotations: map[string]string{annotation.SidecarInject.Name: "false"},
		}},
	}

	headlessSvc := EchoConfig(HeadlessSvc, true, nil)
	headlessNakedSvc := EchoConfig(HeadlessNakedSvc, true, map[string]string{annotation.SidecarInject.Name: "false"})

	customConfig = append(customConfig, a, b, c, d, e, multiversionCfg, nakedSvc, vmCfg, externalSvc, headlessSvc, headlessNakedSvc)
	*customCfg = customConfig
	return nil
}

// IsMultiversion matches instances that have Multi-version specific setup.
var IsMultiversion match.Matcher = func(i echo.Instance) bool {
	if len(i.Config().Subsets) != 2 {
		return false
	}
	var matchIstio, matchLegacy bool
	for _, s := range i.Config().Subsets {
		if s.Version == "vistio" {
			matchIstio = true
		} else if s.Version == "vlegacy" {
			if val, ok := s.Annotations[annotation.SidecarInject.Name]; ok && val == "false" {
				matchLegacy = true
			}
		}
	}
	return matchIstio && matchLegacy
}

var IsNotMultiversion = match.Not(IsMultiversion)

// SourceMatcher matches workload pod A with sidecar injected and VM
func SourceMatcher(ns namespace.Instance, skipVM bool) match.Matcher {
	m := match.ServiceName(echo.NamespacedName{
		Name:      ASvc,
		Namespace: ns,
	})

	if !skipVM {
		m = match.Or(m, match.ServiceName(echo.NamespacedName{
			Name:      VMSvc,
			Namespace: ns,
		}))
	}

	return m
}

// DestMatcher matches workload pod B with sidecar injected and VM
func DestMatcher(ns namespace.Instance, skipVM bool) match.Matcher {
	m := match.ServiceName(echo.NamespacedName{
		Name:      BSvc,
		Namespace: ns,
	})

	if !skipVM {
		m = match.Or(m, match.ServiceName(echo.NamespacedName{
			Name:      VMSvc,
			Namespace: ns,
		}))
	}

	return m
}
