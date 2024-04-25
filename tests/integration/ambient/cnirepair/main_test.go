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

package cnirepair

import (
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/ambient"
	"istio.io/istio/pkg/test/framework/components/echo"
	common_deploy "istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/prometheus"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/tests/integration/pilot/common"
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
	Namespace namespace.Instance

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
	// SidecarWaypoint is a sidecar with a waypoint
	SidecarWaypoint echo.Instances
	// SidecarCaptured echo services with sidecar and ambient capture
	SidecarCaptured echo.Instances
	// SidecarUncaptured echo services with sidecar and no ambient capture
	SidecarUncaptured echo.Instances

	// All echo services
	All echo.Instances
	// Echo services that are in the mesh
	Mesh echo.Instances
	// Echo services that are not in mesh
	MeshExternal echo.Instances

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
  cni:
    repair:
      enabled: true
  ztunnel:
    terminationGracePeriodSeconds: 5
    env:
      SECRET_TTL: 5m
`
		}, cert.CreateCASecretAlt)).
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

	builder := deployment.New(t).
		WithClusters(t.Clusters()...).
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
		}).
		WithConfig(echo.Config{
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

	// Build the applications
	echos, err := builder.Build()
	fmt.Println(echos)
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

	return nil
}

func TestTrafficWithCNIRepair(t *testing.T) {
	framework.NewTest(t).
		TopLevel().
		Run(func(t framework.TestContext) {
			apps := common_deploy.NewOrFail(t, t, common_deploy.Config{
				NoExternalNamespace: true,
				IncludeExtAuthz:     false,
			})
			common.RunAllTrafficTests(t, i, apps.SingleNamespaceView())
		})
}
