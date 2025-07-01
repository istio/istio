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
	"testing"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/ambient"
	"istio.io/istio/pkg/test/framework/components/echo"
	cdeployment "istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
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

const (
	ambientControlPlaneValues = `
values:
  cni:
    # The CNI repair feature is disabled for these tests because this is a controlled environment,
    # and it is important to catch issues that might otherwise be automatically fixed.
    # Refer to issue #49207 for more context.
    repair:
      enabled: false
  ztunnel:
    terminationGracePeriodSeconds: 5
    env:
      SECRET_TTL: 5m
`

	ambientMultiNetworkControlPlaneValues = `
values:
  pilot:
    env:
      AMBIENT_ENABLE_MULTI_NETWORK: "true"
  cni:
    # The CNI repair feature is disabled for these tests because this is a controlled environment,
    # and it is important to catch issues that might otherwise be automatically fixed.
    # Refer to issue #49207 for more context.
    repair:
      enabled: false
  ztunnel:
    terminationGracePeriodSeconds: 5
    env:
      SECRET_TTL: 5m
  meshConfig:
    serviceScopeConfigs:
      - servicesSelector:
          matchExpressions:
            - key: istio.io/global
              operator: Exists
        scope: GLOBAL
`
)

type EchoDeployments struct {
	// Namespace echo apps will be deployed
	Namespace         namespace.Instance
	ExternalNamespace namespace.Instance

	// AllWaypoint is a waypoint for all types
	AllWaypoint echo.Instances
	// WorkloadAddressedWaypoint is a workload only waypoint
	WorkloadAddressedWaypoint echo.Instances
	// ServiceAddressedWaypoint is a serviceonly waypoint
	ServiceAddressedWaypoint echo.Instances
	// Captured echo service
	Captured echo.Instances
	// Uncaptured echo Service
	Uncaptured echo.Instances
	// Sidecar echo services with sidecar
	Sidecar echo.Instances
	// Globally scoped echo service
	Global echo.Instances
	// Locally scoped echo service
	Local echo.Instances

	// All echo services
	All echo.Instances
	// Echo services that are in the mesh
	Mesh echo.Instances
	// Echo services that are not in mesh
	MeshExternal echo.Instances

	MockExternal echo.Instances

	// WaypointProxies by
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
		Setup(func(t resource.Context) error {
			t.Settings().Ambient = true
			return nil
		}).
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			// can't deploy VMs without eastwest gateway
			ctx.Settings().SkipVMs()
			cfg.EnableCNI = true
			cfg.DeployEastWestGW = false
			cfg.ControlPlaneValues = ambientControlPlaneValues
			if ctx.Settings().AmbientMultiNetwork {
				cfg.DeployEastWestGW = true
				cfg.DeployGatewayAPI = true
				cfg.ControlPlaneValues = ambientMultiNetworkControlPlaneValues
				// TODO: Remove once we're actually ready to test the multi-cluster
				// features
				cfg.SkipDeployCrossClusterSecrets = true
			}
		}, cert.CreateCASecretAlt)).
		Setup(func(t resource.Context) error {
			gatewayConformanceInputs.Cluster = t.Clusters().Default()
			gatewayConformanceInputs.Client = t.Clusters().Default()
			gatewayConformanceInputs.Cleanup = !t.Settings().NoCleanup

			return nil
		}).
		SetupParallel(
			testRegistrySetup,
			func(t resource.Context) error {
				return SetupApps(t, i, apps)
			},
			func(t resource.Context) (err error) {
				prom, err = prometheus.New(t, prometheus.Config{})
				if err != nil {
					return err
				}
				return
			},
		).
		Run()
}

const (
	WorkloadAddressedWaypoint = "workload-addressed-waypoint"
	ServiceAddressedWaypoint  = "service-addressed-waypoint"
	Captured                  = "captured"
	Uncaptured                = "uncaptured"
	Sidecar                   = "sidecar"
	Global                    = "global"
	Local                     = "local"
	EastWestGateway           = "eastwest-gateway"
)

var inMesh = match.Matcher(func(instance echo.Instance) bool {
	return instance.Config().HasProxyCapabilities()
})

func SetupApps(t resource.Context, i istio.Instance, apps *EchoDeployments) error {
	var err error
	apps.Namespace, err = namespace.New(t, namespace.Config{
		Prefix: "echo",
		Inject: false,
		Labels: map[string]string{
			label.IoIstioDataplaneMode.Name: "ambient",
		},
	})
	if err != nil {
		return err
	}
	apps.ExternalNamespace, err = namespace.New(t, namespace.Config{
		Prefix: "external",
		Inject: false,
		Labels: map[string]string{
			"istio.io/test-exclude-namespace": "true",
		},
	})
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
						"app":                         WorkloadAddressedWaypoint,
						"version":                     "v1",
						label.IoIstioUseWaypoint.Name: "waypoint",
					},
				},
				{
					Replicas: 1,
					Version:  "v2",
					Labels: map[string]string{
						"app":                         WorkloadAddressedWaypoint,
						"version":                     "v2",
						label.IoIstioUseWaypoint.Name: "waypoint",
					},
				},
			},
		}).
		WithConfig(echo.Config{
			Service:              ServiceAddressedWaypoint,
			Namespace:            apps.Namespace,
			Ports:                ports.All(),
			ServiceLabels:        map[string]string{label.IoIstioUseWaypoint.Name: "waypoint"},
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
				},
				{
					Replicas: 1,
					Version:  "v2",
					Labels: map[string]string{
						"app":     ServiceAddressedWaypoint,
						"version": "v2",
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
					Replicas: 1,
					Version:  "v1",
					Labels:   map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeNone},
				},
				{
					Replicas: 1,
					Version:  "v2",
					Labels:   map[string]string{label.IoIstioDataplaneMode.Name: constants.DataplaneModeNone},
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
		builder = builder.WithConfig(echo.Config{
			Service:        Sidecar,
			Namespace:      apps.Namespace,
			Ports:          ports.All(),
			ServiceAccount: true,
			Subsets: []echo.SubsetConfig{
				{
					Replicas: 1,
					Version:  "v1",
					Labels: map[string]string{
						"sidecar.istio.io/inject":       "true",
						label.IoIstioDataplaneMode.Name: constants.DataplaneModeNone,
					},
				},
				{
					Replicas: 1,
					Version:  "v2",
					Labels: map[string]string{
						"sidecar.istio.io/inject":       "true",
						label.IoIstioDataplaneMode.Name: constants.DataplaneModeNone,
					},
				},
			},
		})
	}

	// Only deploy local and global apps if ambient multi-network is enabled
	if t.Settings().AmbientMultiNetwork {
		builder = builder.WithConfig(echo.Config{
			Service:        Global,
			ServiceLabels:  map[string]string{"istio.io/global": "true"},
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
		}).WithConfig(echo.Config{
			Service:        Local,
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
		})
	}

	external := cdeployment.External{Namespace: apps.ExternalNamespace}
	external.Build(t, builder)

	echos, err := builder.Build()
	if err != nil {
		return err
	}
	for _, b := range echos {
		scopes.Framework.Infof("built %v", b.Config().Service)
	}

	external.LoadValues(echos)
	apps.MockExternal = external.All

	// All does not include external
	echos = match.Not(match.ServiceName(echo.NamespacedName{Name: cdeployment.ExternalSvc, Namespace: apps.ExternalNamespace})).GetMatches(echos)
	apps.All = echos
	apps.WorkloadAddressedWaypoint = match.ServiceName(echo.NamespacedName{Name: WorkloadAddressedWaypoint, Namespace: apps.Namespace}).GetMatches(echos)
	apps.ServiceAddressedWaypoint = match.ServiceName(echo.NamespacedName{Name: ServiceAddressedWaypoint, Namespace: apps.Namespace}).GetMatches(echos)
	apps.AllWaypoint = apps.AllWaypoint.Append(apps.WorkloadAddressedWaypoint)
	apps.AllWaypoint = apps.AllWaypoint.Append(apps.ServiceAddressedWaypoint)
	apps.Uncaptured = match.ServiceName(echo.NamespacedName{Name: Uncaptured, Namespace: apps.Namespace}).GetMatches(echos)
	apps.Captured = match.ServiceName(echo.NamespacedName{Name: Captured, Namespace: apps.Namespace}).GetMatches(echos)
	apps.Sidecar = match.ServiceName(echo.NamespacedName{Name: Sidecar, Namespace: apps.Namespace}).GetMatches(echos)
	apps.Global = match.ServiceName(echo.NamespacedName{Name: Global, Namespace: apps.Namespace}).GetMatches(echos)
	apps.Local = match.ServiceName(echo.NamespacedName{Name: Local, Namespace: apps.Namespace}).GetMatches(echos)
	apps.Mesh = inMesh.GetMatches(echos)
	apps.MeshExternal = match.Not(inMesh).GetMatches(echos)

	if err := cdeployment.DeployExternalServiceEntry(t.ConfigIstio(), apps.Namespace, apps.ExternalNamespace, false).
		Apply(apply.CleanupConditionally); err != nil {
		return err
	}

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
