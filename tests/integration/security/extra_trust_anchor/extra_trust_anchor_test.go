//  Copyright 2019 Istio Authors
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

package extratrustanchor

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"github.com/hashicorp/go-multierror"
	kubeApiCore "k8s.io/api/core/v1"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/citadel"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/security/pkg/k8s/configmap"
	"istio.io/istio/security/pkg/k8s/controller"
	pkiutil "istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/connection"
	"istio.io/istio/tests/integration/security/util/secret"
)

func serialNumbersFromCert(cert []byte) []string {
	var sn []string
	for len(cert) > 0 {
		var block *pem.Block
		block, cert = pem.Decode(cert)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		sn = append(sn, cert.SerialNumber.String())
	}
	return sn
}

func strLess(a, b string) bool { return a < b }

type operation int

const (
	opAdd operation = iota
	opRemove
)

func createConfigMapOrFail(t test.Failer, env *kube.Environment, ns namespace.Instance, cm *kubeApiCore.ConfigMap) *kubeApiCore.ConfigMap {
	t.Helper()

	created, err := env.CreateConfigMap(ns.Name(), cm)
	if err != nil {
		t.Fatalf("Could not create configmap %v in ns %v: %v", cm.Name, ns, err)
	}
	return created
}

func deleteConfigMapOrFail(t test.Failer, env *kube.Environment, name string, namespace namespace.Instance) {
	t.Helper()

	if err := env.DeleteConfigMap(namespace.Name(), name); err != nil {
		t.Fatalf("Could not delete configmap %v in namespace %v: %v", name, namespace, err)
	}
}

const waitDelay = time.Second

func updateAndCheck(t *testing.T, env *kube.Environment, c citadel.Instance, cm *kubeApiCore.ConfigMap, ns namespace.Instance, op operation, roots []string) {
	t.Helper()

	if cm != nil {
		if op == opAdd {
			createConfigMapOrFail(t, env, ns, cm)
		} else {
			deleteConfigMapOrFail(t, env, cm.GetName(), ns)
		}

		// ConfigMap updates are fast. Give Citadel a second to refresh its internal state.
		time.Sleep(waitDelay)

		// Secret updates are slow sometimes (1-2 minutes). Force citadel to regenerate the
		// secret with the new roots after a configmap change.
		c.DeleteSecretOrFail(t, ns, citadel.SecretName)
	}

	updated := c.WaitForSecretToExistOrFail(t, ns, citadel.SecretName)
	secret.ExamineOrFail(t, updated)

	// Verify the updated secret's roots match citadel's root concatenated with any additional trusted roots.
	gotRoots := updated.Data[controller.RootCertID]
	wantRoots := []byte(strings.Join(roots, "\n"))

	gotSN := serialNumbersFromCert(gotRoots)
	wantSN := serialNumbersFromCert(wantRoots)

	if diff := cmp.Diff(gotSN, wantSN, cmpopts.SortSlices(strLess)); diff != "" {
		t.Fatalf("wrong set of roots (by serial number): \n gotRoots %v \nwantRoots %v \ndiff %v", gotSN, wantSN, diff)
	}
}

func makeConfigMap(name string, labels map[string]string, data map[string]string) *kubeApiCore.ConfigMap {
	return &kubeApiCore.ConfigMap{
		ObjectMeta: kubeApiMeta.ObjectMeta{Name: name, Labels: labels},
		Data:       data,
	}
}

func getCitadelRootCertOrFail(t test.Failer, env *kube.Environment, ns namespace.Instance) string {
	t.Helper()

	cm, err := env.GetConfigMap(ns.Name(), configmap.IstioSecurityConfigMapName)
	if err != nil {
		t.Fatalf("Failed to get %v configmap: %v", configmap.IstioSecurityConfigMapName, err)
	}
	encoded := cm.Data[configmap.CATLSRootCertName]
	bytes, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("Failed to decode the CA's root cert: %v", err)
	}
	return strings.TrimRight(string(bytes), "\n")
}

const (
	keySize = 2048
	ttl     = time.Hour * 24
)

// Derived from Citadel's cert-signing code, but without all of the full dependencies on a CA and controller.
func createFakeCitadelSecret(saName string, ns namespace.Instance, env *kube.Environment, extraRoot []byte) ([]byte, error) {
	caCertOptions := pkiutil.CertOptions{
		TTL:          ttl,
		Org:          fmt.Sprintf("cluster-local (%s)", uuid.New().String()),
		IsCA:         true,
		IsSelfSigned: true,
		RSAKeySize:   keySize,
	}
	rootCert, rootKey, err := pkiutil.GenCertKeyFromOptions(caCertOptions)
	if err != nil {
		return nil, err
	}
	rootList := make([]byte, len(rootCert))
	copy(rootList, rootCert)
	rootList = append(rootList, extraRoot...)
	keyCertBundle, err := pkiutil.NewVerifiedKeyCertBundleFromPem(rootCert, rootKey, nil, rootList)
	if err != nil {
		return nil, err
	}
	signingCert, signingKey, _, _ := keyCertBundle.GetAll()

	uri := spiffe.MustGenSpiffeURI(ns.Name(), saName)
	serviceCertOptions := pkiutil.CertOptions{
		Host:       uri,
		RSAKeySize: keySize,
	}
	csrPEM, keyPEM, err := pkiutil.GenCSR(serviceCertOptions)
	if err != nil {
		return nil, err
	}
	csr, err := pkiutil.ParsePemEncodedCSR(csrPEM)
	if err != nil {
		return nil, err
	}

	subjectIDs := strings.Split(uri, ",")
	certBytes, err := pkiutil.GenCertFromCSR(csr, signingCert, csr.PublicKey, *signingKey, subjectIDs, ttl, false)
	if err != nil {
		return nil, err
	}
	block := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	}
	certPem := pem.EncodeToMemory(block)

	s := &kubeApiCore.Secret{
		ObjectMeta: kubeApiMeta.ObjectMeta{
			Name:        controller.GetSecretName(saName),
			Annotations: map[string]string{controller.ServiceAccountNameAnnotationKey: saName},
		},
		Data: map[string][]byte{
			controller.CertChainID:  certPem,
			controller.PrivateKeyID: keyPEM,
			controller.RootCertID:   rootList,
		},
	}
	if err := env.CreateSecret(ns.Name(), s); err != nil {
		return nil, err
	}
	return rootCert, nil
}

func createFakeCitadelSecretOrFail(t test.Failer, saName string, ns namespace.Instance, env *kube.Environment, extraRoot []byte) []byte {
	t.Helper()

	rootCert, err := createFakeCitadelSecret(saName, ns, env, extraRoot)
	if err != nil {
		t.Fatal("failed to create fake citadel secret for serviceaccount=%v in ns=%v: %v", saName, ns.Name())
	}
	return rootCert
}

func currentEnvoyEpoch(t test.Failer, e echo.Instance) uint32 {
	t.Helper()

	w, err := e.Workloads()
	if err != nil {
		t.Fatalf("failed to get echo instance %v's sidecar epoch: %v", e.ID(), err)
	}
	serverInfo, err := w[0].Sidecar().Info()
	if err != nil {
		t.Fatal(err)
	}
	return serverInfo.CommandLineOptions.RestartEpoch
}

const meshPolicyStrictMTLS = `
apiVersion: authentication.istio.io/v1alpha1
kind: MeshPolicy
metadata:
  name: "default"
spec:
  peers:
  - mtls: 
      mode: STRICT
`

func destinationRuleServiceToServiceMTLS(ns namespace.Instance) string {
	const format = `apiVersion: "networking.istio.io/v1alpha3"
kind: "DestinationRule"
metadata:
  name: "default"
spec:
  host: "*.%v.svc.cluster.local"
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
`
	return fmt.Sprintf(format, ns.Name())
}

const (
	localServiceA  = "a"
	localServiceB  = "b"
	remoteServiceA = "remote-a"
	remoteServiceB = "remote-b"
)

// TODO(ayj) revisit this test when the integration framework supports multi-cluster.
func TestExtraTrustAnchorMultiRootTraffic(t *testing.T) {
	framework.Run(t, func(ctx framework.TestContext) {
		c := citadel.NewOrFail(t, ctx, citadel.Config{Istio: ist})
		systemNamespace := namespace.ClaimOrFail(t, ctx, ist.Settings().SystemNamespace)
		env := ctx.Environment().(*kube.Environment)

		// local namespace certs managed by the in-cluster citadel
		localNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
			Prefix: "local",
			Inject: true,
			Labels: map[string]string{controller.NamespaceManagedLabel: systemNamespace.Name()},
		})

		// use namespaces to simulate remote clusters. Configure Citadel to ignore these remote namespaces and
		// manually create certs signed by different per-namespace root key/certs.
		remoteNamespaceA := namespace.NewOrFail(t, ctx, namespace.Config{
			Prefix: remoteServiceA,
			Inject: true,
			Labels: map[string]string{controller.NamespaceOverrideLabel: "false"},
		})
		remoteNamespaceB := namespace.NewOrFail(t, ctx, namespace.Config{
			Prefix: remoteServiceB,
			Inject: true,
			Labels: map[string]string{controller.NamespaceOverrideLabel: "false"},
		})

		// Use mTLS for service-to-service traffic. Use no auth for proxy-to-control-plane
		// so the simulated remote service's proxys with localA different root CAs can connect to pilot and mixer.
		g.ApplyConfigOrFail(t, systemNamespace, meshPolicyStrictMTLS) // namespace
		ctx.WhenDone(func() error {
			return g.DeleteConfig(systemNamespace, meshPolicyStrictMTLS)
		})
		for _, namespace := range []namespace.Instance{localNamespace, remoteNamespaceA, remoteNamespaceB} {
			g.ApplyConfigOrFail(t, namespace, destinationRuleServiceToServiceMTLS(namespace))
			ctx.WhenDone(func() error {
				return g.DeleteConfig(namespace, destinationRuleServiceToServiceMTLS(namespace))
			})
		}

		// create the per-namespace root cert and service key/cert.
		localRoot := getCitadelRootCertOrFail(t, env, systemNamespace)
		remote0RootCert := createFakeCitadelSecretOrFail(t, remoteServiceA, remoteNamespaceA, env, []byte(localRoot))
		remote1RootCert := createFakeCitadelSecretOrFail(t, remoteServiceB, remoteNamespaceB, env, []byte(localRoot))

		var localA, localB, remoteA, remoteB echo.Instance
		echoboot.NewBuilderOrFail(t, ctx).
			With(&localA, util.EchoConfig(localServiceA, localNamespace, false, nil, g, p)).
			With(&localB, util.EchoConfig(localServiceB, localNamespace, false, nil, g, p)).
			With(&remoteA, util.EchoConfig(remoteServiceA, remoteNamespaceA, false, nil, g, p)).
			With(&remoteB, util.EchoConfig(remoteServiceB, remoteNamespaceB, false, nil, g, p)).
			BuildOrFail(t)

		// negative test - verify local-to-local passes and local-to-remote fails.
		checkers := []connection.Checker{{
			From: localA,
			Options: echo.CallOptions{
				Target:   localB,
				PortName: "http",
				Scheme:   scheme.HTTP,
			},
			ExpectSuccess: true,
		}, {
			From: localA,
			Options: echo.CallOptions{
				Target:   remoteA,
				PortName: "http",
				Scheme:   scheme.HTTP,
			},
			ExpectSuccess: false,
		}, {
			From: localA,
			Options: echo.CallOptions{
				Target:   remoteB,
				PortName: "http",
				Scheme:   scheme.HTTP,
			},
			ExpectSuccess: false,
		}}
		for _, checker := range checkers {
			retry.UntilSuccessOrFail(t, checker.Check, retry.Delay(time.Second), retry.Timeout(10*time.Second))
		}

		envoyEpochA := currentEnvoyEpoch(t, localA)
		envoyEpochB := currentEnvoyEpoch(t, localB)

		// establish trust between the local cluster and the simulated remote clusters.
		trustAnchorRemoteA := &kubeApiCore.ConfigMap{
			ObjectMeta: kubeApiMeta.ObjectMeta{
				Name:   remoteServiceA,
				Labels: map[string]string{controller.ExtraTrustAnchorsLabel: "true"},
			},
			Data: map[string]string{remoteServiceA: string(remote0RootCert)},
		}
		trustAnchorRemoteB := &kubeApiCore.ConfigMap{
			ObjectMeta: kubeApiMeta.ObjectMeta{
				Name:   remoteServiceB,
				Labels: map[string]string{controller.ExtraTrustAnchorsLabel: "true"},
			},
			Data: map[string]string{remoteServiceB: string(remote1RootCert)},
		}
		createConfigMapOrFail(t, env, systemNamespace, trustAnchorRemoteA)
		createConfigMapOrFail(t, env, systemNamespace, trustAnchorRemoteB)
		ctx.WhenDone(func() error {
			var errs error
			if err := env.DeleteConfigMap(systemNamespace.Name(), remoteServiceA); err != nil {
				errs = multierror.Append(errs, err)
			}
			if err := env.DeleteConfigMap(systemNamespace.Name(), remoteServiceB); err != nil {
				errs = multierror.Append(errs, err)
			}
			return errs
		})

		// ConfigMap updates are fast. Give Citadel localA second to refresh its internal
		// state versionBeforeUpdate forcing secret rotation.
		time.Sleep(waitDelay)

		// force citadel to re-create secrets with the updated list of roots and wait for
		// pilot-agenta to restart they envoys with the new certs.
		for _, service := range []string{localServiceA, localServiceB} {
			secret := controller.GetSecretName(service)
			c.DeleteSecretOrFail(t, localNamespace, secret)
			c.WaitForSecretToExistOrFail(t, localNamespace, secret)
		}
		retry.UntilSuccessOrFail(t, func() error {
			if epoch := currentEnvoyEpoch(t, localA); epoch == envoyEpochA {
				return fmt.Errorf("echo %v's sidecar restart epoch hasn't changed from %v", localA.ID(), envoyEpochA)
			}
			if epoch := currentEnvoyEpoch(t, localB); epoch == envoyEpochB {
				return fmt.Errorf("echo %v's sidecar restart epoch hasn't changed from %v", localB.ID(), envoyEpochB)
			}
			return nil
		}, retry.Delay(time.Second), retry.Timeout(5*time.Minute))

		// verify local-to-local and local-to-remote traffic passes
		checkers = []connection.Checker{{
			From: localA,
			Options: echo.CallOptions{
				Target:   localB,
				PortName: "http",
				Scheme:   scheme.HTTP,
			},
			ExpectSuccess: true,
		}, {
			From: localA,
			Options: echo.CallOptions{
				Target:   remoteA,
				PortName: "http",
				Scheme:   scheme.HTTP,
			},
			ExpectSuccess: true,
		}, {
			From: localA,
			Options: echo.CallOptions{
				Target:   remoteB,
				PortName: "http",
				Scheme:   scheme.HTTP,
			},
			ExpectSuccess: true,
		}}
		for _, checker := range checkers {
			// extra time in case secret/cert propagation takes more time.
			retry.UntilSuccessOrFail(t, checker.Check, retry.Delay(time.Second), retry.Timeout(5*time.Minute))
		}
	})
}

// Test assumes --max-workload-cert-ttl is 90 days.
func TestExtraTrustAnchorSecretDistribution(t *testing.T) {
	framework.Run(t, func(ctx framework.TestContext) {
		c := citadel.NewOrFail(t, ctx, citadel.Config{Istio: ist})
		systemNamespace := namespace.ClaimOrFail(t, ctx, ist.Settings().SystemNamespace)
		env := ctx.Environment().(*kube.Environment)

		var (
			labelsTrue  = map[string]string{controller.ExtraTrustAnchorsLabel: "true"}
			labelsFalse = map[string]string{controller.ExtraTrustAnchorsLabel: "false"}
			labelsNone  = map[string]string{}

			root1Data     = map[string]string{"root1": root1}
			root2Data     = map[string]string{"root2": root2}
			root3Data     = map[string]string{"root3": root3}
			root4And5Data = map[string]string{"root4": root4, "root5": root5}

			cmMissingLabelRoot1      = makeConfigMap("root1", labelsNone, root1Data)
			cmLabeledFalseRoot2      = makeConfigMap("root2", labelsFalse, root2Data)
			cmSingleRootRoot3        = makeConfigMap("root3", labelsTrue, root3Data)
			cmMultipleRootsRoot4And5 = makeConfigMap("root4and5", labelsTrue, root4And5Data)

			rootCA = getCitadelRootCertOrFail(t, env, systemNamespace)
		)

		steps := []struct {
			name      string
			configmap *kubeApiCore.ConfigMap
			op        operation
			roots     []string
		}{
			{
				name:  "default root",
				roots: []string{rootCA},
			},
			{
				name:      "add configmap with missing label",
				op:        opAdd,
				configmap: cmMissingLabelRoot1,
				roots:     []string{rootCA},
			},
			{
				name:      "add config map wrong label",
				op:        opAdd,
				configmap: cmLabeledFalseRoot2,
				roots:     []string{rootCA},
			},
			{
				name:      "add configmap with single root",
				op:        opAdd,
				configmap: cmSingleRootRoot3,
				roots:     []string{rootCA, root3},
			},
			{
				name:      "add configmap with multiple roots",
				op:        opAdd,
				configmap: cmMultipleRootsRoot4And5,
				roots:     []string{rootCA, root3, root4, root5}, // plus root3 from previous step
			},
			{
				name:      "remove configmap with single root",
				op:        opRemove,
				configmap: cmSingleRootRoot3,
				roots:     []string{rootCA, root4, root5},
			},
			{
				name:      "remove configmap with multiple roots",
				op:        opRemove,
				configmap: cmMultipleRootsRoot4And5,
				roots:     []string{rootCA},
			},
		}

		for i, step := range steps {
			t.Run(fmt.Sprintf("[%v] %v", i, step.name), func(tt *testing.T) {
				updateAndCheck(tt, env, c, step.configmap, systemNamespace, step.op, step.roots)
			})
		}

	})
}
