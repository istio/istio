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

// TestMutualTlsOrigination test MUTUAL TLS mode with TLS origination happening at Sidecar
// It uses CredentialName set in DestinationRule API to fetch secrets from k8s API server
func TestSidecarMutualTlsOrigination(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).
		Features("security.egress.mtls.sds").
		Run(func(t framework.TestContext) {
			var (
				credNameGeneric = "mtls-credential-generic"
				fakeCredName    = "fake-mtls-credential"
			)

			// Create a valid kubernetes secret to provision key/cert for sidecar.
			ingressutil.CreateIngressKubeSecretInNamespace(t, credNameGeneric, ingressutil.Mtls, ingressutil.IngressCredential{
				Certificate: file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dns/cert-chain.pem")),
				PrivateKey:  file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dns/key.pem")),
				CaCert:      file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dns/root-cert.pem")),
			}, true, apps.Ns1.Namespace.Name())

			// Authorize only one of the sidecars in the namespace with secret listing permissions.
			serviceAccount := apps.Ns1.A.Config().ServiceAccountName()
			serviceAccountName := serviceAccount[strings.LastIndex(serviceAccount, "/")+1:]
			authorizeSidecar(t, apps.Ns1.Namespace, serviceAccountName)

			// Create a kubernetes secret with an invalid ClientCert
			ingressutil.CreateIngressKubeSecretInNamespace(t, fakeCredName, ingressutil.Mtls, ingressutil.IngressCredential{
				Certificate: file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dns/fake-cert-chain.pem")),
				PrivateKey:  file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dns/key.pem")),
				CaCert:      file.AsStringOrFail(t, path.Join(env.IstioSrc, "tests/testdata/certs/dns/root-cert.pem")),
			}, false, apps.Ns1.Namespace.Name())

			// Set up Host Namespace
			host := apps.External.All.Config().ClusterLocalFQDN()

			testCases := []struct {
				name            string
				statusCode      int
				credentialToUse string
				from            echo.Instances
				drSelector      string
			}{
				// Mutual TLS origination from an authorized sidecar to https endpoint
				{
					name:            "authorized sidecar",
					statusCode:      http.StatusOK,
					credentialToUse: strings.TrimSuffix(credNameGeneric, "-cacert"),
					from:            apps.Ns1.A,
					drSelector:      "a",
				},
				// Mutual TLS origination from an unauthorized sidecar to https endpoint
				// This will result in `ERROR Secret is not supplied by SDS`
				{
					name:            "unauthorized sidecar",
					statusCode:      http.StatusBadRequest,
					credentialToUse: strings.TrimSuffix(credNameGeneric, "-cacert"),
					from:            apps.Ns1.B,
					drSelector:      "b",
				},
				// Mutual TLS origination using an invalid client certificate
				// This will result in `400 BadRequest`
				{
					name:            "invalid client cert",
					statusCode:      http.StatusBadRequest,
					credentialToUse: strings.TrimSuffix(fakeCredName, "-cacert"),
					from:            apps.Ns1.C,
					drSelector:      "c",
				},
			}
			for _, tc := range testCases {
				t.NewSubTest(tc.name).Run(func(t framework.TestContext) {
					newTLSSidecarDestinationRule(t, apps.External.All, "MUTUAL", tc.drSelector, tc.credentialToUse,
						apps.Ns1.Namespace)
					callOpt := newTLSSidecarCallOpts(apps.External.All[0], host, tc.statusCode)
					tc.from[0].CallOrFail(t, callOpt)
				})
			}
		})
}

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
          number: 443
        tls:
          mode: {{.Mode}}
          credentialName: {{.CredentialName}}
          sni: {{ .to.Config.ClusterLocalFQDN }}
`
	t.ConfigIstio().Eval(clientNamespace.Name(), args, dr).ApplyOrFail(t, apply.NoCleanup)
}

func newTLSSidecarCallOpts(to echo.Target, host string, statusCode int) echo.CallOptions {
	return echo.CallOptions{
		To: to,
		Port: echo.Port{
			Protocol:    protocol.HTTP,
			ServicePort: 443,
		},
		HTTP: echo.HTTP{
			Headers: headers.New().WithHost(host).Build(),
		},
		Check: check.And(
			check.NoErrorAndStatus(statusCode),
		),
	}
}
