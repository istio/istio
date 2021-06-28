// +build integ
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

package cni

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

var (
	ist  istio.Instance
	apps = &util.EchoDeployments{}
)

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(&ist, nil)).
		Setup(func(t resource.Context) error {
			return util.SetupApps(t, ist, apps, false)
		}).
		Run()
}

func TestCNIRaceRepair(t *testing.T) {
	framework.NewTest(t).
		Features("traffic.cni.race-condition-repair").
		Run(func(t framework.TestContext) {
			if !ist.Settings().EnableCNI {
				t.Skip("CNI race condition mitigation is only tested when CNI is enabled.")
			}
			cluster := t.Clusters().Default()
			// To begin with, delete CNI Daemonset to simulate a CNI race condition.
			if err := deleteCNIDaemonset(cluster); err != nil {
				t.Fatalf("failed to delete CNI Dameonset %v", err)
			}

			// Rollout restart instances in namespace 1, and wait for a broken instance.
			rolloutCmd := fmt.Sprintf("kubectl rollout restart deployment -n %s", apps.Namespace1.Name())
			if _, err := shell.Execute(true, rolloutCmd); err != nil {
				t.Fatalf("failed to rollout restart deployments %v", err)
			}
			waitForBrokenPodOrFail(t, cluster)

			// Now bring back CNI Daemonset, and pod in namespace 1 should be repaired.
			if err := deployCNIDaemonset(t, cluster); err != nil {
				t.Fatalf("failed to deploy CNI Dameonset %v", err)
			}
			waitForRepairOrFail(t, cluster)
		})
}

func deleteCNIDaemonset(c cluster.Cluster) error {
	if err := c.(istioKube.ExtendedClient).
		Kube().AppsV1().DaemonSets("kube-system").
		Delete(context.Background(), "istio-cni-node", metav1.DeleteOptions{}); err != nil {
		return err
	}

	// Wait until the CNI Daemonset pod cannot be fetched anymore
	err := retry.UntilSuccess(func() error {
		scopes.Framework.Infof("Checking if CNI Daemonset pod is deleted...")
		pods, err := c.PodsForSelector(context.TODO(), "kube-system", "k8s-app=istio-cni-node")
		if err != nil {
			return err
		}
		if len(pods.Items) > 0 {
			return errors.New("CNI Daemonset pod still exists after deletion")
		}
		return nil
	}, retry.Delay(1*time.Second), retry.Timeout(80*time.Second))

	return err
}

func deployCNIDaemonset(ctx framework.TestContext, c cluster.Cluster) error {
	args := []string{
		"install", "-f", filepath.Join(env.IstioSrc,
			"tests/integration/pilot/cni/testdata/cni.yaml"),
		"--skip-confirmation",
	}
	istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{Cluster: c})
	_, _, err := istioCtl.Invoke(args)
	return err
}

func waitForBrokenPodOrFail(t framework.TestContext, cluster cluster.Cluster) {
	retry.UntilSuccessOrFail(t, func() error {
		pods, err := cluster.CoreV1().Pods(apps.Namespace1.Name()).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		if len(pods.Items) == 0 {
			return errors.New("still waiting the pod in namespace 1 to start")
		}
		// Verify that at least one pod is in broken state due to the race condition.
		for _, p := range pods.Items {
			for _, container := range p.Status.InitContainerStatuses {
				if state := container.LastTerminationState.Terminated; state != nil && state.ExitCode ==
					constants.ValidationErrorCode {
					return nil
				}
			}
		}
		return fmt.Errorf("cannot find any pod with wanted exit code %v", constants.ValidationErrorCode)
	}, retry.Delay(1*time.Second), retry.Timeout(80*time.Second))
}

func waitForRepairOrFail(t framework.TestContext, cluster cluster.Cluster) {
	retry.UntilSuccessOrFail(t, func() error {
		pods, err := cluster.CoreV1().Pods(apps.Namespace1.Name()).List(context.TODO(), metav1.ListOptions{})
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
