//go:build integ

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

package filemountedcerts

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

const (
	gatewayCertsSecretName = "test-gateway-file-certs"
	gatewayCRLSecretName   = "test-gateway-file-crl"
)

func createGatewayCRLSecrets(ctx resource.Context) error {
	systemNS, err := istio.ClaimSystemNamespace(ctx)
	if err != nil {
		return err
	}
	secrets := ctx.Clusters().Default().Kube().CoreV1().Secrets(systemNS.Name())
	_, err = secrets.Create(context.Background(), &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: gatewayCertsSecretName},
		Data: map[string][]byte{
			"tls.crt": []byte(ingressutil.TLSServerCertA),
			"tls.key": []byte(ingressutil.TLSServerKeyA),
			"ca.crt":  []byte(ingressutil.CaCertA),
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	_, err = secrets.Create(context.Background(), &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: gatewayCRLSecretName},
		Data: map[string][]byte{
			"ca.crl": []byte(ingressutil.DummyCaCrlA),
		},
	}, metav1.CreateOptions{})
	return err
}

func TestGatewayFileCRLRotation(t *testing.T) {
	framework.NewTest(t).Run(func(t framework.TestContext) {
		const host = "gateway-file-crl.example.com"
		gatewayLabel := inst.Settings().IngressGatewayIstioLabel
		if gatewayLabel == "" {
			gatewayLabel = "ingressgateway"
		}
		t.ConfigIstio().Eval(echo1NS.Name(), map[string]string{
			"Host":         host,
			"GatewayLabel": gatewayLabel,
		}, `
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: file-crl
spec:
  selector:
    istio: {{.GatewayLabel}}
  servers:
  - port:
      number: 443
      name: https
      protocol: HTTPS
    hosts:
    - {{.Host}}
    tls:
      mode: MUTUAL
      serverCertificate: /gateway-crl-certs/tls.crt
      privateKey: /gateway-crl-certs/tls.key
      caCertificates: /gateway-crl-certs/ca.crt
      caCrl: /gateway-crl/ca.crl
---
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: file-crl
spec:
  hosts:
  - {{.Host}}
  gateways:
  - file-crl
  http:
  - directResponse:
      status: 200
`).ApplyOrFail(t)

		gateway := inst.IngressFor(t.Clusters().Default())
		send := func(expected echo.Checker, noRetry bool) {
			t.Helper()
			gateway.CallOrFail(t, echo.CallOptions{
				Port: echo.Port{Protocol: protocol.HTTPS},
				HTTP: echo.HTTP{Headers: http.Header{"Host": {host}}},
				TLS: echo.TLS{
					CaCert: ingressutil.CaCertA,
					Key:    ingressutil.TLSClientKeyA,
					Cert:   ingressutil.TLSClientCertA,
				},
				NewConnectionPerRequest: true,
				Retry:                   echo.Retry{NoRetry: noRetry, Options: []retry.Option{retry.Timeout(2 * time.Minute)}},
				Check:                   expected,
			})
		}
		updateCRL := func(crl string) {
			t.Helper()
			patch, err := json.Marshal(map[string]map[string][]byte{"data": {"ca.crl": []byte(crl)}})
			if err != nil {
				t.Fatal(err)
			}
			secrets := t.Clusters().Default().Kube().CoreV1().Secrets(inst.Settings().SystemNamespace)
			if _, err := secrets.Patch(context.Background(), gatewayCRLSecretName, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
				t.Fatalf("update gateway CRL: %v", err)
			}
		}

		send(check.OK(), false)
		updateCRL(ingressutil.CaCrlA)
		send(check.ErrorContains("tls: revoked certificate"), false)

		malformedUpdateTime := metav1.Now()
		// A valid PEM envelope with invalid DER must reach Envoy's CRL parser.
		malformedCRL := pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: []byte{0x30, 0x00}})
		updateCRL(string(malformedCRL))
		retry.UntilSuccessOrFail(t, func() error {
			pods, err := t.Clusters().Default().PodsForSelector(context.Background(), gateway.Namespace(),
				"istio="+gatewayLabel)
			if err != nil {
				return err
			}
			if len(pods.Items) == 0 {
				return fmt.Errorf("no ingress gateway pods found")
			}
			for _, pod := range pods.Items {
				logs, err := t.Clusters().Default().PodLogsWithOptions(context.Background(), pod.Name, gateway.Namespace(), &v1.PodLogOptions{
					Container: "istio-proxy",
					SinceTime: &malformedUpdateTime,
				})
				if err != nil {
					return err
				}
				if !strings.Contains(logs, "Failed to reload certificates: Failed to load CRL") {
					return fmt.Errorf("gateway %s has not rejected the malformed CRL yet", pod.Name)
				}
			}
			return nil
		}, retry.Timeout(2*time.Minute), retry.Delay(time.Second))
		send(check.ErrorContains("tls: revoked certificate"), true)

		updateCRL(ingressutil.DummyCaCrlA)
		send(check.OK(), false)
	})
}
