package sdstlsorigination

import (
	"bytes"
	"fmt"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"reflect"
	"testing"
	"text/template"
	"time"

	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/resource/environment"
	"istio.io/istio/pkg/test/util/retry"
	sdstlsutil "istio.io/istio/tests/integration/security/sds_tls_origination/util"
)

func TestSimpleTlsOrigination(t *testing.T) {
	framework.NewTest(t).
		Features("security.egress.tls.sds").
		Run(func(ctx framework.TestContext) {
			ctx.RequireOrSkip(environment.Kube)

			var (
				credName = "tls-credential-cacert"
			)

			var CredentialA = sdstlsutil.TLSCredential{
				ClientCert: sdstlsutil.ClientCertA,
				PrivateKey: sdstlsutil.ClientKeyA,
				CaCert:     sdstlsutil.RootCertA,
			}


			// Add kubernetes secret to provision key/cert for gateway.
			sdstlsutil.CreateKubeSecret(t, ctx, []string{credName}, "SIMPLE", CredentialA, false)
			defer sdstlsutil.DeleteKubeSecret(t, ctx, []string{credName})

			internalClient, externalServer, _, serverNamespace := sdstlsutil.SetupEcho(t, ctx, &p)

			// enforce mTLS
			// createStrictMtls(t, ctx, serverNamespace)

			credName = "tls-credential"
			host := "server." + serverNamespace.Name() + ".svc.cluster.local"

			bufDestinationRule := sdstlsutil.CreateDestinationRule(t, serverNamespace, "SIMPLE", credName)

			// Get namespace for gateway pod.
			istioCfg := istio.DefaultConfigOrFail(t, ctx)
			systemNS := namespace.ClaimOrFail(t, ctx, istioCfg.SystemNamespace)

			ctx.Config().ApplyYAMLOrFail(ctx, systemNS.Name(), bufDestinationRule.String())
			defer ctx.Config().DeleteYAMLOrFail(ctx, systemNS.Name(), bufDestinationRule.String())

			retry.UntilSuccessOrFail(t, func() error {
				resp, err := internalClient.Call(echo.CallOptions{
					Target:   externalServer,
					PortName: "http",
					Headers: map[string][]string{
						"Host": {host},
					},
				})
				if err != nil {
					return fmt.Errorf("request failed: %v", err)
				}
				codes := make([]string, 0, len(resp))
				for _, r := range resp {
					codes = append(codes, r.Code)
				}
				if !reflect.DeepEqual(codes, []string{response.StatusCodeOK}) {
					return fmt.Errorf("got codes %q, expected %q", codes, []string{response.StatusCodeOK})
				}
				for _, r := range resp {
					if _, f := r.RawResponse["Handled-By-Egress-Gateway"]; !f {
						return fmt.Errorf("expected to be handled by gateway. response: %+v", r.RawResponse)
					}
				}
				return nil
			}, retry.Delay(time.Second), retry.Timeout(5*time.Second))
		})
}

const (
	PeerAuthentication = `
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: "default"
spec:
  mtls:
    mode: PERMISSIVE
---
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata:
  name: default
  namespace: {{.ServerNamespace}}
spec:
  selector:
    matchLabels:
      app: server
  mtls:
    mode: STRICT
`
)

func createStrictMtls(t *testing.T, ctx resource.Context, serverNamespace namespace.Instance) {
	tmpl, err := template.New("PeerAuthentication").Parse(PeerAuthentication)
	if err != nil {
		t.Errorf("failed to create template: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"ServerNamespace": serverNamespace.Name()}); err != nil {
		t.Errorf("failed to create template: %v", err)
	}
	if err := ctx.Config().ApplyYAML(serverNamespace.Name(), buf.String()); err != nil {
		t.Errorf("failed to apply sidecar scope: %v", err)
	}
}
