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

package migrationca

import (
	"context"
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/connection"
)

var inst istio.Instance

const (
	ASvc            = "a"
	BSvc            = "b"
	IstioCARevision = "asm-revision-istiodca"
	MeshCARevision  = "asm-revision-meshca"
)

func checkConnectivity(t *testing.T, ctx framework.TestContext, a echo.Instances, b echo.Instances, testPrefix string) {
	t.Helper()
	ctx.NewSubTest(testPrefix).Run(func(ctx framework.TestContext) {
		srcList := []echo.Instance{a[0], b[0]}
		dstList := []echo.Instance{b[0], a[0]}
		for index := range srcList {
			src := srcList[index]
			dst := dstList[index]
			callOptions := echo.CallOptions{
				Target:   dst,
				PortName: "http",
				Scheme:   scheme.HTTP,
				Count:    1,
			}
			checker := connection.Checker{
				From:          src,
				Options:       callOptions,
				ExpectSuccess: true,
				DestClusters:  b.Clusters(),
			}
			checker.CheckOrFail(ctx)
		}
	})
}

// TestIstiodToMeshCAMigration: test zero downtime migration from Istiod CA to Google Mesh CA
func TestIstiodToMeshCAMigration(t *testing.T) {
	framework.NewTest(t).
		RequiresSingleCluster().
		Features("security.migrationca.citadel-meshca").
		Run(func(ctx framework.TestContext) {
			oldCaNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix:   "citadel",
				Inject:   true,
				Revision: IstioCARevision,
			})

			newCaNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix:   "meshca",
				Inject:   true,
				Revision: MeshCARevision,
			})

			builder := echoboot.NewBuilder(ctx)

			// Create workloads in namespaces served by both CA's
			builder.
				WithClusters(ctx.Clusters()...).
				WithConfig(util.EchoConfig(ASvc, oldCaNamespace, false, nil)).
				WithConfig(util.EchoConfig(BSvc, newCaNamespace, false, nil))

			echos, err := builder.Build()
			if err != nil {
				t.Fatalf("failed to bring up apps for ca_migration: %v", err)
				return
			}
			cluster := ctx.Clusters().Default()
			a := echos.Match(echo.Service(ASvc)).Match(echo.InCluster(cluster))
			b := echos.Match(echo.Service(BSvc)).Match(echo.InCluster(cluster))

			// Test Basic setup between workloads served by different CA's
			checkConnectivity(t, ctx, a, b, "cross-ca-connectivity")

			// Migration tests

			// Annotate oldCANamespace with newer CA control plane revision label.
			err = oldCaNamespace.SetLabel("istio.io/rev", MeshCARevision)
			if err != nil {
				t.Fatalf("unable to annotate namespace %v with label %v: %v",
					oldCaNamespace.Name(), MeshCARevision, err)
			}

			// Restart workload A to allow mutating webhook to do its job
			if err := a[0].Restart(); err != nil {
				t.Fatalf("revisioned instance rollout failed with: %v", err)
			}

			// Check mTLS connectivity works after workload A is signed by meshca
			checkConnectivity(t, ctx, a, b, "same-ca-connectivity")

			// Handle removal of root of trust
			systemNs, err := istio.ClaimSystemNamespace(ctx)
			if err != nil {
				t.Fatalf("unable to retrieve istio-system namespace: %v", err)
			}

			err = cluster.CoreV1().Secrets(systemNs.Name()).Delete(context.TODO(), "istio-ca-secret", metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				t.Fatalf("unable to delete secret %v from the cluster. Encountered error: %v", "istio-ca-secret", err)
			}
			err = cluster.CoreV1().Secrets(systemNs.Name()).Delete(context.TODO(), "cacerts", metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				t.Fatalf("unable to delete secret %v from the cluster. Encountered error: %v", "cacerts", err)
			}

			// TODO: need better way to determine that secret has been unmounted from istiod file-system in case of cacerts
			// instead of sleeping
			time.Sleep(10 * time.Second)

			// Restart MeshCa Control plane to pick up new roots of trust
			deploymentsClient := cluster.AppsV1().Deployments(systemNs.Name())
			meshcaDeployment, err := deploymentsClient.Get(context.TODO(), fmt.Sprintf("istiod-%s", MeshCARevision), metav1.GetOptions{})
			if err != nil {
				t.Fatalf("unable to retrieve meshca control plane deployment: %v", err)
			}
			meshcaDeployment.Spec.Template.ObjectMeta.Annotations["restart"] = "true"
			_, err = deploymentsClient.Update(context.TODO(), meshcaDeployment, metav1.UpdateOptions{})
			if err != nil {
				t.Fatalf("unable to update meshca control plane deployment: %v", err)
			}
			// TODO: need better way to rollout and restart meshca deployment and wait until pods are active
			// also need way to correctly determine if Istiod root has been removed from workload trustbundle
			time.Sleep(10 * time.Second)

			// Check mTLS works after roots of trust have been removed
			checkConnectivity(t, ctx, a, b, "post-trust-removal-connectivity")
		})
}

func TestMain(t *testing.M) {
	// Integration test for testing migration of workloads from Istiod Ca based control plane to
	// Google Mesh Ca based control plane
	framework.NewSuite(t).
		Label(label.CustomSetup).
		Setup(istio.Setup(&inst, setupConfig)).
		Run()
}

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
}
