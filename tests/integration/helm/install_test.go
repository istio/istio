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

package helm

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	kubeApiCore "k8s.io/api/core/v1"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/helm"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/pkg/log"
)

const (
	IstioNamespace = "istio-system"
	ReleasePrefix  = "istio-"
	BaseChart      = "base"
	DiscoveryChart = "istio-discovery"
	retryDelay     = 2 * time.Second
	retryTimeOut   = 20 * time.Minute
)

var (
	// ChartPath is path of local Helm charts used for testing.
	ChartPath = filepath.Join(env.IstioSrc, "manifests/charts")
)

// TestInstallWithFirstPartyJwt tests Istio installation using Helm
// on a cluster with first-party-jwt
func TestInstallWithFirstPartyJwt(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			workDir, err := ctx.CreateTmpDirectory("helm-install-test")
			if err != nil {
				t.Fatal("failed to create test directory")
			}
			cs := ctx.Environment().(*kube.Environment).KubeClusters[0]
			h := helm.New(cs.Filename(), ChartPath)
			s, err := image.SettingsFromCommandLine()
			if err != nil {
				t.Fatal(err)
			}
			overrideValuesStr := `
global:
  hub: %s
  tag: %s
  jwtPolicy: first-party-jwt
`
			overrideValues := fmt.Sprintf(overrideValuesStr, s.Hub, s.Tag)
			overrideValuesFile := filepath.Join(workDir, "values.yaml")
			if err := ioutil.WriteFile(overrideValuesFile, []byte(overrideValues), os.ModePerm); err != nil {
				t.Fatalf("failed to write iop cr file: %v", err)
			}
			if _, err := cs.CoreV1().Namespaces().Create(context.TODO(), &kubeApiCore.Namespace{
				ObjectMeta: kubeApiMeta.ObjectMeta{
					Name: IstioNamespace,
				},
			}, kubeApiMeta.CreateOptions{}); err != nil {
				_, err := cs.CoreV1().Namespaces().Get(context.TODO(), IstioNamespace, kubeApiMeta.GetOptions{})
				if err == nil {
					log.Info("istio namespace already exist")
				} else {
					t.Fatalf("failed to create istio namespace: %v", err)
				}
			}

			// Install base chart
			err = h.InstallChart(ReleasePrefix+BaseChart, BaseChart,
				IstioNamespace, overrideValuesFile)
			if err != nil {
				t.Fatalf("failed to install istio %s chart", BaseChart)
			}

			// Install discovery chart
			err = h.InstallChart("istiod", "istio-control/"+DiscoveryChart,
				IstioNamespace, overrideValuesFile)
			if err != nil {
				t.Fatalf("failed to install istio %s chart", DiscoveryChart)
			}
			verifyInstallation(t, ctx, cs)
		})
}

// verifyInstallation verify that the Helm installation is successfull
func verifyInstallation(t *testing.T, ctx resource.Context, cs resource.Cluster) {
	scopes.Framework.Infof("=== verifying istio installation === ")

	retry.UntilSuccessOrFail(t, func() error {
		if _, err := kubetest.CheckPodsAreReady(kubetest.NewSinglePodFetch(cs, IstioNamespace, "app=istiod")); err != nil {
			return fmt.Errorf("istiod pod is not ready: %v", err)
		}
		return nil
	}, retry.Timeout(retryTimeOut), retry.Delay(retryDelay))
	sanityCheck(t, ctx)
	scopes.Framework.Infof("=== succeeded ===")
}

func sanityCheck(t *testing.T, ctx resource.Context) {
	scopes.Framework.Infof("running sanity test")
	var client, server echo.Instance
	test := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "default",
		Inject: true,
	})
	echoboot.NewBuilder(ctx).
		With(&client, echo.Config{
			Service:   "client",
			Namespace: test,
			Ports:     []echo.Port{},
		}).
		With(&server, echo.Config{
			Service:   "server",
			Namespace: test,
			Ports: []echo.Port{
				{
					Name:         "http",
					Protocol:     protocol.HTTP,
					InstancePort: 8090,
				}},
		}).
		BuildOrFail(t)
	_ = client.CallWithRetryOrFail(t, echo.CallOptions{
		Target:    server,
		PortName:  "http",
		Validator: echo.ExpectOK(),
	})
}
