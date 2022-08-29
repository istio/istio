//go:build integ
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

package uproxy

import (
	"context"
	"strings"
	"testing"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/ambient"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/scopes"
)

var (
	i istio.Instance

	// Below are various preconfigured echo deployments. Whenever possible, tests should utilize these
	// to avoid excessive creation/tear down of deployments. In general, a test should only deploy echo if
	// its doing something unique to that specific test.
	apps = &EchoDeployments{}

	// used to validate telemetry in-cluster
	prom prometheus.Instance
)

type EchoDeployments struct {
	// Namespace echo apps will be deployed
	Namespace         namespace.Instance
	Remote            echo.Instances
	AltRemote         echo.Instances
	Captured          echo.Instances
	Uncaptured        echo.Instances
	SidecarRemote     echo.Instances
	SidecarCaptured   echo.Instances
	SidecarUncaptured echo.Instances
	All               echo.Instances
	Mesh              echo.Instances
	MeshExternal      echo.Instances

	RemotePEP ambient.PEP
}

var ControlPlaneValues = `
profile: ambient
values:
  meshConfig:
    ambientMesh:
      mode: "DEFAULT"
    defaultConfig:
      proxyMetadata:
        ISTIO_META_DNS_CAPTURE: "true"
        DNS_PROXY_ADDR: "0.0.0.0:15053"
    accessLogFile: /dev/stdout`

// TestMain defines the entrypoint for pilot tests using a standard Istio installation.
// If a test requires a custom install it should go into its own package, otherwise it should go
// here to reuse a single install across tests.
func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			cfg.DeployEastWestGW = false
			cfg.ControlPlaneValues = ControlPlaneValues
		})).
		Setup(func(t resource.Context) error {
			return SetupApps(t, i, apps)
		}).
		Run()
}

const (
	Remote            = "remote"
	AltRemote         = "alt-remote"
	Captured          = "captured"
	Uncaptured        = "uncaptured"
	SidecarRemote     = "sidecar-remote"
	SidecarCaptured   = "sidecar-captured"
	SidecarUncaptured = "sidecar-uncaptured"
)

var inMesh = match.Matcher(func(instance echo.Instance) bool {
	names := []string{"remote", "captured", "sidecar"}
	for _, name := range names {
		if strings.Contains(instance.Config().Service, name) {
			return true
		}
	}
	return false
})

func SetupApps(t resource.Context, i istio.Instance, apps *EchoDeployments) error {
	var err error
	apps.Namespace, err = namespace.New(t, namespace.Config{
		Prefix: "echo",
		Inject: false,
		Labels: map[string]string{
			"istio.io/dataplane-mode": "ambient",
		},
	})
	if err != nil {
		return err
	}

	prom, err = prometheus.New(t, prometheus.Config{})
	if err != nil {
		return err
	}

	// Headless services don't work with targetPort, set to same port
	headlessPorts := make([]echo.Port, len(ports.All()))
	for i, p := range ports.All() {
		p.ServicePort = p.WorkloadPort
		headlessPorts[i] = p
	}
	builder := deployment.New(t).
		WithClusters(t.Clusters()...).
		WithConfig(echo.Config{
			Service:        Remote,
			Namespace:      apps.Namespace,
			Ports:          ports.All(),
			ServiceAccount: true,
			Subsets: []echo.SubsetConfig{
				{
					Replicas: 1,
					Version:  "v1",
					Labels: map[string]string{
						// TODO: remove or keep https://github.com/solo-io/istio-sidecarless/issues/168
						"asm-type":   "workload",
						"asm-remote": "true",
						"app":        "remote",
						"version":    "v1",
					},
				},
				{
					Replicas: 1,
					Version:  "v2",
					Labels: map[string]string{
						"asm-type":   "workload",
						"asm-remote": "true",
						"app":        "remote",
						"version":    "v2",
					},
				},
			},
		}).
		//WithConfig(echo.Config{
		//	Service:        AltRemote,
		//	Namespace:      apps.Namespace,
		//	Ports:          ports.All(),
		//	ServiceAccount: true,
		//	Subsets: []echo.SubsetConfig{{
		//		Replicas: 2,
		//		Labels: map[string]string{
		//			"asm-type":   "workload",
		//			"asm-remote": "true",
		//		},
		//	}},
		//}).
		WithConfig(echo.Config{
			Service:        Captured,
			Namespace:      apps.Namespace,
			Ports:          ports.All(),
			ServiceAccount: true,
			Subsets: []echo.SubsetConfig{
				{
					Replicas: 1,
					Version:  "v1",
					Labels: map[string]string{
						"asm-type":     "workload",
						"ambient-type": "workload",
					},
				},
				{
					Replicas: 1,
					Version:  "v2",
					Labels: map[string]string{
						"asm-type":     "workload",
						"ambient-type": "workload",
					},
				},
			},
		}).
		WithConfig(echo.Config{
			Service:        Uncaptured,
			Namespace:      apps.Namespace,
			Ports:          ports.All(),
			ServiceAccount: true,
			Subsets: []echo.SubsetConfig{
				{
					Replicas: 1,
					Version:  "v1",
					Labels: map[string]string{
						"asm-type":     "none",
						"ambient-type": "none",
					},
				},
				{
					Replicas: 1,
					Version:  "v2",
					Labels: map[string]string{
						"asm-type":     "none",
						"ambient-type": "none",
					},
				},
			},
		})

	if err := t.ConfigIstio().YAML(apps.Namespace.Name(), `apiVersion: gateway.networking.k8s.io/v1alpha2
kind: Gateway
metadata:
  name: remote
  annotations:
    istio.io/service-account: remote
spec:
  gatewayClassName: istio-mesh`).Apply(apply.NoCleanup); err != nil {
		return err
	}

	_, whErr := t.Clusters().Default().
		Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().
		Get(context.Background(), "istio-sidecar-injector", metav1.GetOptions{})
	if whErr != nil && !kerrors.IsNotFound(whErr) {
		return whErr
	}
	// Only setup sidecar tests if webhook is installed
	if whErr == nil {
		// TODO(https://github.com/solo-io/istio-sidecarless/issues/154) support sidecars that are captured
		//builder = builder.WithConfig(echo.Config{
		//	Service:   SidecarRemote,
		//	Namespace: apps.Namespace,
		//	Ports:     ports.All(),
		//	Subsets: []echo.SubsetConfig{
		//		{
		//			Replicas: 1,
		//			Version:  "v1",
		//			Labels: map[string]string{
		//				"asm-type":                "workload",
		//				"ambient-type":            "workload",
		//				"asm-remote":              "true",
		//				"sidecar.istio.io/inject": "true",
		//			},
		//		},
		//		{
		//			Replicas: 1,
		//			Version:  "v2",
		//			Labels: map[string]string{
		//				"asm-type":                "workload",
		//				"ambient-type":            "workload",
		//				"asm-remote":              "true",
		//				"sidecar.istio.io/inject": "true",
		//			},
		//		},
		//	},
		//})
		//	builder = builder.WithConfig(echo.Config{
		//		Service:   SidecarCaptured,
		//		Namespace: apps.Namespace,
		//		Ports:     ports.All(),
		//		Subsets: []echo.SubsetConfig{
		//			{
		//				Replicas: 1,
		//				Version:  "v1",
		//				Labels: map[string]string{
		//					"asm-type":                "workload",
		//					"ambient-type":            "workload",
		//					"sidecar.istio.io/inject": "true",
		//				},
		//			},
		//			{
		//				Replicas: 1,
		//				Version:  "v2",
		//				Labels: map[string]string{
		//					"asm-type":                "workload",
		//					"ambient-type":            "workload",
		//					"sidecar.istio.io/inject": "true",
		//				},
		//			},
		//		},
		//	})
		builder = builder.WithConfig(echo.Config{
			Service:        SidecarUncaptured,
			Namespace:      apps.Namespace,
			Ports:          ports.All(),
			ServiceAccount: true,
			Subsets: []echo.SubsetConfig{
				{
					Replicas: 1,
					Version:  "v1",
					Labels: map[string]string{
						"asm-type":                "none",
						"ambient-type":            "none",
						"sidecar.istio.io/inject": "true",
					},
				},
				{
					Replicas: 1,
					Version:  "v2",
					Labels: map[string]string{
						"asm-type":                "none",
						"ambient-type":            "none",
						"sidecar.istio.io/inject": "true",
					},
				},
			},
		})
	}

	echos, err := builder.Build()
	if err != nil {
		return err
	}
	for _, b := range echos {
		scopes.Framework.Infof("built %v", b.Config().Service)
	}
	apps.All = echos
	apps.Remote = match.ServiceName(echo.NamespacedName{Name: Remote, Namespace: apps.Namespace}).GetMatches(echos)
	apps.Uncaptured = match.ServiceName(echo.NamespacedName{Name: Uncaptured, Namespace: apps.Namespace}).GetMatches(echos)
	apps.Captured = match.ServiceName(echo.NamespacedName{Name: Captured, Namespace: apps.Namespace}).GetMatches(echos)
	apps.SidecarRemote = match.ServiceName(echo.NamespacedName{Name: SidecarRemote, Namespace: apps.Namespace}).GetMatches(echos)
	apps.SidecarUncaptured = match.ServiceName(echo.NamespacedName{Name: SidecarUncaptured, Namespace: apps.Namespace}).GetMatches(echos)
	apps.SidecarCaptured = match.ServiceName(echo.NamespacedName{Name: SidecarCaptured, Namespace: apps.Namespace}).GetMatches(echos)
	apps.Mesh = inMesh.GetMatches(echos)
	apps.MeshExternal = match.Not(inMesh).GetMatches(echos)

	apps.RemotePEP, err = ambient.NewPEP(t, apps.Namespace, apps.Remote.ServiceName())
	if err != nil {
		return err
	}
	return nil
}
