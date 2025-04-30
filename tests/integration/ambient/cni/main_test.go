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

package cni

import (
	"fmt"
	"testing"
	"time"

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
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/pkg/test/util/retry"
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
			if ctx.Settings().AmbientMultiNetwork {
				cfg.SkipDeployCrossClusterSecrets = true
			}
			cfg.ControlPlaneValues = `
values:
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

// Tests that pods which have already been configured by `istio-cni`
// continue to function when `istio-cni` has been removed from the node.
//
// New pods would be "missed" in this scenario, so it is not recommended to do
// this in the real world, but it is an effective way to test that, once configured, pods and ztunnel
// can tolerate `istio-cni` disruptions, without disrupting the actual established dataplane
func TestTrafficWithEstablishedPodsIfCNIMissing(t *testing.T) {
	framework.NewTest(t).
		TopLevel().
		Run(func(t framework.TestContext) {
			apps := common_deploy.NewOrFail(t, common_deploy.Config{
				NoExternalNamespace: true,
				IncludeExtAuthz:     false,
			})

			c := t.Clusters().Default()
			t.Log("Getting current daemonset")
			// mostly a correctness check - to make sure it's actually there
			origDS := util.GetCNIDaemonSet(t, c, i.Settings().SystemNamespace)

			ns := apps.SingleNamespaceView().EchoNamespace.Namespace
			fetchFn := testKube.NewPodFetch(c, ns.Name())

			if _, err := testKube.WaitUntilPodsAreReady(fetchFn); err != nil {
				t.Fatal(err)
			}

			t.Log("Deleting current daemonset")
			// Delete JUST the daemonset - ztunnel + workloads remain in place
			util.DeleteCNIDaemonset(t, c, i.Settings().SystemNamespace)

			// Our echo instances have already been deployed/configured by the CNI,
			// so the CNI being removed should not disrupt them.
			common.RunAllTrafficTests(t, i, apps.SingleNamespaceView())

			// put it back
			util.DeployCNIDaemonset(t, c, origDS)
		})
}

func TestCNIMisconfigHealsOnRestart(t *testing.T) {
	framework.NewTest(t).
		TopLevel().
		Run(func(t framework.TestContext) {
			c := t.Clusters().Default()
			t.Log("Updating CNI Daemonset config")

			// TODO this is really not very nice - we are mutating cluster state here
			// with other tests which means other tests can break us and we don't have isolation,
			// so we have to be more paranoid.
			//
			// I don't think we have a good way to solve this ATM so doing stuff like this is as
			// good as it gets, short of creating an entirely new suite for every possibly-cluster-destructive op.
			retry.UntilSuccessOrFail(t, func() error {
				ensureCNIDS := util.GetCNIDaemonSet(t, c, i.Settings().SystemNamespace)
				if ensureCNIDS.Status.NumberReady == ensureCNIDS.Status.DesiredNumberScheduled {
					return nil
				}
				return fmt.Errorf("still waiting for CNI pods to become ready before starting")
			}, retry.Delay(1*time.Second), retry.Timeout(80*time.Second))

			// we want to "break" the CNI config by giving a bad path
			// nolint: lll
			volPatch := []byte(fmt.Sprintf(`{"spec":{"template":{"spec":{"volumes":[{"name":"cni-net-dir","hostPath":{"path": "%s", "type": ""}}]}}}}`, "/etc/cni/nope.d"))

			t.Log("Patching the CNI Daemonset")
			_ = util.PatchCNIDaemonSet(t, c, i.Settings().SystemNamespace, volPatch)

			// Why not use `rollout restart` here? It waits for each node's pod to go healthy,
			// so if we intentionally break the DS, we'll never finish breaking all the nodes.
			// So, delete all the pods at once by label
			restartDSPodsCmd := fmt.Sprintf("kubectl delete pods -l k8s-app=istio-cni-node -n %s", i.Settings().SystemNamespace)

			retry.UntilSuccessOrFail(t, func() error {
				t.Log("Restart CNI daemonset pods to get broken instances on every node")
				// depending on timing it can actually take little bit for the patch to be applied and
				// to get all pods to enter a broken state break - so rely on the retry delay to sort that for us
				if _, err := shell.Execute(true, restartDSPodsCmd); err != nil {
					t.Fatalf("failed to restart daemonset pods %v", err)
				}

				time.Sleep(1 * time.Second)

				brokenCNIDS := util.GetCNIDaemonSet(t, c, i.Settings().SystemNamespace)
				t.Log("Checking for broken DS")
				if brokenCNIDS.Status.NumberReady == 0 {
					return nil
				}

				return fmt.Errorf("CNI daemonset pods should all be broken, restarting pods again")
			}, retry.Delay(1*time.Second), retry.Timeout(80*time.Second))

			t.Log("Redeploy CNI with corrected config")

			// we want to "unbreak" the CNI config by giving it back the correct path
			// TODO it would be nice to get the correct path from the original DS, so it doesn't have to be hardcoded - but
			// it shouldn't change, and the other parts of the chart volume ref could change too - don't want to couple this too closely to Helm structure.
			// nolint: lll
			fixedVolPatch := []byte(fmt.Sprintf(`{"spec":{"template":{"spec":{"volumes":[{"name":"cni-net-dir","hostPath":{"path": "%s", "type": ""}}]}}}}`, "/etc/cni/net.d"))

			t.Log("Re-patching the CNI Daemonset")
			_ = util.PatchCNIDaemonSet(t, c, i.Settings().SystemNamespace, fixedVolPatch)

			// Need to sleep a bit to make sure this takes,
			// and also to avoid `restart`-ing too fast, which can give an error like
			// `if restart has already been triggered within the past second, please wait before attempting to trigger another`
			time.Sleep(1 * time.Second)

			// Restart CNI pods so they get the fixed config.
			// to _fix_ the pods we should only have to do this *once*
			t.Log("Restart CNI daemonset to get a fixed instance on every node")
			if _, err := shell.Execute(true, restartDSPodsCmd); err != nil {
				t.Fatalf("failed to restart daemonset %v", err)
			}

			retry.UntilSuccessOrFail(t, func() error {
				fixedCNIDaemonSet := util.GetCNIDaemonSet(t, c, i.Settings().SystemNamespace)
				t.Log("Checking for happy DS")
				if fixedCNIDaemonSet.Status.NumberReady == fixedCNIDaemonSet.Status.DesiredNumberScheduled {
					return nil
				}
				return fmt.Errorf("still waiting for CNI pods to heal")
			}, retry.Delay(1*time.Second), retry.Timeout(80*time.Second))
		})
}
