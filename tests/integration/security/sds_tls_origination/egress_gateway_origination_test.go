package sdstlsorigination

import (
	"fmt"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"reflect"
	"strings"
	"testing"
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
				credName     = "tls-credential-cacert"
				fakeCredName = "fake-tls-credential-cacert"
			)

			var credentialA = sdstlsutil.TLSCredential{
				CaCert: sdstlsutil.RootCertA,
			}
			var fakeCredentialA = sdstlsutil.TLSCredential{
				CaCert: sdstlsutil.FakeRootCertA,
			}
			// Add kubernetes secret to provision key/cert for gateway.
			sdstlsutil.CreateKubeSecret(t, ctx, []string{credName}, "SIMPLE", credentialA, false)
			defer sdstlsutil.DeleteKubeSecret(t, ctx, []string{credName})

			// Add kubernetes secret to provision key/cert for gateway.
			sdstlsutil.CreateKubeSecret(t, ctx, []string{fakeCredName}, "SIMPLE", fakeCredentialA, false)
			defer sdstlsutil.DeleteKubeSecret(t, ctx, []string{fakeCredName})

			internalClient, externalServer, _, serverNamespace := sdstlsutil.SetupEcho(t, ctx, &p)

			// Set up Host Namespace
			host := "server." + serverNamespace.Name() + ".svc.cluster.local"

			testCases := map[string]struct {
				response        []string
				credentialToUse string
				gateway         bool
			}{
				"Simple TLS with Correct Root Cert": {
					response:        []string{response.StatusCodeOK},
					credentialToUse: strings.TrimSuffix(credName, "-cacert"),
					gateway:         true,
				},
				"Simple TLS with Fake Root Cert": {
					response:        []string{response.StatusCodeUnavailable},
					credentialToUse: strings.TrimSuffix(fakeCredName, "-cacert"),
					gateway:         false,
				},
			}

			for name, tc := range testCases {
				t.Run(name, func(t *testing.T) {
					bufDestinationRule := sdstlsutil.CreateDestinationRule(t, serverNamespace, "SIMPLE", tc.credentialToUse)

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
						if !reflect.DeepEqual(codes, tc.response) {
							return fmt.Errorf("got codes %q, expected %q", codes, tc.response)
						}
						for _, r := range resp {
							if _, f := r.RawResponse["Handled-By-Egress-Gateway"]; tc.gateway && !f {
								return fmt.Errorf("expected to be handled by gateway. response: %+v", r.RawResponse)
							}
						}
						return nil
					}, retry.Delay(time.Second), retry.Timeout(5*time.Second))
				})
			}
		})
}
