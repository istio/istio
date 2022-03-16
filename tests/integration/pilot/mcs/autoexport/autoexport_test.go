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

package autoexport

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/pilot/mcs/common"
)

var (
	i     istio.Instance
	echos common.EchoDeployment
)

func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		RequireMultiPrimary().
		RequireMinVersion(17).
		Setup(common.InstallMCSCRDs).
		Setup(istio.Setup(&i, enableMCSAutoExport)).
		Setup(common.DeployEchosFunc("se", &echos)).
		Run()
}

func TestAutoExport(t *testing.T) {
	framework.NewTest(t).
		Features("traffic.mcs.autoexport").
		Run(func(ctx framework.TestContext) {
			serviceExportGVR := common.KubeSettings(ctx).ServiceExportGVR()
			// Verify that ServiceExport is created automatically for services.
			ctx.NewSubTest("exported").RunParallel(
				func(ctx framework.TestContext) {
					serviceB := match.ServiceName(model.NamespacedName{Name: common.ServiceB, Namespace: echos.Namespace})
					for _, cluster := range serviceB.GetMatches(echos.Instances).Clusters() {
						cluster := cluster
						ctx.NewSubTest(cluster.StableName()).RunParallel(func(ctx framework.TestContext) {
							// Verify that the ServiceExport was created.
							ctx.NewSubTest("create").Run(func(ctx framework.TestContext) {
								retry.UntilSuccessOrFail(ctx, func() error {
									serviceExport, err := cluster.Dynamic().Resource(serviceExportGVR).Namespace(echos.Namespace).Get(
										context.TODO(), common.ServiceB, v1.GetOptions{})
									if err != nil {
										return err
									}

									if serviceExport == nil {
										return fmt.Errorf("serviceexport %s/%s not found in cluster %s",
											echos.Namespace, common.ServiceB, cluster.Name())
									}

									return nil
								}, retry.Timeout(30*time.Second))
							})

							// Delete the echo Service and verify that the ServiceExport is automatically removed.
							ctx.NewSubTest("delete").Run(func(ctx framework.TestContext) {
								err := cluster.CoreV1().Services(echos.Namespace).Delete(
									context.TODO(), common.ServiceB, v1.DeleteOptions{})
								if err != nil {
									ctx.Fatalf("failed deleting service %s/%s in cluster %s: %v",
										echos.Namespace, common.ServiceB, cluster.Name(), err)
								}
								retry.UntilSuccessOrFail(t, func() error {
									_, err := cluster.Dynamic().Resource(serviceExportGVR).Namespace(echos.Namespace).Get(
										context.TODO(), common.ServiceB, v1.GetOptions{})

									if err != nil && k8sErrors.IsNotFound(err) {
										// Success! We automatically removed the ServiceExport when the Service
										// removed.
										return nil
									}

									if err != nil {
										return err
									}

									return fmt.Errorf("failed to remove serviceExport %s/%s in cluster %s",
										echos.Namespace, common.ServiceB, cluster.Name())
								}, retry.Timeout(30*time.Second))
							})
						})
					}
				})

			// Verify that cluster-local services do not automatically generate ServiceExport.
			ctx.NewSubTest("non-exported").RunParallel(func(ctx framework.TestContext) {
				ns := "kube-system"
				for i, cluster := range ctx.Clusters() {
					cluster := cluster
					ctx.NewSubTest(strconv.Itoa(i)).RunParallel(func(ctx framework.TestContext) {
						services, err := cluster.Dynamic().Resource(serviceExportGVR).Namespace(ns).List(
							context.TODO(), v1.ListOptions{})
						if err != nil {
							ctx.Fatal(err)
						}
						if len(services.Items) > 0 {
							ctx.Fatalf("serviceexports created for cluster-local services in cluster %s",
								cluster.Name())
						}
					})
				}
			})
		})
}

func enableMCSAutoExport(t resource.Context, cfg *istio.Config) {
	cfg.ControlPlaneValues = fmt.Sprintf(`
values:
  pilot:
    env:
      ENABLE_MCS_AUTO_EXPORT: "true"
      MCS_API_GROUP: %s
      MCS_API_VERSION: %s`,
		common.KubeSettings(t).MCSAPIGroup,
		common.KubeSettings(t).MCSAPIVersion)
}
