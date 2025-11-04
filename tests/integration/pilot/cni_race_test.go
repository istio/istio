//go:build integ

//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package pilot

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/pkg/test/util/retry"
	util "istio.io/istio/tests/integration/ambient"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

func TestCNIRaceRepair(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			if !i.Settings().EnableCNI {
				t.Skip("CNI race condition mitigation is only tested when CNI is enabled.")
			}
			c := t.Clusters().Default()

			ns := namespace.NewOrFail(t, namespace.Config{
				Prefix: "cni-race",
				Inject: true,
			})

			// Create a echo deployment in the cni-race namespace.
			t.Logf("Deploy an echo instance in namespace %v...", ns.Name())
			deployment.
				New(t, c).
				WithConfig(echo.Config{
					Namespace: ns,
					Ports:     ports.All(),
					Subsets:   []echo.SubsetConfig{{}},
				}).BuildOrFail(t)

			// To begin with, delete CNI Daemonset to simulate a CNI race condition.
			// Temporarily store CNI DaemonSet, which will be deployed again later.
			t.Log("Delete CNI Daemonset temporarily to simulate race condition")
			cniDaemonSet := util.GetCNIDaemonSet(t, c, i.Settings().SystemNamespace)
			util.DeleteCNIDaemonset(t, c, i.Settings().SystemNamespace)

			// Rollout restart instances in the echo namespace, and wait for a broken instance.
			t.Log("Rollout restart echo instance to get a broken instance")
			rolloutCmd := fmt.Sprintf("kubectl rollout restart deployment -n %s", ns.Name())
			if _, err := shell.Execute(true, rolloutCmd); err != nil {
				t.Fatalf("failed to rollout restart deployments %v", err)
			}
			util.WaitForStalledPodOrFail(t, c, ns)

			t.Log("Redeploy CNI and verify repair takes effect by evicting the broken pod")
			// Now bring back CNI Daemonset, and pod in the echo namespace should be repaired.
			util.DeployCNIDaemonset(t, c, cniDaemonSet)
			waitForRepairOrFail(t, c, ns)
		})
}

func waitForRepairOrFail(t framework.TestContext, cluster cluster.Cluster, ns namespace.Instance) {
	retry.UntilSuccessOrFail(t, func() error {
		pods, err := cluster.Kube().CoreV1().Pods(ns.Name()).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		if len(pods.Items) == 0 {
			return errors.New("no pod found")
		}
		// Verify that no pod is broken by the race condition now.
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
