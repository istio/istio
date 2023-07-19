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

package security

import (
	"fmt"
	"net/http"
	"path"
	"strings"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/util/file"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

// TestSidecarMutualTlsOrigination test MUTUAL TLS mode with TLS origination happening at the sidecar.
// It uses CredentialName set in DestinationRule API to fetch secrets from k8s API server.
func TestSidecarMutualTlsOrigination(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).
		Features("security.egress.mtls.sds").
		Run(func(t framework.TestContext) {
			var (
				credNameGeneric  = "mtls-credential-generic"
				fakeCredName     = "fake-mtls-credential"
				credWithCRL      = "mtls-credential-generic-valid-crl"
				credWithDummyCRL = "mtls-credential-generic-dummy-crl"
			)

			// Create a valid kubernetes secret to provision key/cert for sidecar.
			ingressutil.CreateIngressKubeSecretInNamespace(t, credNameGeneric, ingressutil.Mtls, ingressutil.IngressCredential{
				Certificate: file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dns/cert-chain.pem")),
				PrivateKey:  file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dns/key.pem")),
				CaCert:      file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dns/root-cert.pem")),
			}, true, apps.Ns1.Namespace.Name())

			// Create a kubernetes secret with an invalid ClientCert
			ingressutil.CreateIngressKubeSecretInNamespace(t, fakeCredName, ingressutil.Mtls, ingressutil.IngressCredential{
				Certificate: file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dns/fake-cert-chain.pem")),
				PrivateKey:  file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dns/key.pem")),
				CaCert:      file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dns/root-cert.pem")),
			}, false, apps.Ns1.Namespace.Name())

			// Create a valid kubernetes secret to provision key/cert for sidecar, configured with valid CRL
			ingressutil.CreateIngressKubeSecretInNamespace(t, credWithCRL, ingressutil.Mtls, ingressutil.IngressCredential{
				Certificate: file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dns/cert-chain.pem")),
				PrivateKey:  file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dns/key.pem")),
				CaCert:      file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dns/root-cert.pem")),
				Crl:         file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/ca.crl")),
			}, false, apps.Ns2.Namespace.Name())

			// Create a valid kubernetes secret to provision key/cert for sidecar, configured with dummy CRL
			ingressutil.CreateIngressKubeSecretInNamespace(t, credWithDummyCRL, ingressutil.Mtls, ingressutil.IngressCredential{
				Certificate: file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dns/cert-chain.pem")),
				PrivateKey:  file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dns/key.pem")),
				CaCert:      file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dns/root-cert.pem")),
				Crl:         file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dummy.crl")),
			}, false, apps.Ns2.Namespace.Name())

			// Set up Host Namespace
			host := apps.External.All.Config().ClusterLocalFQDN()

			testCases := []struct {
				name             string
				credentialToUse  string
				from             echo.Instances
				authorizeSidecar bool
				drSelector       string
				expectedResponse ingressutil.ExpectedResponse
			}{
				// Mutual TLS origination from an authorized sidecar to https endpoint
				{
					name:             "authorized sidecar",
					credentialToUse:  credNameGeneric,
					from:             apps.Ns1.A,
					drSelector:       "a",
					authorizeSidecar: true,
					expectedResponse: ingressutil.ExpectedResponse{
						StatusCode: http.StatusOK,
					},
				},
				// Mutual TLS origination from an unauthorized sidecar to https endpoint
				// This will result in `TLS ERROR Secret is not supplied by SDS`
				{
					name:            "unauthorized sidecar",
					credentialToUse: credNameGeneric,
					from:            apps.Ns1.B,
					drSelector:      "b",
					expectedResponse: ingressutil.ExpectedResponse{
						StatusCode:   http.StatusServiceUnavailable,
						ErrorMessage: "Secret is not supplied by SDS",
					},
				},
				// Mutual TLS origination using an invalid client certificate
				// This will result in `TLS ERROR: Secret is not supplied by SDS`
				{
					name:             "invalid client cert",
					credentialToUse:  fakeCredName,
					from:             apps.Ns1.C,
					drSelector:       "c",
					authorizeSidecar: true,
					expectedResponse: ingressutil.ExpectedResponse{
						StatusCode:   http.StatusServiceUnavailable,
						ErrorMessage: "Secret is not supplied by SDS",
					},
				},
				// Mutual TLS origination from an authorized sidecar to https endpoint with a CRL specifying the server certificate as revoked.
				// This will result in `certificate verify failed`
				{
					name:             "valid crl",
					credentialToUse:  credWithCRL,
					from:             apps.Ns2.A,
					drSelector:       "a",
					authorizeSidecar: true,
					expectedResponse: ingressutil.ExpectedResponse{
						StatusCode:   http.StatusServiceUnavailable,
						ErrorMessage: "CERTIFICATE_VERIFY_FAILED",
					},
				},
				// Mutual TLS origination from an authorized sidecar to https endpoint with a CRL with a dummy revoked certificate.
				// Since the certificate in action is not revoked, the communication should not be impacted.
				{
					name:             "dummy crl",
					credentialToUse:  credWithDummyCRL,
					from:             apps.Ns2.B,
					drSelector:       "b",
					authorizeSidecar: true,
					expectedResponse: ingressutil.ExpectedResponse{
						StatusCode: http.StatusOK,
					},
				},
			}
			for _, tc := range testCases {
				t.NewSubTest(tc.name).Run(func(t framework.TestContext) {
					if tc.authorizeSidecar {
						serviceAccount := tc.from.Config().ServiceAccountName()
						serviceAccountName := serviceAccount[strings.LastIndex(serviceAccount, "/")+1:]
						authorizeSidecar(t, tc.from.Config().Namespace, serviceAccountName)
					}
					newTLSSidecarDestinationRule(t, apps.External.All, "MUTUAL", tc.drSelector, tc.credentialToUse,
						tc.from.Config().Namespace)
					callOpt := newTLSSidecarCallOpts(apps.External.All[0], host, tc.expectedResponse)
					tc.from[0].CallOrFail(t, callOpt)
				})
			}
		})
}

// Authorize only specific sidecars in the namespace with secret listing permissions.
func authorizeSidecar(t framework.TestContext, clientNamespace namespace.Instance, serviceAccountName string) {
	args := map[string]any{
		"ServiceAccount": serviceAccountName,
		"Namespace":      clientNamespace.Name(),
	}

	role := `
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: allow-list-secrets
rules:
- apiGroups:
  - ""
  resources:
  - secrets
  verbs:
  - list
`

	rolebinding := `
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: allow-list-secrets-to-{{ .ServiceAccount }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: allow-list-secrets
subjects:
- kind: ServiceAccount
  name: {{ .ServiceAccount }}
  namespace: {{ .Namespace }}
`
	t.ConfigIstio().Eval(clientNamespace.Name(), args, role, rolebinding).ApplyOrFail(t, apply.NoCleanup)
}

func newTLSSidecarDestinationRule(t framework.TestContext, to echo.Instances, destinationRuleMode string,
	workloadSelector string, credentialName string, clientNamespace namespace.Instance,
) {
	args := map[string]any{
		"to":               to,
		"Mode":             destinationRuleMode,
		"CredentialName":   credentialName,
		"WorkloadSelector": workloadSelector,
	}
	se := `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: originate-mtls-for-nginx
spec:
  hosts:
  - "{{ .to.Config.ClusterLocalFQDN }}"
  ports:
  - number: 80
    name: http-port
    protocol: HTTP
    targetPort: 443
  - number: 443
    name: https-port
    protocol: HTTPS
  resolution: DNS
`
	dr := `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: originate-tls-for-server-sds-{{.WorkloadSelector}}
spec:
  workloadSelector:
    matchLabels:
      app: {{.WorkloadSelector}}
  exportTo:
    - .
  host: "{{ .to.Config.ClusterLocalFQDN }}"
  trafficPolicy:
    portLevelSettings:
      - port:
          number: 80
        tls:
          mode: {{.Mode}}
          credentialName: {{.CredentialName}}
          sni: {{ .to.Config.ClusterLocalFQDN }}
`
	t.ConfigIstio().Eval(clientNamespace.Name(), args, se, dr).ApplyOrFail(t, apply.NoCleanup)
}

func newTLSSidecarCallOpts(to echo.Target, host string, exRsp ingressutil.ExpectedResponse) echo.CallOptions {
	return echo.CallOptions{
		To: to,
		Port: echo.Port{
			Protocol: protocol.HTTP,
		},
		HTTP: echo.HTTP{
			Headers: headers.New().WithHost(host).Build(),
		},
		Check: func(result echo.CallResult, err error) error {
			// Check that the error message is expected.
			if err != nil {
				// If expected error message is empty, but we got some error
				// message then it should be treated as error.
				if len(exRsp.ErrorMessage) == 0 {
					return fmt.Errorf("unexpected error: %w", err)
				}
				if !strings.Contains(err.Error(), exRsp.ErrorMessage) {
					return fmt.Errorf("expected response error message %s but got %w and the response code is %+v",
						exRsp.ErrorMessage, err, result.Responses)
				}
				return nil
			}
			return check.And(check.NoErrorAndStatus(exRsp.StatusCode), check.BodyContains(exRsp.ErrorMessage)).Check(result, nil)
		},
	}
}
