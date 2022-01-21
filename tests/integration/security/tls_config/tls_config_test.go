//go:build integ
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

package tlsconfig

import (
	"context"
	"encoding/pem"
	"fmt"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/test/echo/check"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/cert"
	"istio.io/istio/tests/integration/security/util/dir"
	tutil "istio.io/istio/tests/util"
)

func TestTlsVersion13Config(t *testing.T) {
	framework.NewTest(t).
		Features("security.tls.config").
		Run(func(ctx framework.TestContext) {
			if ctx.Clusters().IsMulticluster() {
				t.Skip("https://github.com/istio/istio/issues/37307")
			}
			instA := apps.A[0]
			instB := apps.B[0]
			// First check TLS certificate
			certPEMs := cert.DumpCertFromSidecar(t, instA, instB, "http")
			block, _ := pem.Decode([]byte(strings.Join(certPEMs, "\n")))
			if block == nil { // nolint: staticcheck
				t.Fatalf("failed to parse certificate PEM")
			}
			// Check B responds OK for the call from A on mTLS
			opts := echo.CallOptions{
				To: instB,
				Port: echo.Port{
					Name: "http",
				},
				Check: check.And(
					check.OK(),
					check.MTLSForHTTP()),
				Retry: echo.Retry{
					Options: []retry.Option{retry.Delay(10 * time.Second), retry.Timeout(30 * time.Second)},
				},
			}
			instA.CallOrFail(t, opts)

			target := fmt.Sprintf("%s.%s:%d", util.BSvc, apps.Namespace1.Name(), 80)
			fromSelector := fmt.Sprintf("app=%s", util.ASvc)
			kubeConfig := (ctx.Clusters().Default().(*kube.Cluster)).Filename()
			fromPod, err := dir.GetPodName(apps.Namespace1, fromSelector, kubeConfig)
			if err != nil {
				t.Fatalf("err getting the pod from pod name: %v", err)
			}
			retry := tutil.Retrier{
				BaseDelay: 10 * time.Second,
				Retries:   3,
				MaxDelay:  30 * time.Second,
			}

			// Check that TLS-1.1 is disallowed
			err = checkTLS11Disallowed(kubeConfig, retry, apps.Namespace1, fromPod, "istio-proxy", target)
			if err != nil {
				t.Errorf("checkTLS11Disallowed() returns error: %v", err)
			} else {
				t.Logf("as expected, TLS 1.1 is disallowed")
			}
			// Check that TLS-1.2 is disallowed
			err = checkTLS12Disallowed(kubeConfig, retry, apps.Namespace1, fromPod, "istio-proxy", target)
			if err != nil {
				t.Errorf("checkTLS12Disallowed() returns error: %v", err)
			} else {
				t.Logf("as expected, TLS 1.2 is disallowed")
			}
			// Check that TLS-1.3 is allowed
			err = checkTLS13Allowed(kubeConfig, retry, apps.Namespace1, fromPod, "istio-proxy", target)
			if err != nil {
				t.Errorf("checkTLS13Allowed() returns error: %v", err)
			} else {
				t.Logf("as expected, TLS 1.3 is allowed")
			}
		})
}

func checkTLS11Disallowed(kubeConfig string, retry tutil.Retrier, ns namespace.Instance, fromPod, fromContainer, connectTarget string) error {
	retryFn := func(_ context.Context, i int) error {
		execCmd := fmt.Sprintf(
			"kubectl exec %s -c %s -n %s --kubeconfig %s -- openssl s_client -alpn istio -tls1_1 -connect %s",
			fromPod, fromContainer, ns.Name(), kubeConfig, connectTarget)
		out, _ := shell.Execute(false, execCmd)
		if !strings.Contains(out, "Cipher is (NONE)") {
			return fmt.Errorf("unexpected output when checking that TLS 1.1 is disallowed: %v", out)
		}
		return nil
	}

	if _, err := retry.Retry(context.Background(), retryFn); err != nil {
		return fmt.Errorf("retry of checking that TLS 1.1 is disallowed returns an err: %v", err)
	}
	return nil
}

func checkTLS12Disallowed(kubeConfig string, retry tutil.Retrier, ns namespace.Instance, fromPod, fromContainer, connectTarget string) error {
	retryFn := func(_ context.Context, i int) error {
		execCmd := fmt.Sprintf(
			"kubectl exec %s -c %s -n %s --kubeconfig %s -- openssl s_client -alpn istio -tls1_2 -connect %s",
			fromPod, fromContainer, ns.Name(), kubeConfig, connectTarget)
		out, _ := shell.Execute(false, execCmd)
		if !strings.Contains(out, "Cipher is (NONE)") {
			return fmt.Errorf("unexpected output when checking that TLS 1.2 is disallowed: %v", out)
		}
		return nil
	}

	if _, err := retry.Retry(context.Background(), retryFn); err != nil {
		return fmt.Errorf("retry of checking that TLS 1.2 is disallowed returns an err: %v", err)
	}
	return nil
}

func checkTLS13Allowed(kubeConfig string, retry tutil.Retrier, ns namespace.Instance, fromPod, fromContainer, connectTarget string) error {
	retryFn := func(_ context.Context, i int) error {
		execCmd := fmt.Sprintf(
			"kubectl exec %s -c %s -n %s --kubeconfig %s -- openssl s_client -alpn istio -tls1_3 -connect %s",
			fromPod, fromContainer, ns.Name(), kubeConfig, connectTarget)
		out, _ := shell.Execute(false, execCmd)
		if strings.Contains(out, "Cipher is (NONE)") {
			return fmt.Errorf("unexpected output when checking that TLS 1.3 is allowed: %v", out)
		}
		return nil
	}

	if _, err := retry.Retry(context.Background(), retryFn); err != nil {
		return fmt.Errorf("retry of checking that TLS 1.3 is allowed returns an err: %v", err)
	}
	return nil
}
