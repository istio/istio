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
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"testing"
	"time"
)

var (
	client echo.Instance
	vm     echo.Instance
)

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		RequireSingleCluster().
		Setup(istio.Setup(nil, func(ctx resource.Context, cfg *istio.Config) {
			cfg.Values["pilot.keepaliveMaxServerConnectionAge"] = "30s"
			cfg.Values["pilot.autoscaleMin"] = "2"
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
			var nextPilot string
			var originalWE types.UID
			retry.UntilSuccessOrFail(ctx, func() error {
				// TODO this "switches pilot" check would likely be flaky
				pilotRes, err := ctx.Clusters().Default().CoreV1().Pods("istio-system").List(context.TODO(), metav1.ListOptions{LabelSelector: "istio=pilot"})
				if err != nil {
					return err
				}
				if len(pilotRes.Items) != 2 {
					return errors.New("expected 2 pilots")
				}

				wles := wlesOrFail(ctx)
				if len(wles) != 1 {
					ctx.Fatal("expected exactly 1 workload entry that is more than 40 seconds old")
				}
				originalWE = wles[0].UID
				currentPilot := wles[0].Annotations[xds.WorkloadControllerAnnotation]
				nextPilot = ""
				for _, p := range pilotRes.Items {
					if p.Name != currentPilot {
						nextPilot = p.Name
						return nil
					}
				}
				return nil
			})

			// timeout is 2 * (grace period + connection age) should allow for 2 disconnect/re-connect cycles.
			retry.UntilSuccessOrFail(ctx, func() error {
				wles := wlesOrFail(ctx)
				if len(wles) != 1 {
					return errors.New("expected exactly 1 workload entry")
				}
				if wles[0].UID != originalWE {
					return errors.New("expected original WorkloadEntry but a different one was created")
				}
				if wles[0].Annotations[xds.WorkloadControllerAnnotation] != nextPilot {
					return errors.New("expected WorkloadEntry to be updated by other pilot")
				}
				return nil
			}, retry.Timeout(90*time.Second))
		})
		ctx.NewSubTest("disconnect deletes WorkloadEntry").Run(func(ctx framework.TestContext) {
			scaleDepOrFail(ctx, 0)
			// it should take at most 2*grace period to trigger removal
			retry.UntilSuccessOrFail(ctx, func() error {
				if len(wlesOrFail(ctx)) > 0 {
					return errors.New("expected 0 WorkloadEntries")
				}
				return nil
			}, retry.Timeout(25*time.Second))
		})
	})
}

func scaleDepOrFail(ctx framework.TestContext, scale int32) {
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

func wlesOrFail(ctx framework.TestContext) []v1alpha3.WorkloadEntry {
	res, err := ctx.Clusters().Default().Istio().NetworkingV1alpha3().
		WorkloadEntries(vm.Config().Namespace.Name()).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		ctx.Fatal(err)
	}
	return res.Items
}
