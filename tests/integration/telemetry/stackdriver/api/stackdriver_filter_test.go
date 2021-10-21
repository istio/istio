//go:build integ
// +build integ

// Copyright Istio Authors. All Rights Reserved.
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

package stackdriver

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cloud.google.com/go/compute/metadata"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/gcemetadata"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/stackdriver"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/tests/integration/telemetry"
	stackdrivertest "istio.io/istio/tests/integration/telemetry/stackdriver"
)

const (
	serverRequestCount = "tests/integration/telemetry/stackdriver/testdata/server_request_count.json.tmpl"
	clientRequestCount = "tests/integration/telemetry/stackdriver/testdata/client_request_count.json.tmpl"
	serverLogEntry     = "tests/integration/telemetry/stackdriver/testdata/unsampled_server_access_log.json.tmpl"

	fakeGCEMetadataServerValues = `
meshConfig:
  defaultConfig:
    proxyMetadata:
      GCE_METADATA_HOST: `
)

var (
	ist        istio.Instance
	echoNsInst namespace.Instance
	gceInst    gcemetadata.Instance
	sdInst     stackdriver.Instance
	srv        echo.Instances
	clt        echo.Instances
)

func getIstioInstance() *istio.Instance {
	return &ist
}

func getEchoNamespaceInstance() namespace.Instance {
	return echoNsInst
}

func unmarshalFromTemplateFile(file string, out proto.Message, clName, trustDomain string) error {
	templateFile, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	resource, err := tmpl.Evaluate(string(templateFile), map[string]interface{}{
		"EchoNamespace": getEchoNamespaceInstance().Name(),
		"ClusterName":   clName,
		"TrustDomain":   trustDomain,
		"OnGCE":         metadata.OnGCE(),
	})
	if err != nil {
		return err
	}
	return protomarshal.Unmarshal([]byte(resource), out)
}

// TestStackdriverMonitoring verifies that stackdriver WASM filter exports metrics with expected labels.
func TestStackdriverMonitoring(t *testing.T) {
	framework.NewTest(t).
		Features("observability.telemetry.stackdriver.api").
		Run(func(ctx framework.TestContext) {
			g, _ := errgroup.WithContext(context.Background())
			for _, cltInstance := range clt {
				cltInstance := cltInstance
				g.Go(func() error {
					err := retry.UntilSuccess(func() error {
						if err := stackdrivertest.SendTraffic(t, cltInstance, http.Header{}); err != nil {
							return err
						}
						clName := cltInstance.Config().Cluster.Name()
						trustDomain := telemetry.GetTrustDomain(cltInstance.Config().Cluster, ist.Settings().SystemNamespace)
						scopes.Framework.Infof("Validating for cluster %s", clName)

						// Validate cluster names in telemetry below once https://github.com/istio/istio/issues/28125 is fixed.
						if err := stackdrivertest.ValidateMetrics(t, filepath.Join(env.IstioSrc, serverRequestCount), filepath.Join(env.IstioSrc, clientRequestCount),
							clName, trustDomain); err != nil {
							return err
						}
						t.Logf("Metrics validated")

						if err := stackdrivertest.ValidateLogs(t, filepath.Join(env.IstioSrc, serverLogEntry), clName, trustDomain, stackdriver.ServerAccessLog); err != nil {
							return err
						}
						t.Logf("logs validated")
						// TODO: add trace validation

						return nil
					}, retry.Delay(framework.TelemetryRetryDelay), retry.Timeout(framework.TelemetryRetryTimeout))
					if err != nil {
						return err
					}
					return nil
				})
			}
			if err := g.Wait(); err != nil {
				t.Fatalf("test failed: %v", err)
			}
		})
}

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		Label(label.CustomSetup).
		Setup(stackdrivertest.ConditionallySetupMetadataServer).
		Setup(istio.Setup(getIstioInstance(), setupConfig)).
		Setup(func(ctx resource.Context) error {
			i, err := istio.Get(ctx)
			if err != nil {
				return err
			}
			return ctx.Config().ApplyYAML(i.Settings().SystemNamespace, `
apiVersion: telemetry.istio.io/v1alpha1
kind: Telemetry
metadata:
  name: mesh-default
  namespace: istio-system
spec:
  accessLogging:
  - providers:
    - name: stackdriver
  metrics:
  - providers:
    - name: stackdriver
`)
		}).
		Setup(stackdrivertest.TestSetup).
		Run()
}

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}

	// conditionally use a fake metadata server for testing off of GCP
	if gceInst != nil {
		cfg.ControlPlaneValues = strings.Join([]string{cfg.ControlPlaneValues, fakeGCEMetadataServerValues, gceInst.Address()}, "")
	}
}
