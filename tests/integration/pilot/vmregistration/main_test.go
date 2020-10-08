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

package vmregistration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	ist    istio.Instance
	client echo.Instance
	vm     echo.Instance
)

func TestMain(m *testing.M) {
	// TODO refactor this so it can be integrated into main pilot suite
	framework.NewSuite(m).
		RequireSingleCluster().
		Setup(istio.Setup(&ist, func(ctx resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = `
values:
  pilot:
    autoscaleMin: 2
    env:
      ENABLE_ADMIN_ENDPOINTS: true
`
		})).
		Setup(func(ctx resource.Context) error {
			ns, err := namespace.New(ctx, namespace.Config{Prefix: "vmreg", Inject: true})
			if err != nil {
				return err
			}
			_, err = echoboot.NewBuilder(ctx).
				With(&client, echo.Config{Namespace: ns, Service: "client"}).
				With(&vm, echo.Config{
					Namespace: ns,
					Service:   "vm",
					Ports: []echo.Port{{
						Name:         "http",
						Protocol:     protocol.HTTP,
						ServicePort:  8080,
						InstancePort: 8080,
					}},
					DeployAsVM:     true,
					AutoRegisterVM: true,
				}).
				Build()
			return err
		}).Run()
}

func TestAutoRegistrationLifecycle(t *testing.T) {
	// a very implementation-detail specific test that is being used for development, will need restructuring to merge
	framework.NewTest(t).Run(func(ctx framework.TestContext) {
		ctx.NewSubTest("initial registration").Run(func(ctx framework.TestContext) {
			client.CallOrFail(ctx, echo.CallOptions{Target: vm, Port: &vm.Config().Ports[0]})
		})
		ctx.NewSubTest("reconnect resuses WorkloadEntry").Run(func(ctx framework.TestContext) {
			// ensure we have two pilot instances, other tests can pass before the second one comes up
			retry.UntilSuccessOrFail(ctx, func() error {
				pilotRes, err := ctx.Clusters().Default().CoreV1().Pods(ist.Settings().SystemNamespace).List(context.TODO(), metav1.ListOptions{LabelSelector: "istio=pilot"})
				if err != nil {
					return err
				}
				if len(pilotRes.Items) != 2 {
					return errors.New("expected 2 pilots")
				}
				return nil
			}, retry.Timeout(10*time.Second))

			// get the initial workload entry state
			entries := getWorkloadEntriesOrFail(ctx)
			if len(entries) != 1 {
				ctx.Fatalf("expected exactly 1 WorkloadEntry but got %d", len(entries))
			}
			initialWLE := entries[0]

			// keep force-disconnecting until we observe a reconnect to a different istiod instance
			initialPilot := initialWLE.Annotations[xds.WorkloadControllerAnnotation]
			disconnectProxy(ctx, initialPilot, vm)
			retry.UntilSuccessOrFail(ctx, func() error {
				entries := getWorkloadEntriesOrFail(ctx)
				if len(entries) != 1 || entries[0].UID != initialWLE.UID {
					ctx.Fatalf("WorkloadEntry was cleaned up unexpectedly")
				}

				currentPilot := entries[0].Annotations[xds.WorkloadControllerAnnotation]
				if currentPilot == initialPilot || !strings.HasPrefix(currentPilot, "istiod-") {
					disconnectProxy(ctx, currentPilot, vm)
					return errors.New("expected WorkloadEntry to be updated by other pilot")
				}
				return nil
			}, retry.Delay(5*time.Second))
		})
		//ctx.NewSubTest("disconnect deletes WorkloadEntry").Run(func(ctx framework.TestContext) {
		//	scaleDeploymentOrFail(ctx, 0)
		//	// it should take at most 2*grace period to trigger removal
		//	retry.UntilSuccessOrFail(ctx, func() error {
		//		if len(getWorkloadEntriesOrFail(ctx)) > 0 {
		//			return errors.New("expected 0 WorkloadEntries")
		//		}
		//		return nil
		//	}, retry.Timeout(25*time.Second))
		//})
	})
}

func disconnectProxy(ctx framework.TestContext, pilot string, instance echo.Instance) {
	proxyID := strings.Join([]string{instance.WorkloadsOrFail(ctx)[0].PodName(), instance.Config().Namespace.Name()}, ".")
	stdOut, _, err := ctx.Clusters().Default().
		PodExec(pilot, ist.Settings().SystemNamespace, "discovery", "pilot-discovery request GET /debug/force_disconnect?proxyID="+proxyID)
	if err != nil {
		scopes.Framework.Warnf("failed disconnect: %v: %v", stdOut, err)
	}
}

func scaleDeploymentOrFail(ctx framework.TestContext, scale int32) {
	depName := fmt.Sprintf("%s-%s", vm.Config().Service, "v1")
	s, err := ctx.Clusters().Default().AppsV1().Deployments(vm.Config().Namespace.Name()).
		GetScale(context.TODO(), depName, metav1.GetOptions{})
	if err != nil {
		ctx.Fatal(err)
	}
	s.Spec.Replicas = scale
	_, err = ctx.Clusters().Default().AppsV1().Deployments(vm.Config().Namespace.Name()).
		UpdateScale(context.TODO(), depName, s, metav1.UpdateOptions{})
	if err != nil {
		ctx.Fatal(err)
	}
}

func getWorkloadEntriesOrFail(ctx framework.TestContext) []v1alpha3.WorkloadEntry {
	res, err := ctx.Clusters().Default().Istio().NetworkingV1alpha3().
		WorkloadEntries(vm.Config().Namespace.Name()).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		ctx.Fatal(err)
	}
	return res.Items
}
