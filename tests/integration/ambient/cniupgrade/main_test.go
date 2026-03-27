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

package cniupgrade

import (
	"fmt"
	"testing"

	"istio.io/api/label"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	common_deploy "istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	testlabel "istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/shell"
	util "istio.io/istio/tests/integration/ambient"
	"istio.io/istio/tests/integration/pilot/common"
	"istio.io/istio/tests/integration/security/util/cert"
)

var (
	i istio.Instance

	// Below are various preconfigured echo deployments. Whenever possible, tests should utilize these
	// to avoid excessive creation/tear down of deployments. In general, a test should only deploy echo if
	// its doing something unique to that specific test.
	apps = &EchoDeployments{}
)

type EchoDeployments struct {
	// Namespace echo apps will be deployed
	Namespace namespace.Instance
	// Captured echo service
	Captured echo.Instances
	// Uncaptured echo Service
	Uncaptured echo.Instances

	// All echo services
	All echo.Instances
}

// TestMain defines the entrypoint for pilot tests using a standard Istio installation.
// If a test requires a custom install it should go into its own package, otherwise it should go
// here to reuse a single install across tests.
func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		RequireMinVersion(24).
		RequireSingleCluster().
		Label(testlabel.IPv4). // https://github.com/istio/istio/issues/41008
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
			if ctx.Settings().NativeNftables {
				cfg.ControlPlaneValues += `
  global:
    nativeNftables: true
`
			}
		}, cert.CreateCASecretAlt)).
		Setup(func(t resource.Context) error {
			return SetupApps(t, i, apps)
		}).
		Run()
}

const (
	Captured   = "captured"
	Uncaptured = "uncaptured"
)

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

	// Build the applications
	echos, err := builder.Build()
	if err != nil {
		return err
	}
	for _, b := range echos {
		scopes.Framework.Infof("built %v", b.Config().Service)
	}

	apps.All = echos
	apps.Uncaptured = match.ServiceName(echo.NamespacedName{Name: Uncaptured, Namespace: apps.Namespace}).GetMatches(echos)
	apps.Captured = match.ServiceName(echo.NamespacedName{Name: Captured, Namespace: apps.Namespace}).GetMatches(echos)

	return nil
}

func TestTrafficWithCNIUpgrade(t *testing.T) {
	framework.NewTest(t).
		TopLevel().
		Run(func(t framework.TestContext) {
			apps := common_deploy.NewOrFail(t, common_deploy.Config{
				NoExternalNamespace: true,
				IncludeExtAuthz:     false,
			})

			c := t.Clusters().Default()
			ns := apps.SingleNamespaceView().EchoNamespace.Namespace

			// We need to simulate a Daemonset that exists, but has zero pods backing it.
			//
			// This is to simulate the backing pods terminating while the DS remains
			// (as would be the case for a short period during upgrade)
			// This is tricky to orchestrate in a test flow as by-design DSes don't scale to 0.
			// But, we can hack the DS selector to mimic this.
			t.Log("Updating CNI Daemonset")
			origCNIDaemonSet := util.GetCNIDaemonSet(t, c, i.Settings().SystemNamespace)

			// Leave the DS in place, but cause all its backing pods to terminate.
			util.ScaleCNIDaemonsetToZeroPods(t, c, i.Settings().SystemNamespace)

			// Rollout restart app instances in the echo namespace, and wait for a broken instance.
			// Because the CNI daemonset was not marked for deleting when the CNI daemonset pods shut down,
			// the CNI daemonset pods we just removed should have left the CNI plugin in place on the nodes,
			// which will stall new pods being scheduled.
			t.Log("Rollout restart echo instance to get broken app instances")
			rolloutCmd := fmt.Sprintf("kubectl rollout restart deployment -n %s", ns.Name())
			if output, err := shell.Execute(true, rolloutCmd); err != nil {
				t.Fatalf("failed to rollout restart deployments %v. Output: %v", err, output)
			}

			// Since the CNI plugin is in place but no agent is there, pods should stall infinitely
			util.WaitForStalledPodOrFail(t, c, ns)

			t.Log("Redeploy CNI")
			// The only thing left is the raw DS with no backing pods, so just delete it
			util.DeleteCNIDaemonset(t, c, i.Settings().SystemNamespace)
			// Now bring back the original CNI Daemonset, which should recreate backing pods
			util.DeployCNIDaemonset(t, c, origCNIDaemonSet)

			// Rollout restart app instances in the echo namespace, which should schedule now
			// that the CNI daemonset is back
			// NOTE - technically we don't need to forcibly restart the app pods, they will
			// (eventually) reschedule naturally now that the node agent is back.
			// Doing an explicit rollout restart is typically just faster and also helps keep tests reliable.
			t.Log("Rollout restart echo instance to get a fixed instance")
			if output, err := shell.Execute(true, rolloutCmd); err != nil {
				t.Fatalf("failed to rollout restart deployments %v. Output: %v", err, output)
			}
			rolloutStatusCmd := fmt.Sprintf("kubectl rollout status deployment -n %s", ns.Name())
			t.Log("wait for rollouts to finish")
			if output, err := shell.Execute(true, rolloutStatusCmd); err != nil {
				t.Fatalf("failed to rollout status deployments %v. Output: %v", err, output)
			}

			// Everyone should be happy
			common.RunAllTrafficTests(t, i, apps.SingleNamespaceView())
		})
}
