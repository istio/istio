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

package mcs

import (
	"context"
	"fmt"
	"io/ioutil"
	"strconv"
	"testing"
	"time"

	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/pilot/common"
)

const (
	serviceExportName = "test-service"
)

var (
	i               istio.Instance
	serviceExportNS string
	echos           echo.Instances
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		RequireEnvironmentVersion("1.17").
		Setup(func(ctx resource.Context) error {
			crd, err := ioutil.ReadFile("../testdata/mcs-serviceexport-crd.yaml")
			if err != nil {
				return err
			}
			return ctx.Config().ApplyYAML("", string(crd))
		}).
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = `
values:
  pilot:
    env:
      PILOT_ENABLE_MCS_SERVICEEXPORT: "true"`
		})).
		Setup(func(ctx resource.Context) error {
			// Create a new namespace in each cluster.
			ns, err := namespace.New(ctx, namespace.Config{
				Prefix: "se",
				Inject: true,
			})
			if err != nil {
				return err
			}
			serviceExportNS = ns.Name()

			// Create an echo instance in each cluster.
			echos, err = echoboot.NewBuilder(ctx).
				WithClusters(ctx.Clusters()...).
				WithConfig(echo.Config{
					Service:   serviceExportName,
					Namespace: ns,
					Ports:     common.EchoPorts,
				}).Build()
			return err
		}).
		Run()
}

func TestServiceExports(t *testing.T) {
	framework.NewTest(t).
		Features("traffic.mcs.serviceexport").
		RequiresSingleCluster().
		Run(func(ctx framework.TestContext) {
			// Verify that ServiceExport is created automatically for services.
			ctx.NewSubTest("exported").RunParallel(
				func(ctx framework.TestContext) {
					for i, e := range echos {
						e := e
						ctx.NewSubTest(strconv.Itoa(i)).RunParallel(func(ctx framework.TestContext) {
							cluster := e.Config().Cluster
							client := cluster.MCSApis().MulticlusterV1alpha1().ServiceExports(serviceExportNS)

							// Verify that the ServiceExport was created.
							ctx.NewSubTest("create").Run(func(ctx framework.TestContext) {
								retry.UntilSuccessOrFail(ctx, func() error {
									serviceExport, err := client.Get(context.TODO(), serviceExportName, v1.GetOptions{})
									if err != nil {
										return err
									}

									if serviceExport == nil {
										return fmt.Errorf("serviceexport %s/%s not found in cluster %s",
											serviceExportNS, serviceExportName, cluster.Name())
									}

									return nil
								}, retry.Timeout(30*time.Second))
							})

							// Delete the echo Service and verify that the ServiceExport is automatically removed.
							ctx.NewSubTest("delete").Run(func(ctx framework.TestContext) {
								err := cluster.CoreV1().Services(serviceExportNS).Delete(
									context.TODO(), serviceExportName, v1.DeleteOptions{})
								if err != nil {
									ctx.Fatalf("failed deleting service %s/%s in cluster %s: %v",
										serviceExportNS, serviceExportName, cluster.Name(), err)
								}
								retry.UntilSuccessOrFail(t, func() error {
									_, err := client.Get(context.TODO(), serviceExportName, v1.GetOptions{})

									if err != nil && k8sErrors.IsNotFound(err) {
										// Success! We automatically removed the ServiceExport when the Service
										// removed.
										return nil
									}

									if err != nil {
										return err
									}

									return fmt.Errorf("failed to remove serviceExport %s/%s in cluster %s",
										serviceExportNS, serviceExportName, cluster.Name())
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
						client := cluster.MCSApis().MulticlusterV1alpha1().ServiceExports(ns)
						services, err := client.List(context.TODO(), v1.ListOptions{})
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
