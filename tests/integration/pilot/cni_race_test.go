//go:build integ
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

package pilot

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	v1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

func TestCNIRaceRepair(t *testing.T) {
	framework.NewTest(t).
		Features("traffic.cni.race-condition-repair").
		Run(func(t framework.TestContext) {
			if !i.Settings().EnableCNI {
				t.Skip("CNI race condition mitigation is only tested when CNI is enabled.")
			}
			cluster := t.Clusters().Default()

			ns := namespace.NewOrFail(t, t, namespace.Config{
				Prefix: "cni-race",
				Inject: true,
			})

			// Create a echo deployment in the cni-race namespace.
			t.Logf("Deploy an echo instance in namespace %v...", ns.Name())
			deployment.
				New(t, cluster).
				WithConfig(echo.Config{
					Namespace: ns,
					Ports:     common.Ports,
					Subsets:   []echo.SubsetConfig{{}},
				}).BuildOrFail(t)

			// To begin with, delete CNI Daemonset to simulate a CNI race condition.
			// Temporarily store CNI DaemonSet, which will be deployed again later.
			t.Log("Delete CNI Daemonset temporarily to simulate race condition")
			cniDaemonSet := getCNIDaemonSet(t, cluster)
			deleteCNIDaemonset(t, cluster)

			// Rollout restart instances in the echo namespace, and wait for a broken instance.
			t.Log("Rollout restart echo instance to get a broken instance")
			rolloutCmd := fmt.Sprintf("kubectl rollout restart deployment -n %s", ns.Name())
			if _, err := shell.Execute(true, rolloutCmd); err != nil {
				t.Fatalf("failed to rollout restart deployments %v", err)
			}
			waitForBrokenPodOrFail(t, cluster, ns)

			t.Log("Redeploy CNI and verify repair takes effect by evicting the broken pod")
			// Now bring back CNI Daemonset, and pod in the echo namespace should be repaired.
			deployCNIDaemonset(t, cluster, cniDaemonSet)
			waitForRepairOrFail(t, cluster, ns)
		})
}

func getCNIDaemonSet(ctx framework.TestContext, c cluster.Cluster) *v1.DaemonSet {
	cniDaemonSet, err := c.(istioKube.ExtendedClient).
		Kube().AppsV1().DaemonSets("kube-system").
		Get(context.Background(), "istio-cni-node", metav1.GetOptions{})
	if err != nil {
		ctx.Fatalf("failed to get CNI Daemonset %v", err)
	}
	if cniDaemonSet == nil {
		ctx.Fatal("cannot find CNI Daemonset")
	}
	return cniDaemonSet
}

func deleteCNIDaemonset(ctx framework.TestContext, c cluster.Cluster) {
	if err := c.(istioKube.ExtendedClient).
		Kube().AppsV1().DaemonSets("kube-system").
		Delete(context.Background(), "istio-cni-node", metav1.DeleteOptions{}); err != nil {
		ctx.Fatalf("failed to delete CNI Daemonset %v", err)
	}

	// Wait until the CNI Daemonset pod cannot be fetched anymore
	retry.UntilSuccessOrFail(ctx, func() error {
		scopes.Framework.Infof("Checking if CNI Daemonset pods are deleted...")
		pods, err := c.PodsForSelector(context.TODO(), "kube-system", "k8s-app=istio-cni-node")
		if err != nil {
			return err
		}
		if len(pods.Items) > 0 {
			return errors.New("CNI Daemonset pod still exists after deletion")
		}
		return nil
	}, retry.Delay(1*time.Second), retry.Timeout(80*time.Second))
}

func deployCNIDaemonset(ctx framework.TestContext, c cluster.Cluster, cniDaemonSet *v1.DaemonSet) {
	deployDaemonSet := v1.DaemonSet{}
	deployDaemonSet.Spec = cniDaemonSet.Spec
	deployDaemonSet.ObjectMeta = metav1.ObjectMeta{
		Name:        cniDaemonSet.ObjectMeta.Name,
		Namespace:   cniDaemonSet.ObjectMeta.Namespace,
		Labels:      cniDaemonSet.ObjectMeta.Labels,
		Annotations: cniDaemonSet.ObjectMeta.Annotations,
	}
	_, err := c.(istioKube.ExtendedClient).Kube().AppsV1().DaemonSets("kube-system").
		Create(context.Background(), &deployDaemonSet, metav1.CreateOptions{})
	if err != nil {
		ctx.Fatalf("failed to deploy CNI Daemonset %v", err)
	}
}

func waitForBrokenPodOrFail(t framework.TestContext, cluster cluster.Cluster, ns namespace.Instance) {
	retry.UntilSuccessOrFail(t, func() error {
		pods, err := cluster.CoreV1().Pods(ns.Name()).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}
		if len(pods.Items) == 0 {
			return fmt.Errorf("still waiting the pod in namespace %v to start", ns.Name())
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

func waitForRepairOrFail(t framework.TestContext, cluster cluster.Cluster, ns namespace.Instance) {
	retry.UntilSuccessOrFail(t, func() error {
		pods, err := cluster.CoreV1().Pods(ns.Name()).List(context.TODO(), metav1.ListOptions{})
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
