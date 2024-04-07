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

package ambient

import (
	"context"
	"strings"
	"testing"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/ambient"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/tests/integration/security/util/cert"
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
	Namespace                 namespace.Instance
	AllWaypoint               echo.Instances
	WorkloadAddressedWaypoint echo.Instances
	ServiceAddressedWaypoint  echo.Instances
	Captured                  echo.Instances
	Uncaptured                echo.Instances
	SidecarWaypoint           echo.Instances
	SidecarCaptured           echo.Instances
	SidecarUncaptured         echo.Instances
	All                       echo.Instances
	Mesh                      echo.Instances
	MeshExternal              echo.Instances

	WaypointProxies map[string]ambient.WaypointProxy
}

// TestMain defines the entrypoint for pilot tests using a standard Istio installation.
// If a test requires a custom install it should go into its own package, otherwise it should go
// here to reuse a single install across tests.
func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		RequireMinVersion(24).
		SkipIf("https://github.com/istio/istio/issues/43243", func(ctx resource.Context) bool {
			return strings.Contains(ctx.Settings().Image.Tag, "distroless")
		}).
		Label(label.IPv4). // https://github.com/istio/istio/issues/41008
		Setup(func(t resource.Context) error {
			t.Settings().Ambient = true
			return nil
		}).
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			// can't deploy VMs without eastwest gateway
			ctx.Settings().SkipVMs()
			cfg.EnableCNI = true
			cfg.DeployEastWestGW = false
			cfg.ControlPlaneValues = `
values:
  ztunnel:
    terminationGracePeriodSeconds: 5
    env:
      SECRET_TTL: 5m
`
		}, cert.CreateCASecretAlt)).
		Setup(func(t resource.Context) error {
			gatewayConformanceInputs.Client = t.Clusters().Default()
			gatewayConformanceInputs.Cleanup = !t.Settings().NoCleanup

			return nil
		}).
		Setup(func(t resource.Context) error {
			return SetupApps(t, i, apps)
		}).
		Run()
}

const (
	WorkloadAddressedWaypoint = "workload-addressed-waypoint"
	ServiceAddressedWaypoint  = "service-addressed-waypoint"
	Captured                  = "captured"
	Uncaptured                = "uncaptured"
	SidecarWaypoint           = "sidecar-waypoint"
	SidecarCaptured           = "sidecar-captured"
	SidecarUncaptured         = "sidecar-uncaptured"
)

var inMesh = match.Matcher(func(instance echo.Instance) bool {
	names := []string{"waypoint", "captured", "sidecar"}
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
			constants.DataplaneMode: "ambient",
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
			Service:               WorkloadAddressedWaypoint,
			Namespace:             apps.Namespace,
			Ports:                 ports.All(),
			ServiceAccount:        true,
			WorkloadWaypointProxy: "waypoint",
			Subsets: []echo.SubsetConfig{
				{
					Replicas: 1,
					Version:  "v1",
					Labels: map[string]string{
						"app":     WorkloadAddressedWaypoint,
						"version": "v1",
					},
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.AmbientUseWaypoint: {
							Value: "waypoint",
						},
					},
				},
				{
					Replicas: 1,
					Version:  "v2",
					Labels: map[string]string{
						"app":     WorkloadAddressedWaypoint,
						"version": "v2",
					},
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.AmbientUseWaypoint: {
							Value: "waypoint",
						},
					},
				},
			},
		}).
		WithConfig(echo.Config{
			Service:              ServiceAddressedWaypoint,
			Namespace:            apps.Namespace,
			Ports:                ports.All(),
			ServiceAnnotations:   echo.NewAnnotations().Set(echo.AmbientUseWaypoint, "waypoint"),
			ServiceAccount:       true,
			ServiceWaypointProxy: "waypoint",
			Subsets: []echo.SubsetConfig{
				{
					Replicas: 1,
					Version:  "v1",
					Labels: map[string]string{
						"app":     ServiceAddressedWaypoint,
						"version": "v1",
					},
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.AmbientUseWaypoint: {
							Value: "waypoint",
						},
					},
				},
				{
					Replicas: 1,
					Version:  "v2",
					Labels: map[string]string{
						"app":     ServiceAddressedWaypoint,
						"version": "v2",
					},
					Annotations: map[echo.Annotation]*echo.AnnotationValue{
						echo.AmbientUseWaypoint: {
							Value: "waypoint",
						},
					},
				},
			},
		}).
		WithConfig(echo.Config{
			Service:        Captured,
			Namespace:      apps.Namespace,
			Ports:          ports.All(),
			ServiceAccount: true,
			Subsets: []echo.SubsetConfig{
				{
					Replicas: 1,
					Version:  "v1",
				},
				{
					Replicas: 1,
					Version:  "v2",
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
					Replicas:    1,
					Version:     "v1",
					Annotations: echo.NewAnnotations().Set(echo.AmbientType, constants.AmbientRedirectionDisabled),
				},
				{
					Replicas:    1,
					Version:     "v2",
					Annotations: echo.NewAnnotations().Set(echo.AmbientType, constants.AmbientRedirectionDisabled),
				},
			},
		})

	_, whErr := t.Clusters().Default().
		Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().
		Get(context.Background(), "istio-sidecar-injector", metav1.GetOptions{})
	if whErr != nil && !kerrors.IsNotFound(whErr) {
		return whErr
	}
	// Only setup sidecar tests if webhook is installed
	if whErr == nil {
		// TODO(https://github.com/istio/istio/issues/43244) support sidecars that are captured
		//builder = builder.WithConfig(echo.Config{
		//	Service:   SidecarWaypoint,
		//	Namespace: apps.Namespace,
		//	Ports:     ports.All(),
		//	Subsets: []echo.SubsetConfig{
		//		{
		//			Replicas: 1,
		//			Version:  "v1",
		//			Labels: map[string]string{
		//				"ambient-type":            "workload",
		//				"sidecar.istio.io/inject": "true",
		//			},
		//		},
		//		{
		//			Replicas: 1,
		//			Version:  "v2",
		//			Labels: map[string]string{
		//				"ambient-type":            "workload",
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
		//					"ambient-type":            "workload",
		//					"sidecar.istio.io/inject": "true",
		//				},
		//			},
		//			{
		//				Replicas: 1,
		//				Version:  "v2",
		//				Labels: map[string]string{
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
					Replicas:    1,
					Version:     "v1",
					Annotations: echo.NewAnnotations().Set(echo.AmbientType, constants.AmbientRedirectionDisabled),
					Labels: map[string]string{
						"sidecar.istio.io/inject": "true",
					},
				},
				{
					Replicas:    1,
					Version:     "v2",
					Annotations: echo.NewAnnotations().Set(echo.AmbientType, constants.AmbientRedirectionDisabled),
					Labels: map[string]string{
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
	apps.WorkloadAddressedWaypoint = match.ServiceName(echo.NamespacedName{Name: WorkloadAddressedWaypoint, Namespace: apps.Namespace}).GetMatches(echos)
	apps.ServiceAddressedWaypoint = match.ServiceName(echo.NamespacedName{Name: ServiceAddressedWaypoint, Namespace: apps.Namespace}).GetMatches(echos)
	apps.AllWaypoint = apps.AllWaypoint.Append(apps.WorkloadAddressedWaypoint)
	apps.AllWaypoint = apps.AllWaypoint.Append(apps.ServiceAddressedWaypoint)
	apps.Uncaptured = match.ServiceName(echo.NamespacedName{Name: Uncaptured, Namespace: apps.Namespace}).GetMatches(echos)
	apps.Captured = match.ServiceName(echo.NamespacedName{Name: Captured, Namespace: apps.Namespace}).GetMatches(echos)
	apps.SidecarWaypoint = match.ServiceName(echo.NamespacedName{Name: SidecarWaypoint, Namespace: apps.Namespace}).GetMatches(echos)
	apps.SidecarUncaptured = match.ServiceName(echo.NamespacedName{Name: SidecarUncaptured, Namespace: apps.Namespace}).GetMatches(echos)
	apps.SidecarCaptured = match.ServiceName(echo.NamespacedName{Name: SidecarCaptured, Namespace: apps.Namespace}).GetMatches(echos)
	apps.Mesh = inMesh.GetMatches(echos)
	apps.MeshExternal = match.Not(inMesh).GetMatches(echos)

	if apps.WaypointProxies == nil {
		apps.WaypointProxies = make(map[string]ambient.WaypointProxy)
	}

	for _, echo := range echos {
		svcwp := echo.Config().ServiceWaypointProxy
		wlwp := echo.Config().WorkloadWaypointProxy
		if svcwp != "" {
			if _, found := apps.WaypointProxies[svcwp]; !found {
				apps.WaypointProxies[svcwp], err = ambient.NewWaypointProxy(t, apps.Namespace, svcwp)
				if err != nil {
					return err
				}
			}
		}
		if wlwp != "" {
			if _, found := apps.WaypointProxies[wlwp]; !found {
				apps.WaypointProxies[wlwp], err = ambient.NewWaypointProxy(t, apps.Namespace, wlwp)
				if err != nil {
					return err
				}
			}
		}

	}

	return nil
}
