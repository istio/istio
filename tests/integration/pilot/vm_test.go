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

package pilot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/client-go/pkg/apis/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/controller/workloadentry"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/pilot/common"
)

func GetAdditionVMImages() []string {
	// Note - bionic is not here as its the default
	return []string{"app_sidecar_ubuntu_xenial", "app_sidecar_ubuntu_focal",
		"app_sidecar_debian_9", "app_sidecar_debian_10", "app_sidecar_centos_7", "app_sidecar_centos_8"}
}

func TestVmOSPost(t *testing.T) {
	framework.
		NewTest(t).
		RequiresSingleCluster(). // TODO(landow) fix DNS issues with multicluster/VMs/headless
		Features("traffic.reachability").
		Label(label.Postsubmit).
		Run(func(ctx framework.TestContext) {
			b := echoboot.NewBuilder(ctx)
			images := GetAdditionVMImages()
			instances := make([]echo.Instance, len(images))
			for i, image := range images {
				b = b.With(&instances[i], echo.Config{
					Service:    "vm-" + strings.ReplaceAll(image, "_", "-"),
					Namespace:  apps.Namespace,
					Ports:      common.EchoPorts,
					DeployAsVM: true,
					VMImage:    image,
					Subsets:    []echo.SubsetConfig{{}},
					Cluster:    ctx.Clusters().Default(),
				})
			}
			b.BuildOrFail(ctx)

			for i, image := range images {
				i, image := i, image
				ctx.NewSubTest(image).RunParallel(func(ctx framework.TestContext) {
					for _, tt := range common.VMTestCases(echo.Instances{instances[i]}, apps) {
						tt.Run(ctx, apps.Namespace.Name())
					}
				})
			}
		})
}

func TestVMRegistrationLifecycle(t *testing.T) {
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Features("vm.autoregistration").
		Run(func(ctx framework.TestContext) {
			scaleDeploymentOrFail(ctx, "istiod", i.Settings().SystemNamespace, 2)
			client := apps.PodA.GetOrFail(ctx, echo.InCluster(ctx.Clusters().Default()))
			// TODO test multi-network (must be shared control plane but on different networks)
			var autoVM echo.Instance
			_ = echoboot.NewBuilder(ctx).
				With(&autoVM, echo.Config{
					Namespace:      apps.Namespace,
					Service:        "auto-vm",
					Ports:          common.EchoPorts,
					DeployAsVM:     true,
					AutoRegisterVM: true,
				}).BuildOrFail(ctx)
			ctx.NewSubTest("initial registration").Run(func(ctx framework.TestContext) {
				retry.UntilSuccessOrFail(ctx, func() error {
					res, err := client.Call(echo.CallOptions{Target: autoVM, Port: &autoVM.Config().Ports[0]})
					if err != nil {
						return err
					}
					return res.CheckOK()
				}, retry.Timeout(15*time.Second))
			})
			ctx.NewSubTest("reconnect reuses WorkloadEntry").Run(func(ctx framework.TestContext) {
				// ensure we have two pilot instances, other tests can pass before the second one comes up
				retry.UntilSuccessOrFail(ctx, func() error {
					pilotRes, err := ctx.Clusters().Default().CoreV1().Pods(i.Settings().SystemNamespace).
						List(context.TODO(), metav1.ListOptions{LabelSelector: "istio=pilot"})
					if err != nil {
						return err
					}
					if len(pilotRes.Items) != 2 {
						return errors.New("expected 2 pilots")
					}
					return nil
				}, retry.Timeout(10*time.Second))

				// get the initial workload entry state
				entries := getWorkloadEntriesOrFail(ctx, autoVM)
				if len(entries) != 1 {
					ctx.Fatalf("expected exactly 1 WorkloadEntry but got %d", len(entries))
				}
				initialWLE := entries[0]

				// keep force-disconnecting until we observe a reconnect to a different istiod instance
				initialPilot := initialWLE.Annotations[workloadentry.WorkloadControllerAnnotation]
				disconnectProxy(ctx, initialPilot, autoVM)
				retry.UntilSuccessOrFail(ctx, func() error {
					entries := getWorkloadEntriesOrFail(ctx, autoVM)
					if len(entries) != 1 || entries[0].UID != initialWLE.UID {
						ctx.Fatalf("WorkloadEntry was cleaned up unexpectedly")
					}

					currentPilot := entries[0].Annotations[workloadentry.WorkloadControllerAnnotation]
					if currentPilot == initialPilot || !strings.HasPrefix(currentPilot, "istiod-") {
						disconnectProxy(ctx, currentPilot, autoVM)
						return errors.New("expected WorkloadEntry to be updated by other pilot")
					}
					return nil
				}, retry.Delay(5*time.Second))
			})
			ctx.NewSubTest("disconnect deletes WorkloadEntry").Run(func(ctx framework.TestContext) {
				deployment := fmt.Sprintf("%s-%s", autoVM.Config().Service, "v1")
				scaleDeploymentOrFail(ctx, deployment, autoVM.Config().Namespace.Name(), 0)
				// it should take at most just over GracePeriod to cleanup if all pilots are healthy
				retry.UntilSuccessOrFail(ctx, func() error {
					if len(getWorkloadEntriesOrFail(ctx, autoVM)) > 0 {
						return errors.New("expected 0 WorkloadEntries")
					}
					return nil
				}, retry.Timeout(2*features.WorkloadEntryCleanupGracePeriod+(2*time.Second)))
			})
		})
}

func disconnectProxy(ctx framework.TestContext, pilot string, instance echo.Instance) {
	proxyID := strings.Join([]string{instance.WorkloadsOrFail(ctx)[0].PodName(), instance.Config().Namespace.Name()}, ".")
	cmd := "pilot-discovery request GET /debug/force_disconnect?proxyID=" + proxyID
	stdOut, _, err := ctx.Clusters().Default().
		PodExec(pilot, i.Settings().SystemNamespace, "discovery", cmd)
	if err != nil {
		scopes.Framework.Warnf("failed to force disconnect %s: %v: %v", proxyID, stdOut, err)
	}
}

func scaleDeploymentOrFail(ctx framework.TestContext, name, namespace string, scale int32) {
	s, err := ctx.Clusters().Default().AppsV1().Deployments(namespace).
		GetScale(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		ctx.Fatal(err)
	}
	s.Spec.Replicas = scale
	_, err = ctx.Clusters().Default().AppsV1().Deployments(namespace).
		UpdateScale(context.TODO(), name, s, metav1.UpdateOptions{})
	if err != nil {
		ctx.Fatal(err)
	}
}

func getWorkloadEntriesOrFail(ctx framework.TestContext, vm echo.Instance) []v1alpha3.WorkloadEntry {
	res, err := ctx.Clusters().Default().Istio().NetworkingV1alpha3().
		WorkloadEntries(vm.Config().Namespace.Name()).
		List(context.TODO(), metav1.ListOptions{LabelSelector: "app=" + vm.Config().Service})
	if err != nil {
		ctx.Fatal(err)
	}
	return res.Items
}
