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

package cniupgrade

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/label"
	"istio.io/istio/pkg/config/constants"
	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
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
	"istio.io/istio/pkg/test/util/retry"
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
			origCNIDaemonSet := getCNIDaemonSet(t, c)

			// Leave the DS in place, but cause all its backing pods to terminate.
			scaleCNIDaemonsetToZeroPods(t, c)

			// Rollout restart app instances in the echo namespace, and wait for a broken instance.
			// Because the CNI daemonset was not marked for deleting when the CNI daemonset pods shut down,
			// the CNI daemonset pods we just removed should have left the CNI plugin in place on the nodes,
			// which will stall new pods being scheduled.
			t.Log("Rollout restart echo instance to get broken app instances")
			rolloutCmd := fmt.Sprintf("kubectl rollout restart deployment -n %s", ns.Name())
			if _, err := shell.Execute(true, rolloutCmd); err != nil {
				t.Fatalf("failed to rollout restart deployments %v", err)
			}

			// Since the CNI plugin is in place but no agent is there, pods should stall infinitely
			waitForStalledPodOrFail(t, c, ns)

			t.Log("Redeploy CNI")
			// Now bring back the original CNI Daemonset, which should recreate backing pods
			deployCNIDaemonset(t, c, origCNIDaemonSet)

			// Rollout restart app instances in the echo namespace, which should schedule now
			// that the CNI daemonset is back
			t.Log("Rollout restart echo instance to get a fixed instance")
			if _, err := shell.Execute(true, rolloutCmd); err != nil {
				t.Fatalf("failed to rollout restart deployments %v", err)
			}

			// Everyone should be happy
			common.RunAllTrafficTests(t, i, apps.SingleNamespaceView())
		})
}

func getCNIDaemonSet(ctx framework.TestContext, c cluster.Cluster) *appsv1.DaemonSet {
	cniDaemonSet, err := c.(istioKube.CLIClient).
		Kube().AppsV1().DaemonSets(i.Settings().SystemNamespace).
		Get(context.Background(), "istio-cni-node", metav1.GetOptions{})
	if err != nil {
		ctx.Fatalf("failed to get CNI Daemonset %v from ns %s", err, i.Settings().SystemNamespace)
	}
	if cniDaemonSet == nil {
		ctx.Fatal("cannot find CNI Daemonset")
	}
	return cniDaemonSet
}

// scaleCNIDaemonsetToZeroPods patches the DS with a non-existing node selector.
// This will cause the DS to "scale down" to 0 pods, while leaving the
// actual DS resource in-place for our test (important for CNI upgrade flow test)
func scaleCNIDaemonsetToZeroPods(ctx framework.TestContext, c cluster.Cluster) {
	patchData := `{
			"spec": {
				"template": {
					"spec": {
						"nodeSelector": {
							"non-existing": "true"
						}
					}
				}
			}
		}`

	if _, err := c.(istioKube.CLIClient).
		Kube().AppsV1().DaemonSets(i.Settings().SystemNamespace).
		Patch(context.Background(), "istio-cni-node", types.StrategicMergePatchType, []byte(patchData), metav1.PatchOptions{}); err != nil {
		ctx.Fatalf("failed to patch CNI Daemonset %v", err)
	}

	// Wait until the CNI Daemonset pod cannot be fetched anymore
	retry.UntilSuccessOrFail(ctx, func() error {
		scopes.Framework.Infof("Checking if CNI Daemonset pods are deleted...")
		pods, err := c.PodsForSelector(context.TODO(), i.Settings().SystemNamespace, "k8s-app=istio-cni-node")
		if err != nil {
			return err
		}
		if len(pods.Items) > 0 {
			return errors.New("CNI Daemonset pod still exists after deletion")
		}
		return nil
	}, retry.Delay(1*time.Second), retry.Timeout(80*time.Second))
}

func deployCNIDaemonset(ctx framework.TestContext, c cluster.Cluster, cniDaemonSet *appsv1.DaemonSet) {
	deployDaemonSet := appsv1.DaemonSet{}
	deployDaemonSet.Spec = cniDaemonSet.Spec
	deployDaemonSet.ObjectMeta = metav1.ObjectMeta{
		Name:        cniDaemonSet.ObjectMeta.Name,
		Namespace:   cniDaemonSet.ObjectMeta.Namespace,
		Labels:      cniDaemonSet.ObjectMeta.Labels,
		Annotations: cniDaemonSet.ObjectMeta.Annotations,
	}
	_, err := c.(istioKube.CLIClient).Kube().AppsV1().DaemonSets(cniDaemonSet.ObjectMeta.Namespace).
		Create(context.Background(), &deployDaemonSet, metav1.CreateOptions{})
	if err != nil {
		ctx.Fatalf("failed to deploy CNI Daemonset %v", err)
	}
}

func waitForStalledPodOrFail(t framework.TestContext, cluster cluster.Cluster, ns namespace.Instance) {
	retry.UntilSuccessOrFail(t, func() error {
		pods, err := cluster.Kube().CoreV1().Pods(ns.Name()).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		if len(pods.Items) == 0 {
			return fmt.Errorf("still waiting the pod in namespace %v to start", ns.Name())
		}
		// Verify that every pod is in broken state due to CNI plugin failure.
		for _, p := range pods.Items {
			for _, cState := range p.Status.ContainerStatuses {
				waiting := cState.State.Waiting

				scopes.Framework.Infof("checking pod status for stall")
				if waiting != nil && waiting.Reason == "ContainerCreating" {
					scopes.Framework.Infof("checking pod events")
					events, err := cluster.Kube().CoreV1().Events(ns.Name()).List(context.TODO(), metav1.ListOptions{})
					if err != nil {
						return err
					}
					for _, ev := range events.Items {
						if ev.InvolvedObject.Name == p.Name && strings.Contains(ev.Message, "Failed to create pod sandbox") {
							return nil
						}
					}
				}

			}
		}
		return fmt.Errorf("cannot find any pod with wanted failure status")
	}, retry.Delay(1*time.Second), retry.Timeout(80*time.Second))
}
