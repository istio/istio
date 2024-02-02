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

package cacertrotation

import (
	"errors"
	"fmt"
	"testing"
	"time"

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"

	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/echotest"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/tests/integration/security/util/cert"
	"istio.io/istio/tests/integration/security/util/reachability"
)

var apps deployment.SingleNamespaceView

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(nil, setupConfig, cert.CreateCASecret)).
		Setup(deployment.SetupSingleNamespace(&apps, deployment.Config{})).
		Setup(func(ctx resource.Context) error {
			return reachability.CreateCustomInstances(&apps)
		}).
		Run()
}

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfgYaml := `
values:
  pilot:
    env:
      ISTIO_MULTIROOT_MESH: true
  meshConfig:
    defaultConfig:
      proxyMetadata:
        PROXY_CONFIG_XDS_AGENT: "true"
`
	cfg.ControlPlaneValues = cfgYaml
}

func TestReachability(t *testing.T) {
	framework.NewTest(t).
		Features("security.peer.cacert-rotation").
		Run(func(t framework.TestContext) {
			istioCfg := istio.DefaultConfigOrFail(t, t)
			istioCtl := istioctl.NewOrFail(t, t, istioctl.Config{})
			namespace.ClaimOrFail(t, t, istioCfg.SystemNamespace)

			from := apps.EchoNamespace.A
			to := apps.EchoNamespace.B
			fromAndTo := from.Append(to)

			lastUpdateTime, err := getWorkloadCertLastUpdateTime(t, from[0], istioCtl)
			if err != nil {
				t.Errorf("failed to get workload cert last update time: %v", err)
			}

			// Verify traffic works between a and b
			echotest.New(t, fromAndTo).
				WithDefaultFilters(1, 1).
				FromMatch(match.ServiceName(from.NamespacedName())).
				ToMatch(match.ServiceName(to.NamespacedName())).
				Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
					// Verify mTLS works between a and b
					opts := echo.CallOptions{
						To: to,
						Port: echo.Port{
							Name: "http",
						},
					}
					opts.Check = check.And(check.OK(), check.ReachedTargetClusters(t))

					from.CallOrFail(t, opts)
				})

			// step 1: Update CA root cert with combined root
			if err := cert.CreateCustomCASecret(t,
				"ca-cert.pem", "ca-key.pem",
				"cert-chain.pem", "root-cert-combined.pem"); err != nil {
				t.Errorf("failed to update combined CA secret: %v", err)
			}

			lastUpdateTime = waitForWorkloadCertUpdate(t, from[0], istioCtl, lastUpdateTime)

			// step 2: Update CA signing key/cert with cacert to trigger workload cert resigning
			if err := cert.CreateCustomCASecret(t,
				"ca-cert-alt.pem", "ca-key-alt.pem",
				"cert-chain-alt.pem", "root-cert-combined-2.pem"); err != nil {
				t.Errorf("failed to update CA secret: %v", err)
			}

			lastUpdateTime = waitForWorkloadCertUpdate(t, from[0], istioCtl, lastUpdateTime)

			// step 3: Remove the old root cert
			if err := cert.CreateCustomCASecret(t,
				"ca-cert-alt.pem", "ca-key-alt.pem",
				"cert-chain-alt.pem", "root-cert-alt.pem"); err != nil {
				t.Errorf("failed to update CA secret: %v", err)
			}

			waitForWorkloadCertUpdate(t, from[0], istioCtl, lastUpdateTime)

			// Verify traffic works between a and b after cert rotation
			echotest.New(t, fromAndTo).
				WithDefaultFilters(1, 1).
				FromMatch(match.ServiceName(from.NamespacedName())).
				ToMatch(match.ServiceName(to.NamespacedName())).
				Run(func(t framework.TestContext, from echo.Instance, to echo.Target) {
					// Verify mTLS works between a and b
					opts := echo.CallOptions{
						To: to,
						Port: echo.Port{
							Name: "http",
						},
					}
					opts.Check = check.And(check.OK(), check.ReachedTargetClusters(t))

					from.CallOrFail(t, opts)
				})
		})
}

func getWorkloadCertLastUpdateTime(t framework.TestContext, i echo.Instance, ctl istioctl.Instance) (time.Time, error) {
	podID, err := getPodID(i)
	if err != nil {
		t.Fatalf("Could not get Pod ID: %v", err)
	}
	podName := fmt.Sprintf("%s.%s", podID, i.NamespaceName())
	out, errOut, err := ctl.Invoke([]string{"pc", "s", podName, "-o", "json"})
	if err != nil || errOut != "" {
		t.Errorf("failed to retrieve pod secret from %s, err: %v errOut: %s", podName, err, errOut)
	}

	dump := &admin.SecretsConfigDump{}
	if err := protomarshal.Unmarshal([]byte(out), dump); err != nil {
		t.Errorf("failed to unmarshal secret dump: %v", err)
	}

	for _, s := range dump.DynamicActiveSecrets {
		if s.Name == security.WorkloadKeyCertResourceName {
			return s.LastUpdated.AsTime(), nil
		}
	}

	return time.Now(), errors.New("failed to find workload cert")
}

// Abstracted function to wait for workload cert to be updated
func waitForWorkloadCertUpdate(t framework.TestContext, from echo.Instance, istioCtl istioctl.Instance, lastUpdateTime time.Time) time.Time {
	retry.UntilOrFail(t, func() bool {
		updateTime, err := getWorkloadCertLastUpdateTime(t, from, istioCtl)
		if err != nil {
			t.Logf("failed to get workload cert last update time: %v", err)
			return false
		}

		// retry when workload cert is not updated
		if updateTime.After(lastUpdateTime) {
			lastUpdateTime = updateTime
			t.Logf("workload cert is updated, last update time: %v", updateTime)
			return true
		}

		return false
	}, retry.Timeout(5*time.Minute), retry.Delay(1*time.Second))
	return lastUpdateTime
}

func getPodID(i echo.Instance) (string, error) {
	wls, err := i.Workloads()
	if err != nil {
		return "", nil
	}

	for _, wl := range wls {
		return wl.PodName(), nil
	}

	return "", fmt.Errorf("no workloads")
}
