//go:build integ

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

package crl

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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	pkica "istio.io/istio/security/pkg/pki/ca"
)

const (
	secretName  = "cacerts"
	waitTimeout = 2 * time.Minute
)

// CA holds the shared root CA material and one Intermediate CA bundle per cluster.
type CA struct {
	rootCert    *x509.Certificate
	rootKey     *rsa.PrivateKey
	RootCertPEM []byte
	bundles     map[string]*bundle
}

// bundle holds the intermediate CA material and CRL state for a single cluster.
type bundle struct {
	c                   cluster.Cluster
	intermediateCert    *x509.Certificate
	intermediateKey     *rsa.PrivateKey
	intermediateCertPEM []byte
	intermediateKeyPEM  []byte
	certChainPEM        []byte
	intermediateSerial  *big.Int

	crlPEM []byte

	revokedForeignIntermediateSerial *big.Int
}

// GenerateCaCerts creates a shared root CA and one Intermediate CA bundle per cluster in ctx.
// Each bundle is installed into its cluster's cacerts secret.
func GenerateCaCerts(ctx resource.Context) (*CA, error) {
	clusters := ctx.AllClusters()

	rootKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, err
	}
	rootTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1000),
		Subject:               pkix.Name{CommonName: "Root CA"},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            2,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)
	if err != nil {
		return nil, err
	}
	rootCert, err := x509.ParseCertificate(rootDER)
	if err != nil {
		return nil, err
	}

	c := &CA{
		rootCert:    rootCert,
		rootKey:     rootKey,
		RootCertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER}),
		bundles:     make(map[string]*bundle, len(clusters)),
	}

	for i, cl := range clusters {
		b, err := c.newBundle(cl, big.NewInt(int64(2000+i*1000)))
		if err != nil {
			return nil, err
		}
		if err := c.rebuildCRL(b); err != nil {
			return nil, err
		}
		if err := c.installBundle(ctx, b); err != nil {
			return nil, err
		}
		c.bundles[cl.Name()] = b
	}

	return c, nil
}

// RevokeRemoteIntermediate revokes the remote cluster's intermediate CA in the local
// cluster's root CRL and pushes the update to the local cluster's cacerts secret.
func (ca *CA) RevokeRemoteIntermediate(t framework.TestContext, local, remote cluster.Cluster) {
	t.Helper()
	lb := ca.bundles[local.Name()]
	rb := ca.bundles[remote.Name()]
	t.Logf("revoking %s IA in %s root CRL", remote.Name(), local.Name())
	lb.revokedForeignIntermediateSerial = rb.intermediateSerial
	if err := ca.rebuildCRL(lb); err != nil {
		t.Fatalf("failed to rebuild CRL after revoking remote intermediate: %v", err)
	}
	ca.updateCRLInSecret(t, lb)
}

// WaitForCRLPropagation waits until the istio-ca-crl ConfigMap in each namespace on the
// given cluster reflects the cluster's current CRL.
func (ca *CA) WaitForCRLPropagation(t framework.TestContext, c cluster.Cluster, namespaces ...string) {
	t.Helper()
	b := ca.bundles[c.Name()]
	retry.UntilSuccessOrFail(t, func() error {
		return verifyCRLConfigMapsOnCluster(c, namespaces, b.crlPEM)
	}, retry.Timeout(waitTimeout))
}

// ResetCRL clears all revocation state for the given cluster, rebuilds empty CRLs,
// pushes the update to the cluster's cacerts secret, and waits for propagation to namespaces.
func (ca *CA) ResetCRL(t framework.TestContext, c cluster.Cluster, namespaces ...string) {
	t.Helper()
	b := ca.bundles[c.Name()]
	t.Logf("resetting CRL to empty state on %s", c.Name())
	b.revokedForeignIntermediateSerial = nil
	if err := ca.rebuildCRL(b); err != nil {
		t.Fatalf("failed to rebuild CRL during reset on %s: %v", c.Name(), err)
	}
	ca.updateCRLInSecret(t, b)
	retry.UntilSuccessOrFail(t, func() error {
		return verifyCRLConfigMapsOnCluster(c, namespaces, b.crlPEM)
	}, retry.Timeout(waitTimeout))
}

func (ca *CA) newBundle(c cluster.Cluster, iaSerial *big.Int) (*bundle, error) {
	iaKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, err
	}
	iaTemplate := &x509.Certificate{
		SerialNumber:          iaSerial,
		Subject:               pkix.Name{CommonName: "Intermediate CA " + c.Name()},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            0,
	}
	iaDER, err := x509.CreateCertificate(rand.Reader, iaTemplate, ca.rootCert, &iaKey.PublicKey, ca.rootKey)
	if err != nil {
		return nil, err
	}
	iaCert, err := x509.ParseCertificate(iaDER)
	if err != nil {
		return nil, err
	}
	iaCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: iaDER})
	iaKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(iaKey),
	})
	return &bundle{
		c:                   c,
		intermediateCert:    iaCert,
		intermediateKey:     iaKey,
		intermediateCertPEM: iaCertPEM,
		intermediateKeyPEM:  iaKeyPEM,
		intermediateSerial:  iaSerial,
		certChainPEM:        append(iaCertPEM, ca.RootCertPEM...),
	}, nil
}

func (ca *CA) rebuildCRL(b *bundle) error {
	var rootRevoked []x509.RevocationListEntry
	if b.revokedForeignIntermediateSerial != nil {
		rootRevoked = append(rootRevoked, x509.RevocationListEntry{
			SerialNumber:   b.revokedForeignIntermediateSerial,
			RevocationTime: time.Now(),
		})
	}
	rootCRLPEM, err := ca.signRootCRL(rootRevoked)
	if err != nil {
		return err
	}
	intermediateCRLPEM, err := b.signIntermediateCRL(nil)
	if err != nil {
		return err
	}
	b.crlPEM = append(rootCRLPEM, intermediateCRLPEM...)
	return nil
}

func (ca *CA) signRootCRL(revoked []x509.RevocationListEntry) ([]byte, error) {
	now := time.Now()
	crlBytes, err := x509.CreateRevocationList(rand.Reader, &x509.RevocationList{
		SignatureAlgorithm:        ca.rootCert.SignatureAlgorithm,
		Number:                    big.NewInt(1),
		ThisUpdate:                now,
		NextUpdate:                now.Add(30 * 24 * time.Hour),
		Issuer:                    ca.rootCert.Subject,
		RevokedCertificateEntries: revoked,
	}, ca.rootCert, ca.rootKey)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: crlBytes}), nil
}

func (b *bundle) signIntermediateCRL(revoked []x509.RevocationListEntry) ([]byte, error) {
	now := time.Now()
	crlBytes, err := x509.CreateRevocationList(rand.Reader, &x509.RevocationList{
		SignatureAlgorithm:        b.intermediateCert.SignatureAlgorithm,
		Number:                    big.NewInt(1),
		ThisUpdate:                now,
		NextUpdate:                now.Add(30 * 24 * time.Hour),
		Issuer:                    b.intermediateCert.Subject,
		RevokedCertificateEntries: revoked,
	}, b.intermediateCert, b.intermediateKey)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: crlBytes}), nil
}

func (ca *CA) updateCRLInSecret(t framework.TestContext, b *bundle) {
	t.Helper()
	systemNS, err := istio.ClaimSystemNamespace(t)
	if err != nil {
		t.Fatalf("failed to claim system namespace: %v", err)
	}
	if err := upsertSecret(b.c.Kube(), systemNS.Name(), map[string][]byte{
		pkica.CACRLFile: b.crlPEM,
	}); err != nil {
		t.Fatalf("failed to update CRL in secret on %s: %v", b.c.Name(), err)
	}
}

func (ca *CA) installBundle(ctx resource.Context, b *bundle) error {
	systemNs, err := istio.ClaimSystemNamespace(ctx)
	if err != nil {
		return err
	}
	data := map[string][]byte{
		pkica.CACertFile:       b.intermediateCertPEM,
		pkica.CAPrivateKeyFile: b.intermediateKeyPEM,
		pkica.CertChainFile:    b.certChainPEM,
		pkica.RootCertFile:     ca.RootCertPEM,
		pkica.CACRLFile:        b.crlPEM,
	}
	return upsertSecret(b.c.Kube(), systemNs.Name(), data)
}

func verifyCRLConfigMapsOnCluster(c cluster.Cluster, namespaces []string, expectedCRL []byte) error {
	if c.IsExternalControlPlane() {
		return nil
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
	return nil
}

func upsertSecret(kubeClient kubernetes.Interface, namespace string, data map[string][]byte) error {
	patchData := map[string]interface{}{"data": data}
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
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
				Data:       data,
			}
			if _, err = kubeClient.CoreV1().Secrets(namespace).Create(
				context.TODO(), secret, metav1.CreateOptions{},
			); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}
