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

	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/security/pkg/pki/ca"
)

const (
	secretName         = "cacerts"
	waitTimeout        = 2 * time.Minute
	proxyContainerName = "istio-proxy"
)

type Bundle struct {
	rootCertPEM         []byte
	rootKeyPEM          []byte
	intermediateCertPEM []byte
	intermediateKeyPEM  []byte
	certChainPEM        []byte
	crlPEM              []byte
	rootCRLPEM          []byte
	intermediateCRLPEM  []byte

	// internal state for revocation
	rootCert           *x509.Certificate
	rootKey            *rsa.PrivateKey
	intermediateCert   *x509.Certificate
	intermediateKey    *rsa.PrivateKey
	intermediateSerial *big.Int
	revoked            bool
}

func GenerateBundle(ctx resource.Context) (*Bundle, error) {
	bundle := &Bundle{}

	rootKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, err
	}

	// generate root cert and key
	rootSerial := big.NewInt(1000)
	rootTemplate := &x509.Certificate{
		SerialNumber:          rootSerial,
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
		return nil, err
	}
	bundle.rootCert, err = x509.ParseCertificate(rootDER)
	if err != nil {
		return nil, err
	}
	bundle.rootKey = rootKey
	bundle.rootCertPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: rootDER,
	})
	bundle.rootKeyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(rootKey),
	})

	// generate intermediate cert and key
	intermediateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, err
	}
	intermediateSerial := big.NewInt(2000)
	intermediateTemplate := &x509.Certificate{
		SerialNumber:          intermediateSerial,
		Subject:               pkix.Name{CommonName: "Intermediate CA"},
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
		bundle.rootCert,
		&intermediateKey.PublicKey,
		rootKey,
	)
	if err != nil {
		return nil, err
	}
	bundle.intermediateCert, err = x509.ParseCertificate(intermediateDER)
	if err != nil {
		return nil, err
	}
	bundle.intermediateKey = intermediateKey
	bundle.intermediateSerial = intermediateSerial
	bundle.intermediateCertPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: intermediateDER,
	})
	bundle.intermediateKeyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(intermediateKey),
	})

	bundle.certChainPEM = append(bundle.intermediateCertPEM, bundle.rootCertPEM...)

	// generate initial CRL with no revoked certs
	if err = bundle.generateCRL(); err != nil {
		return nil, err
	}

	// create cacert secret in istio-system namespace
	if err = createCustomCASecret(ctx, bundle); err != nil {
		return nil, err
	}

	return bundle, nil
}

// generateCRL generates a combined CRL for root and intermediate with no revoked certificates
func (b *Bundle) generateCRL() error {
	now := time.Now()
	nextUpdate := now.Add(30 * 24 * time.Hour) // 30 days validity

	// root CRL (no revoked certs)
	rootCRLBytes, err := x509.CreateRevocationList(
		rand.Reader,
		&x509.RevocationList{
			SignatureAlgorithm:        b.rootCert.SignatureAlgorithm,
			Number:                    big.NewInt(1),
			ThisUpdate:                now,
			NextUpdate:                nextUpdate,
			ExtraExtensions:           nil,
			Issuer:                    b.rootCert.Subject,
			RevokedCertificateEntries: nil,
		},
		b.rootCert,
		b.rootKey,
	)
	if err != nil {
		return err
	}
	rootCRLPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "X509 CRL",
			Bytes: rootCRLBytes,
		},
	)

	// generate Intermediate CRL
	intermediateCRLBytes, err := x509.CreateRevocationList(
		rand.Reader,
		&x509.RevocationList{
			SignatureAlgorithm:        b.intermediateCert.SignatureAlgorithm,
			Number:                    big.NewInt(1),
			ThisUpdate:                now,
			NextUpdate:                nextUpdate,
			Issuer:                    b.intermediateCert.Subject,
			RevokedCertificateEntries: nil,
		},
		b.intermediateCert,
		b.intermediateKey,
	)
	if err != nil {
		return err
	}
	intermediateCRLPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "X509 CRL",
			Bytes: intermediateCRLBytes,
		},
	)
	b.intermediateCRLPEM = intermediateCRLPEM

	// combine both CRLs
	b.setCombineCRL(rootCRLPEM, b.intermediateCRLPEM)

	return nil
}

// getUpdatedRootCRL generates root CRL with revoked intermediate cert
func (b *Bundle) getUpdatedRootCRL(t framework.TestContext) ([]byte, error) {
	t.Helper()
	if !b.revoked {
		// if the intermediate cert is not revoked, return the existing root CRL
		return b.rootCRLPEM, nil
	}

	now := time.Now()
	nextUpdate := now.Add(30 * 24 * time.Hour)

	revokedIntermediates := []x509.RevocationListEntry{
		{
			SerialNumber:   b.intermediateSerial,
			RevocationTime: now,
		},
	}

	rootCRLBytes, err := x509.CreateRevocationList(
		rand.Reader,
		&x509.RevocationList{
			SignatureAlgorithm:        b.rootCert.SignatureAlgorithm,
			Number:                    big.NewInt(1),
			ThisUpdate:                now,
			NextUpdate:                nextUpdate,
			ExtraExtensions:           nil,
			Issuer:                    b.rootCert.Subject,
			RevokedCertificateEntries: revokedIntermediates,
		},
		b.rootCert,
		b.rootKey,
	)
	if err != nil {
		return nil, err
	}
	rootCRLPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "X509 CRL",
			Bytes: rootCRLBytes,
		},
	)

	return rootCRLPEM, nil
}

func (b *Bundle) setCombineCRL(rootCRLPEM, intermediateCRLPEM []byte) {
	b.crlPEM = append(rootCRLPEM, intermediateCRLPEM...)
}

// RevokeIntermediate revokes the intermediate certificate and updates the CRL in the secret.
func RevokeIntermediate(t framework.TestContext, bundle *Bundle) {
	t.Helper()
	t.Logf("revoking intermediate certificate")
	bundle.revoked = true

	// get updated root CRL with revoked intermediate cert
	rootCRLPEM, err := bundle.getUpdatedRootCRL(t)
	if err != nil {
		t.Fatalf("failed to get updated root CRL: %v", err)
	}

	// combine both CRLs
	bundle.setCombineCRL(rootCRLPEM, bundle.intermediateCRLPEM)

	systemNS, err := istio.ClaimSystemNamespace(t)
	if err != nil {
		t.Fatalf("failed to claim system namespace: %v", err)
	}

	for _, kubeCluster := range t.AllClusters() {
		data := map[string][]byte{
			ca.CACRLFile: bundle.crlPEM,
		}

		err = upsertSecret(kubeCluster.Kube(), systemNS.Name(), data)
		if err != nil {
			t.Fatalf("failed to update CRL in secret %s/%s: %v", systemNS.Name(), secretName, err)
		}
	}

	t.Logf("CRL updated successfully with revoked intermediate certificate")
}

func createCustomCASecret(
	ctx resource.Context,
	bundle *Bundle,
) error {
	systemNs, err := istio.ClaimSystemNamespace(ctx)
	if err != nil {
		return err
	}

	for _, kubeCluster := range ctx.AllClusters() {
		data := map[string][]byte{
			ca.CACertFile:       bundle.intermediateCertPEM,
			ca.CAPrivateKeyFile: bundle.intermediateKeyPEM,
			ca.CertChainFile:    bundle.certChainPEM,
			ca.RootCertFile:     bundle.rootCertPEM,
			ca.CACRLFile:        bundle.crlPEM,
		}

		if err = upsertSecret(kubeCluster.Kube(), systemNs.Name(), data); err != nil {
			return err
		}

		// if there is a configmap storing the CA cert from a previous
		// integration test, remove it. Ideally, CI should delete all
		// resources from a previous integration test, but sometimes
		// the resources from a previous integration test are not deleted.
		configMapName := "istio-ca-root-cert"
		err = kubeCluster.Kube().CoreV1().ConfigMaps(systemNs.Name()).Delete(context.TODO(), configMapName,
			metav1.DeleteOptions{})
		if err == nil {
			log.Infof("configmap %v is deleted", configMapName)
		} else {
			log.Warnf("configmap %v may not exist and the deletion returns err (%v)",
				configMapName, err)
		}
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

func WaitForCRLUpdate(t framework.TestContext, namespaces []string, bundle *Bundle, instances ...echo.Instance) {
	t.Helper()
	starTime := time.Now()
	t.Logf("waiting for CRL update in namespaces: %s", strings.Join(namespaces, ", "))
	defer func() {
		t.Logf("CRL wait executed in %v", time.Since(starTime))
	}()

	// verify crl ConfigMaps are updated
	retry.UntilSuccessOrFail(t, func() error {
		return verifyCRLConfigMaps(t, namespaces, bundle.crlPEM)
	}, retry.Timeout(waitTimeout))

	// force pod annotation update to trigger ConfigMap volume refresh
	retry.UntilSuccessOrFail(t, func() error {
		return forceVolumeRefresh(t, instances...)
	}, retry.Timeout(waitTimeout))
}

func verifyCRLConfigMaps(t framework.TestContext, namespaces []string, expectedCRL []byte) error {
	for _, c := range t.AllClusters() {
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
