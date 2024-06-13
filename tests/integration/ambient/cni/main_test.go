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
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config/constants"
	testKube "istio.io/istio/pkg/test/kube"
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
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
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
	Captured   = "captured"
	Uncaptured = "uncaptured"
)

func SetupApps(t resource.Context, i istio.Instance, apps *EchoDeployments) error {
	var err error
	apps.Namespace, err = namespace.New(t, namespace.Config{
		Prefix: "echo",
		Inject: false,
		Labels: map[string]string{
			constants.DataplaneModeLabel: "ambient",
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
					Labels:   map[string]string{constants.DataplaneModeLabel: constants.DataplaneModeNone},
				},
				{
					Replicas: 1,
					Version:  "v2",
					Labels:   map[string]string{constants.DataplaneModeLabel: constants.DataplaneModeNone},
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
// this, but it is an effective way to test that, once configured, pods and ztunnel
// can tolerate `istio-cni` disruptions, without disrupting the dataplane
func TestTrafficWithEstablishedPodsIfCNIMissing(t *testing.T) {
	framework.NewTest(t).
		TopLevel().
		Run(func(t framework.TestContext) {
			apps := common_deploy.NewOrFail(t, t, common_deploy.Config{
				NoExternalNamespace: true,
				IncludeExtAuthz:     false,
			})

			c := t.Clusters().Default()
			t.Log("Getting current daemonset")
			// mostly a correctness check - to make sure it's actually there
			_ = getCNIDaemonSet(t, c)

			ns := apps.SingleNamespaceView().EchoNamespace.Namespace
			fetchFn := testKube.NewPodFetch(c, ns.Name())

			if _, err := testKube.WaitUntilPodsAreReady(fetchFn); err != nil {
				t.Fatal(err)
			}

			t.Log("Deleting current daemonset")
			// Delete JUST the daemonset - ztunnel + workloads remain in place
			deleteCNIDaemonset(t, c)

			// Our echo instances have already been deployed/configured by the CNI,
			// so the CNI being removed should not disrupt them.
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

func deleteCNIDaemonset(ctx framework.TestContext, c cluster.Cluster) {
	if err := c.(istioKube.CLIClient).
		Kube().AppsV1().DaemonSets(i.Settings().SystemNamespace).
		Delete(context.Background(), "istio-cni-node", metav1.DeleteOptions{}); err != nil {
		ctx.Fatalf("failed to delete CNI Daemonset %v", err)
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
