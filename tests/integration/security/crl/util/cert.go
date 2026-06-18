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

package util

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/security/pkg/pki/ca"
)

const (
	secretName  = "cacerts"
	waitTimeout = 2 * time.Minute
)

// RootBundle holds a shared root RootBundle and one intermediate Bundle per cluster.
// Use GenerateCaCerts to construct one.
type RootBundle struct {
	rootCert    *x509.Certificate
	rootKey     *rsa.PrivateKey
	rootCertPEM []byte
	bundles     map[string]*IABundle // keyed by cluster name
}

// IABundle holds the intermediate CA material and CRL state for a single cluster.
type IABundle struct {
	c                         cluster.Cluster
	intermediateCert          *x509.Certificate
	intermediateCertPEM       []byte
	intermediateKey           *rsa.PrivateKey
	intermediateKeyPEM        []byte
	intermediateSerial        *big.Int
	revokedIntermediateSerial *big.Int
	certChainPEM              []byte
	crlPEM                    []byte
}

// generateRootCA creates a new self-signed root CA key and certificate.
func generateRootCA() (*x509.Certificate, *rsa.PrivateKey, []byte, error) {
	rootKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, nil, err
	}

	rootTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1000),
		Subject:               pkix.Name{CommonName: "Root CA"},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            1,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)
	if err != nil {
		return nil, nil, nil, err
	}
	rootCert, err := x509.ParseCertificate(rootDER)
	if err != nil {
		return nil, nil, nil, err
	}
	return rootCert, rootKey, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER}), nil
}

// generateIntermediateCA creates an intermediate CA key and certificate signed by the given root.
func generateIntermediateCA(
	rootCert *x509.Certificate,
	rootKey *rsa.PrivateKey,
	commonName string,
	intermediateSerial *big.Int,
) (*x509.Certificate, *rsa.PrivateKey, []byte, []byte, error) {
	intermediateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	intermediateTemplate := &x509.Certificate{
		SerialNumber:          intermediateSerial,
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            0,
	}
	intermediateDER, err := x509.CreateCertificate(
		rand.Reader,
		intermediateTemplate,
		rootCert,
		&intermediateKey.PublicKey,
		rootKey,
	)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	intermediateCert, err := x509.ParseCertificate(intermediateDER)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	intermediateCertPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: intermediateDER,
	})
	intermediateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(intermediateKey),
	})
	return intermediateCert, intermediateKey, intermediateCertPEM, intermediateKeyPEM, nil
}

// signRootCRL signs a root CRL listing the given revoked certificate entries.
func signRootCRL(rootCert *x509.Certificate, rootKey *rsa.PrivateKey, revoked []x509.RevocationListEntry) ([]byte, error) {
	now := time.Now()
	nextUpdate := now.Add(30 * 24 * time.Hour) // 30 days validity
	rootCRLBytes, err := x509.CreateRevocationList(rand.Reader, &x509.RevocationList{
		SignatureAlgorithm:        rootCert.SignatureAlgorithm,
		Number:                    big.NewInt(1),
		ThisUpdate:                now,
		NextUpdate:                nextUpdate,
		ExtraExtensions:           nil,
		Issuer:                    rootCert.Subject,
		RevokedCertificateEntries: revoked,
	}, rootCert, rootKey)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: rootCRLBytes}), nil
}

// signIntermediateCRL signs an empty intermediate CRL (no workload-level revocations).
func signIntermediateCRL(intermediateCert *x509.Certificate, intermediateKey *rsa.PrivateKey) ([]byte, error) {
	now := time.Now()
	crlBytes, err := x509.CreateRevocationList(rand.Reader, &x509.RevocationList{
		SignatureAlgorithm: intermediateCert.SignatureAlgorithm,
		Number:             big.NewInt(1),
		ThisUpdate:         now,
		NextUpdate:         now.Add(30 * 24 * time.Hour),
		Issuer:             intermediateCert.Subject,
	}, intermediateCert, intermediateKey)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: crlBytes}), nil
}

// GenerateCaCerts creates a shared root CA and one intermediate Bundle per cluster in ctx.
// Each Bundle is installed into its cluster's cacerts secret.
func GenerateCaCerts(ctx resource.Context) (*RootBundle, error) {
	clusters := ctx.AllClusters()

	rootCert, rootKey, rootCertPEM, err := generateRootCA()
	if err != nil {
		return nil, err
	}

	rb := &RootBundle{
		rootCert:    rootCert,
		rootKey:     rootKey,
		rootCertPEM: rootCertPEM,
		bundles:     make(map[string]*IABundle, len(clusters)),
	}

	for i, cl := range clusters {
		b, err := rb.newBundle(cl, big.NewInt(int64(2000+i*1000)))
		if err != nil {
			return nil, err
		}
		if err := rb.rebuildCRL(b); err != nil {
			return nil, err
		}
		if err := rb.createCaCertsSecret(ctx, b); err != nil {
			return nil, err
		}
		rb.bundles[cl.Name()] = b
	}

	return rb, nil
}

// Bundle returns the intermediate CA bundle for the given cluster.
func (rb *RootBundle) Bundle(c cluster.Cluster) *IABundle {
	return rb.bundles[c.Name()]
}

// RevokeOwnIntermediate revokes the given cluster's own intermediate CA in its root CRL
// and pushes the update to that cluster's cacerts secret.
func (rb *RootBundle) RevokeOwnIntermediate(t framework.TestContext, c cluster.Cluster) {
	t.Helper()
	rb.RevokeRemoteIntermediate(t, c, c)
}

// RevokeRemoteIntermediate revokes the remote cluster's intermediate CA in the local
// cluster's root CRL and pushes the update to the local cluster's cacerts secret.
func (rb *RootBundle) RevokeRemoteIntermediate(t framework.TestContext, local, remote cluster.Cluster) {
	t.Helper()
	localBundle := rb.bundles[local.Name()]
	remoteBundle := rb.bundles[remote.Name()]
	t.Logf("revoking %s IA in %s root CRL", remote.Name(), local.Name())
	localBundle.revokedIntermediateSerial = remoteBundle.intermediateSerial
	if err := rb.rebuildCRL(localBundle); err != nil {
		t.Fatalf("failed to rebuild CRL after revoking intermediate: %v", err)
	}
	rb.updateCRLInSecret(t, localBundle)
}

// WaitForCRLPropagation waits until the istio-ca-crl ConfigMap in ztunnel's namespace on the
// given cluster reflects the cluster's current CRL.
func (rb *RootBundle) WaitForCRLPropagation(t framework.TestContext, c cluster.Cluster) {
	t.Helper()
	istioCfg := istio.DefaultConfigOrFail(t, t)
	b := rb.bundles[c.Name()]
	retry.UntilSuccessOrFail(t, func() error {
		return verifyCRLConfigMaps([]string{istioCfg.ZtunnelNamespace}, b.crlPEM, c)
	}, retry.Timeout(waitTimeout))
}

// ResetCRL clears all revocation state for the given cluster, rebuilds empty CRLs, pushes
// the update to the cluster's cacerts secret, and waits for propagation to ztunnel's namespace.
func (rb *RootBundle) ResetCRL(t framework.TestContext, c cluster.Cluster) {
	t.Helper()
	istioCfg := istio.DefaultConfigOrFail(t, t)
	b := rb.bundles[c.Name()]
	t.Logf("resetting CRL to empty state on %s", c.Name())
	b.revokedIntermediateSerial = nil
	if err := rb.rebuildCRL(b); err != nil {
		t.Fatalf("failed to rebuild CRL during reset on %s: %v", c.Name(), err)
	}
	rb.updateCRLInSecret(t, b)
	retry.UntilSuccessOrFail(t, func() error {
		return verifyCRLConfigMaps([]string{istioCfg.ZtunnelNamespace}, b.crlPEM, c)
	}, retry.Timeout(waitTimeout))
}

func (rb *RootBundle) newBundle(c cluster.Cluster, iaSerial *big.Int) (*IABundle, error) {
	iaCert, iaKey, iaCertPEM, iaKeyPEM, err := generateIntermediateCA(
		rb.rootCert, rb.rootKey, "Intermediate CA "+c.Name(), iaSerial,
	)
	if err != nil {
		return nil, err
	}
	return &IABundle{
		c:                   c,
		intermediateCert:    iaCert,
		intermediateKey:     iaKey,
		intermediateCertPEM: iaCertPEM,
		intermediateKeyPEM:  iaKeyPEM,
		intermediateSerial:  iaSerial,
		certChainPEM:        append(iaCertPEM, rb.rootCertPEM...),
	}, nil
}

func (rb *RootBundle) rebuildCRL(b *IABundle) error {
	var rootRevoked []x509.RevocationListEntry
	if b.revokedIntermediateSerial != nil {
		rootRevoked = append(rootRevoked, x509.RevocationListEntry{
			SerialNumber:   b.revokedIntermediateSerial,
			RevocationTime: time.Now(),
		})
	}
	rootCRLPEM, err := signRootCRL(rb.rootCert, rb.rootKey, rootRevoked)
	if err != nil {
		return err
	}
	intermediateCRLPEM, err := signIntermediateCRL(b.intermediateCert, b.intermediateKey)
	if err != nil {
		return err
	}
	b.crlPEM = append(rootCRLPEM, intermediateCRLPEM...)
	return nil
}

func (rb *RootBundle) updateCRLInSecret(t framework.TestContext, b *IABundle) {
	t.Helper()
	systemNS, err := istio.ClaimSystemNamespace(t)
	if err != nil {
		t.Fatalf("failed to claim system namespace: %v", err)
	}
	if err := upsertSecret(b.c.Kube(), systemNS.Name(), map[string][]byte{
		ca.CACRLFile: b.crlPEM,
	}); err != nil {
		t.Fatalf("failed to update CRL in secret on %s: %v", b.c.Name(), err)
	}
}

func (rb *RootBundle) createCaCertsSecret(
	ctx resource.Context,
	bundle *IABundle,
) error {
	systemNs, err := istio.ClaimSystemNamespace(ctx)
	if err != nil {
		return err
	}
	data := map[string][]byte{
		ca.CACertFile:       bundle.intermediateCertPEM,
		ca.CAPrivateKeyFile: bundle.intermediateKeyPEM,
		ca.CertChainFile:    bundle.certChainPEM,
		ca.RootCertFile:     rb.rootCertPEM,
		ca.CACRLFile:        bundle.crlPEM,
	}

	if err := upsertSecret(bundle.c.Kube(), systemNs.Name(), data); err != nil {
		return err
	}
	// delete any stale istio-ca-root-cert ConfigMap from a previous test run so istiod
	// recreates it from the new cacerts secret on startup.
	err = bundle.c.Kube().CoreV1().ConfigMaps(systemNs.Name()).Delete(context.TODO(), features.CACertConfigMapName,
		metav1.DeleteOptions{})
	if err == nil {
		log.Infof("configmap %v is deleted", features.CACertConfigMapName)
	} else {
		log.Warnf("configmap %v may not exist and the deletion returns err (%v)",
			features.CACertConfigMapName, err)
	}
	return nil
}

func upsertSecret(
	kubeClient kubernetes.Interface,
	namespace string,
	data map[string][]byte,
) error {
	patchData := map[string]interface{}{
		"data": data,
	}
	patchBytes, err := json.Marshal(patchData)
	if err != nil {
		return err
	}

	if _, err = kubeClient.CoreV1().Secrets(namespace).Patch(
		context.TODO(),
		secretName,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
	); err != nil {
		if errors.IsNotFound(err) {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
				},
				Data: data,
			}
			if _, err = kubeClient.CoreV1().Secrets(namespace).Create(
				context.TODO(),
				secret,
				metav1.CreateOptions{},
			); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	return nil
}

func WaitForCRLUpdate(t framework.TestContext, namespaces []string, bundle *IABundle, instances ...echo.Instance) {
	t.Helper()
	startTime := time.Now()
	t.Logf("waiting for CRL update in namespaces: %s", strings.Join(namespaces, ", "))
	defer func() {
		t.Logf("CRL wait executed in %v", time.Since(startTime))
	}()

	// verify crl ConfigMaps are updated
	// istiod distributes ConfigMaps to its config cluster's namespaces, so we poll bundle.c.Config() cluster
	retry.UntilSuccessOrFail(t, func() error {
		return verifyCRLConfigMaps(namespaces, bundle.crlPEM, bundle.c.Config())
	}, retry.Timeout(waitTimeout))

	// force pod annotation update to trigger ConfigMap volume refresh
	retry.UntilSuccessOrFail(t, func() error {
		return forceVolumeRefresh(t, instances...)
	}, retry.Timeout(waitTimeout))
}

func verifyCRLConfigMaps(namespaces []string, expectedCRL []byte, clusters ...cluster.Cluster) error {
	for _, c := range clusters {
		if c.IsExternalControlPlane() {
			// we replicate the crl configmap to all clusters where workloads are running. So we can skip this cluster.
			continue
		}
		for _, ns := range namespaces {
			cm, err := c.Kube().CoreV1().ConfigMaps(ns).Get(
				context.TODO(),
				controller.CRLNamespaceConfigMap,
				metav1.GetOptions{},
			)
			if err != nil {
				return fmt.Errorf("failed to get ConfigMap in %s/%s: %w", c.Name(), ns, err)
			}
			if cm.Data[constants.CACRLNamespaceConfigMapDataName] != string(expectedCRL) {
				return fmt.Errorf("CRL not updated in %s/%s", c.Name(), ns)
			}
		}
	}
	return nil
}

func forceVolumeRefresh(t framework.TestContext, instances ...echo.Instance) error {
	for _, instance := range instances {
		for _, w := range instance.WorkloadsOrFail(t) {
			patch := fmt.Sprintf(`{"metadata":{"annotations":{"crl-refresh":"crl-%d"}}}`, time.Now().Unix())
			_, err := w.Cluster().Kube().CoreV1().Pods(instance.NamespaceName()).Patch(
				context.TODO(),
				w.PodName(),
				types.MergePatchType,
				[]byte(patch),
				metav1.PatchOptions{},
			)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
