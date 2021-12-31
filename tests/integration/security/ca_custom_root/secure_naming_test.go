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

package cacustomroot

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/cert"
	"istio.io/istio/tests/integration/security/util/connection"
)

const (
	// The length of the example certificate chain.
	exampleCertChainLength = 3

	defaultIdentityDR = `apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: "service-b-dr"
spec:
  host: "b.NS.svc.cluster.local"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
      subjectAltNames:
      - "spiffe://cluster.local/ns/NS/sa/default"
`
	correctIdentityDR = `apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: "service-b-dr"
spec:
  host: "b.NS.svc.cluster.local"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
      subjectAltNames:
      - "spiffe://cluster.local/ns/NS/sa/b"
`
	nonExistIdentityDR = `apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: "service-b-dr"
spec:
  host: "b.NS.svc.cluster.local"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
      subjectAltNames:
      - "I-do-not-exist"
`
	identityListDR = `apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: "service-b-dr"
spec:
  host: "b.NS.svc.cluster.local"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
      subjectAltNames:
      - "spiffe://cluster.local/ns/NS/sa/a"
      - "spiffe://cluster.local/ns/NS/sa/b"
      - "spiffe://cluster.local/ns/default/sa/default"
      - "I-do-not-exist"
`
)

// TestSecureNaming verifies:
// - The certificate issued by CA to the sidecar is as expected and that strict mTLS works as expected.
// - The plugin CA certs are correctly used in workload mTLS.
// - The CA certificate in the configmap of each namespace is as expected, which
//   is used for data plane to control plane TLS authentication.
// - Secure naming information is respected in the mTLS handshake.
func TestSecureNaming(t *testing.T) {
	framework.NewTest(t).
		Features("security.peer.secure-naming").
		Run(func(t framework.TestContext) {
			// TODO https://github.com/istio/istio/issues/32292
			if t.AllClusters().IsMulticluster() {
				t.Skip()
			}
			istioCfg := istio.DefaultConfigOrFail(t, t)
			testNamespace := apps.Namespace
			namespace.ClaimOrFail(t, t, istioCfg.SystemNamespace)
			// Check that the CA certificate in the configmap of each namespace is as expected, which
			// is used for data plane to control plane TLS authentication.
			retry.UntilSuccessOrFail(t, func() error {
				return checkCACert(t, testNamespace)
			}, retry.Delay(time.Second), retry.Timeout(10*time.Second))

			callCount := 1
			if t.Clusters().IsMulticluster() {
				// so we can validate all clusters are hit
				callCount = util.CallsPerCluster * len(t.Clusters())
			}
			bSet := apps.B.Match(echo.Namespace(testNamespace.Name()))
			for _, cluster := range t.Clusters() {
				t.NewSubTest(fmt.Sprintf("From %s", cluster.StableName())).Run(func(t framework.TestContext) {
					a := apps.A.Match(echo.InCluster(cluster)).Match(echo.Namespace(testNamespace.Name()))[0]
					b := apps.B.Match(echo.InCluster(cluster)).Match(echo.Namespace(testNamespace.Name()))[0]
					t.NewSubTest("mTLS cert validation with plugin CA").
						Run(func(t framework.TestContext) {
							// Verify that the certificate issued to the sidecar is as expected.
							out := cert.DumpCertFromSidecar(t, a, b, "http")
							verifyCertificatesWithPluginCA(t, out)

							// Verify mTLS works between a and b
							callOptions := echo.CallOptions{
								Target:   bSet[0],
								PortName: "http",
								Scheme:   scheme.HTTP,
								Count:    callCount,
							}
							checker := connection.Checker{
								From:          a,
								Options:       callOptions,
								ExpectSuccess: true,
								DestClusters:  bSet.Clusters(),
							}
							checker.CheckOrFail(t)
						})

					secureNamingTestCases := []struct {
						name            string
						destinationRule string
						expectSuccess   bool
					}{
						{
							name:            "connection fails when DR doesn't match SA",
							destinationRule: defaultIdentityDR,
							expectSuccess:   false,
						},
						{
							name:            "connection succeeds when DR matches SA",
							destinationRule: correctIdentityDR,
							expectSuccess:   true,
						},
						{
							name:            "connection fails when DR contains non-matching, non-existing SA",
							destinationRule: nonExistIdentityDR,
							expectSuccess:   false,
						},
						{
							name:            "connection succeeds when SA is in the list of SANs",
							destinationRule: identityListDR,
							expectSuccess:   true,
						},
					}
					for _, tc := range secureNamingTestCases {
						t.NewSubTest(tc.name).
							Run(func(t framework.TestContext) {
								dr := strings.ReplaceAll(tc.destinationRule, "NS", testNamespace.Name())
								t.ConfigIstio().ApplyYAMLOrFail(t, testNamespace.Name(), dr)
								// Verify mTLS works between a and b
								callOptions := echo.CallOptions{
									Target:   bSet[0],
									PortName: "http",
									Scheme:   scheme.HTTP,
									Count:    callCount,
								}
								checker := connection.Checker{
									From:          a,
									Options:       callOptions,
									ExpectSuccess: tc.expectSuccess,
									DestClusters:  bSet.Clusters(),
								}
								if err := retry.UntilSuccess(
									checker.Check, retry.Delay(time.Second), retry.Timeout(15*time.Second), retry.Converge(5)); err != nil {
									t.Fatal(err)
								}
							})
					}
				})
			}
		})
}

func verifyCertificatesWithPluginCA(t framework.TestContext, certs []string) {
	// Verify that the certificate chain length is as expected
	if len(certs) != exampleCertChainLength {
		t.Errorf("expect %v certs in the cert chain but getting %v certs",
			exampleCertChainLength, len(certs))
		return
	}
	var rootCert []byte
	var err error
	if rootCert, err = cert.ReadSampleCertFromFile("root-cert.pem"); err != nil {
		t.Errorf("error when reading expected CA cert: %v", err)
		return
	}
	// Verify that the CA certificate is as expected
	if strings.TrimSpace(string(rootCert)) != strings.TrimSpace(certs[2]) {
		t.Errorf("the actual CA cert is different from the expected. expected: %v, actual: %v",
			strings.TrimSpace(string(rootCert)), strings.TrimSpace(certs[2]))
		return
	}
	t.Log("the CA certificate is as expected")
}

func checkCACert(t framework.TestContext, testNamespace namespace.Instance) error {
	configMapName := "istio-ca-root-cert"
	cm, err := t.Clusters().Default().CoreV1().ConfigMaps(testNamespace.Name()).Get(context.TODO(), configMapName,
		kubeApiMeta.GetOptions{})
	if err != nil {
		return err
	}
	var certData string
	var pluginCert []byte
	var ok bool
	if certData, ok = cm.Data[constants.CACertNamespaceConfigMapDataName]; !ok {
		return fmt.Errorf("CA certificate %v not found", constants.CACertNamespaceConfigMapDataName)
	}
	t.Logf("CA certificate %v found", constants.CACertNamespaceConfigMapDataName)
	if pluginCert, err = cert.ReadSampleCertFromFile("root-cert.pem"); err != nil {
		return err
	}
	if string(pluginCert) != certData {
		return fmt.Errorf("CA certificate (%v) not matching plugin cert (%v)", certData, string(pluginCert))
	}

	return nil
}
