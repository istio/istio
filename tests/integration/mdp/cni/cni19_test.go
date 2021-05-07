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
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	kube2 "istio.io/istio/pkg/test/kube"
	"istio.io/istio/tests/integration/pilot/common"
)

var (
	apps echo.Instances
	ns   namespace.Instance
)

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Run()
}

func TestCNI19Upgrade(t *testing.T) {
	framework.NewTest(t).
		Features("traffic.routing").
		Run(func(ctx framework.TestContext) {
			// Install 1.9 CNI.
			applyFileOrFail(ctx, "kube-system", "testdata/cni-1.9.yaml")

			cluster := ctx.Clusters().Default()
			_, err := kube2.WaitUntilPodsAreReady(kube2.NewSinglePodFetch(cluster, "kube-system", "k8s-app=istio-cni-node"))
			if err != nil {
				ctx.Fatal(err)
			}

			// Install test apps.
			if err := setupApps(ctx); err != nil {
				ctx.Fatal(err)
			}

			// Ping between apps.
			r, err := apps[0].Call(echo.CallOptions{
				Count:    1,
				Target:   apps[1],
				PortName: "http",
				Headers: map[string][]string{
					"Host": {apps[1].Config().FQDN()},
				},
				Message: t.Name(),
			})
			if err != nil {
				ctx.Fatal(err)
			}
			if r.CheckOK() != nil {
				ctx.Fatal("1.9 CNI configured pod cannot connect properly.")
			}
		})
}

// applyFileOrFail applys the given yaml file and deletes it during context cleanup
func applyFileOrFail(t framework.TestContext, ns, filename string) {
	t.Helper()
	if err := t.Clusters().Default().ApplyYAMLFiles(ns, filename); err != nil {
		t.Fatal(err)
	}
	t.ConditionalCleanup(func() {
		t.Clusters().Default().DeleteYAMLFiles(ns, filename)
	})
}

func setupApps(ctx resource.Context) error {
	var err error
	ns, err = namespace.New(ctx, namespace.Config{Prefix: "echo", Inject: true})
	if err != nil {
		return err
	}
	apps, err = echoboot.NewBuilder(ctx).
		WithClusters(ctx.Clusters()...).
		WithConfig(echo.Config{
			Namespace: ns,
			Service:   "app1",
			Ports:     common.EchoPorts,
		}).
		WithConfig(echo.Config{
			Namespace: ns,
			Service:   "app2",
			Ports:     common.EchoPorts,
		}).
		Build()
	return err
}
