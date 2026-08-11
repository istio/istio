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

package common

import (
	"context"
	"errors"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	echodeployment "istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/ambient"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

// RunCNIRaceRepairTests simulates a CNI DaemonSet race condition: deletes CNI,
// restarts pods (which end up broken due to missing iptables/nftables rules), then
// redeploys CNI and verifies the repair mechanism evicts the broken pods.
// The test is automatically skipped when CNI is not enabled.
func RunCNIRaceRepairTests(t framework.TestContext, i istio.Instance) {
	t.Helper()
	if !i.Settings().EnableCNI {
		t.Skip("CNI race condition mitigation is only tested when CNI is enabled.")
	}
	c := t.Clusters().Default()

	ns := namespace.NewOrFail(t, namespace.Config{
		Prefix: "cni-race",
		Inject: true,
	})

	t.Logf("Deploy an echo instance in namespace %v...", ns.Name())
	echodeployment.
		New(t, c).
		WithConfig(echo.Config{
			Namespace: ns,
			Ports:     ports.All(),
			Subsets:   []echo.SubsetConfig{{}},
		}).BuildOrFail(t)

	t.Log("Delete CNI Daemonset temporarily to simulate race condition")
	cniDaemonSet := util.GetCNIDaemonSet(t, c, i.Settings().SystemNamespace)
	util.DeleteCNIDaemonset(t, c, i.Settings().SystemNamespace)

	t.Log("Rollout restart echo instance to get a broken instance")
	rolloutCmd := fmt.Sprintf("kubectl rollout restart deployment -n %s", ns.Name())
	if _, err := shell.Execute(true, rolloutCmd); err != nil {
		t.Fatalf("failed to rollout restart deployments %v", err)
	}
	util.WaitForStalledPodOrFail(t, c, ns)

	t.Log("Redeploy CNI and verify repair takes effect by evicting the broken pod")
	util.DeployCNIDaemonset(t, c, cniDaemonSet)
	waitForCNIRepairOrFail(t, c, ns)
}

func waitForCNIRepairOrFail(t framework.TestContext, clust cluster.Cluster, ns namespace.Instance) {
	retry.UntilSuccessOrFail(t, func() error {
		pods, err := clust.Kube().CoreV1().Pods(ns.Name()).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		if len(pods.Items) == 0 {
			return errors.New("no pod found")
		}
		for _, p := range pods.Items {
			for _, container := range p.Status.InitContainerStatuses {
				if state := container.LastTerminationState.Terminated; state != nil && state.ExitCode ==
					constants.ValidationErrorCode {
					return errors.New("there are still pods in broken state due to CNI race condition")
				}
			}
		}
		return nil
	}, retry.Delay(1*time.Second), retry.Timeout(80*time.Second))
}
