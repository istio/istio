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

package ambient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tools/istio-iptables/pkg/constants"
)

const (
	RetryDelay   = 2 * time.Second
	RetryTimeOut = 5 * time.Minute
	Timeout      = 2 * time.Minute
)

func DeployCNIDaemonset(ctx framework.TestContext, c cluster.Cluster, cniDaemonSet *appsv1.DaemonSet) {
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

	// Wait for the DS backing pods to ready up
	retry.UntilSuccessOrFail(ctx, func() error {
		ensureCNIDS := GetCNIDaemonSet(ctx, c, cniDaemonSet.ObjectMeta.Namespace)
		if ensureCNIDS.Status.NumberReady == ensureCNIDS.Status.DesiredNumberScheduled {
			return nil
		}
		return fmt.Errorf("still waiting for CNI pods to become ready before proceeding")
	}, retry.Delay(RetryDelay), retry.Timeout(RetryTimeOut))
}

func DeleteCNIDaemonset(ctx framework.TestContext, c cluster.Cluster, systemNamespace string) {
	if err := c.(istioKube.CLIClient).
		Kube().AppsV1().DaemonSets(systemNamespace).
		Delete(context.Background(), "istio-cni-node", metav1.DeleteOptions{}); err != nil {
		ctx.Fatalf("failed to delete CNI Daemonset %v", err)
	}

	// Wait until the CNI Daemonset pods cannot be fetched anymore
	retry.UntilSuccessOrFail(ctx, func() error {
		scopes.Framework.Infof("Checking if CNI Daemonset pods are deleted...")
		pods, err := c.PodsForSelector(context.TODO(), systemNamespace, "k8s-app=istio-cni-node")
		if err != nil {
			return err
		}
		if len(pods.Items) > 0 {
			return errors.New("CNI Daemonset pod still exists after deletion")
		}
		return nil
	}, retry.Delay(1*time.Second), retry.Timeout(80*time.Second))
}

func GetCNIDaemonSet(ctx framework.TestContext, c cluster.Cluster, systemNamespace string) *appsv1.DaemonSet {
	cniDaemonSet, err := c.(istioKube.CLIClient).
		Kube().AppsV1().DaemonSets(systemNamespace).
		Get(context.Background(), "istio-cni-node", metav1.GetOptions{})
	if err != nil {
		ctx.Fatalf("failed to get CNI Daemonset %v from ns %s", err, systemNamespace)
	}
	if cniDaemonSet == nil {
		ctx.Fatal("cannot find CNI Daemonset")
	}
	return cniDaemonSet
}

// ScaleCNIDaemonsetToZeroPods patches the DS with a non-existing node selector.
// This will cause the DS to "scale down" to 0 pods, while leaving the
// actual DS resource in-place for our test (important for CNI upgrade flow test)
func ScaleCNIDaemonsetToZeroPods(ctx framework.TestContext, c cluster.Cluster, systemNamespace string) {
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

	PatchCNIDaemonSet(ctx, c, systemNamespace, []byte(patchData))

	// Wait until the CNI Daemonset pod cannot be fetched anymore
	retry.UntilSuccessOrFail(ctx, func() error {
		scopes.Framework.Infof("Checking if CNI Daemonset pods are deleted...")
		pods, err := c.PodsForSelector(context.TODO(), systemNamespace, "k8s-app=istio-cni-node")
		if err != nil {
			return err
		}
		if len(pods.Items) > 0 {
			return errors.New("CNI Daemonset pod still exists after deletion")
		}
		return nil
	}, retry.Delay(1*time.Second), retry.Timeout(80*time.Second))
}

func WaitForStalledPodOrFail(t framework.TestContext, cluster cluster.Cluster, ns namespace.Instance) {
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
				if waiting != nil && (waiting.Reason == "ContainerCreating" || waiting.Reason == "PodInitializing") {
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
			for _, cState := range p.Status.InitContainerStatuses {
				terminated := cState.LastTerminationState.Terminated

				scopes.Framework.Infof("checking pod status terminated")
				if terminated != nil && terminated.ExitCode == constants.ValidationErrorCode {
					return nil
				}
			}
		}
		return fmt.Errorf("cannot find any pod with wanted failure status")
	}, retry.Delay(1*time.Second), retry.Timeout(80*time.Second))
}

func PatchCNIDaemonSet(ctx framework.TestContext, c cluster.Cluster, systemNamespace string, patch []byte) *appsv1.DaemonSet {
	cniDaemonSet, err := c.(istioKube.CLIClient).
		Kube().AppsV1().DaemonSets(systemNamespace).
		Patch(context.Background(), "istio-cni-node", types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		ctx.Fatalf("failed to patch CNI Daemonset %v from ns %s", err, systemNamespace)
	}
	if cniDaemonSet == nil {
		ctx.Fatal("cannot find CNI Daemonset")
	}
	return cniDaemonSet
}
