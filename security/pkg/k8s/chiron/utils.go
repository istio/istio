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

package chiron

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"time"

	cert "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	clientset "k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/security/pkg/pki/util"
)

const (
	// The size of a private key for a leaf certificate.
	keySize = 2048
)

var certWatchTimeout = 60 * time.Second

// GenKeyCertK8sCA : Generates a key pair and gets public certificate signed by K8s_CA
// Options are meant to sign DNS certs
// 1. Generate a CSR
// 2. Call SignCSRK8s to finish rest of the flow
func GenKeyCertK8sCA(client clientset.Interface, dnsName,
	caFilePath string, signerName string, approveCsr bool, requestedLifetime time.Duration,
) ([]byte, []byte, []byte, error) {
	// 1. Generate a CSR
	options := util.CertOptions{
		Host:       dnsName,
		RSAKeySize: keySize,
		IsDualUse:  false,
		PKCS8Key:   false,
	}
	csrPEM, keyPEM, err := util.GenCSR(options)
	if err != nil {
		log.Errorf("CSR generation error (%v)", err)
		return nil, nil, nil, err
	}
	usages := []cert.KeyUsage{
		cert.UsageDigitalSignature,
		cert.UsageKeyEncipherment,
		cert.UsageServerAuth,
	}
	if signerName == "" {
		signerName = "kubernetes.io/legacy-unknown"
	}
	certChain, caCert, err := SignCSRK8s(client, csrPEM, signerName, usages, dnsName, caFilePath, approveCsr, true, requestedLifetime)

	return certChain, keyPEM, caCert, err
}

// SignCSRK8s generates a certificate from CSR using the K8s CA
// 1. Submit a CSR
// 2. Approve a CSR
// 3. Read the signed certificate
// 4. Clean up the artifacts (e.g., delete CSR)
func SignCSRK8s(client clientset.Interface, csrData []byte, signerName string, usages []cert.KeyUsage,
	dnsName, caFilePath string, approveCsr, appendCaCert bool, requestedLifetime time.Duration,
) ([]byte, []byte, error) {
	// 1. Submit the CSR
	csr, err := submitCSR(client, csrData, signerName, usages, requestedLifetime)
	if err != nil {
		return nil, nil, err
	}
	log.Debugf("CSR (%v) has been created", csr.Name)

	// clean up certificate request after deletion
	defer func() {
		_ = cleanupCSR(client, csr)
	}()

	// 2. Approve the CSR
	if approveCsr {
		approvalMessage := fmt.Sprintf("CSR (%s) for the certificate (%s) is approved", csr.Name, dnsName)
		err = approveCSR(client, csr, approvalMessage)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to approve CSR request: %v", err)
		}
		log.Debugf("CSR (%v) is approved", csr.Name)
	}

	// 3. Read the signed certificate
	certChain, caCert, err := readSignedCertificate(client, csr, certWatchTimeout, caFilePath, appendCaCert)
	if err != nil {
		return nil, nil, err
	}

	// If there is a failure of cleaning up CSR, the error is returned.
	return certChain, caCert, err
}

// Read CA certificate and check whether it is a valid certificate.
func readCACert(caCertPath string) ([]byte, error) {
	caCert, err := os.ReadFile(caCertPath)
	if err != nil {
		log.Errorf("failed to read CA cert, cert. path: %v, error: %v", caCertPath, err)
		return nil, fmt.Errorf("failed to read CA cert, cert. path: %v, error: %v", caCertPath, err)
	}

	b, _ := pem.Decode(caCert)
	if b == nil {
		return nil, fmt.Errorf("could not decode pem")
	}
	if b.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("ca certificate contains wrong type: %v", b.Type)
	}
	if _, err := x509.ParseCertificate(b.Bytes); err != nil {
		return nil, fmt.Errorf("ca certificate parsing returns an error: %v", err)
	}

	return caCert, nil
}

func isTCPReachable(host string, port int) bool {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err != nil {
		log.Debugf("DialTimeout() returns err: %v", err)
		// No connection yet, so no need to conn.Close()
		return false
	}
	err = conn.Close()
	if err != nil {
		log.Infof("tcp connection is not closed: %v", err)
	}
	return true
}

func submitCSR(
	client clientset.Interface,
	csrData []byte,
	signerName string,
	usages []cert.KeyUsage,
	requestedLifetime time.Duration,
) (*cert.CertificateSigningRequest, error) {
	log.Debugf("create CSR for signer %v", signerName)
	csr := &cert.CertificateSigningRequest{
		// Username, UID, Groups will be injected by API server.
		TypeMeta: metav1.TypeMeta{Kind: "CertificateSigningRequest"},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "csr-workload-",
		},
		Spec: cert.CertificateSigningRequestSpec{
			Request:    csrData,
			Usages:     usages,
			SignerName: signerName,
		},
	}
	if requestedLifetime != time.Duration(0) {
		csr.Spec.ExpirationSeconds = ptr.Of(int32(requestedLifetime.Seconds()))
	}
	resp, err := client.CertificatesV1().CertificateSigningRequests().Create(context.Background(), csr, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create CSR: %v", err)
	}
	return resp, nil
}

func approveCSR(client clientset.Interface, csr *cert.CertificateSigningRequest, approvalMessage string) error {
	csr.Status.Conditions = append(csr.Status.Conditions, cert.CertificateSigningRequestCondition{
		Type:    cert.CertificateApproved,
		Reason:  approvalMessage,
		Message: approvalMessage,
		Status:  corev1.ConditionTrue,
	})
	_, err := client.CertificatesV1().CertificateSigningRequests().UpdateApproval(context.TODO(), csr.Name, csr, metav1.UpdateOptions{})
	if err != nil {
		log.Errorf("failed to approve CSR (%v): %v", csr.Name, err)
		return err
	}
	return nil
}

// Read the signed certificate
// verify and append CA certificate to certChain if appendCaCert is true
func readSignedCertificate(client clientset.Interface, csr *cert.CertificateSigningRequest,
	watchTimeout time.Duration, caCertPath string, appendCaCert bool,
) ([]byte, []byte, error) {
	// First try to read the signed CSR through a watching mechanism
	certPEM, err := readSignedCsr(client, csr.Name, watchTimeout)
	if err != nil {
		return nil, nil, err
	}
	if len(certPEM) == 0 {
		return nil, nil, fmt.Errorf("no certificate returned for the CSR: %q", csr.Name)
	}
	certsParsed, _, err := util.ParsePemEncodedCertificateChain(certPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("decoding certificate failed")
	}
	if !appendCaCert || caCertPath == "" {
		return certPEM, nil, nil
	}
	caCert, err := readCACert(caCertPath)
	if err != nil {
		return nil, nil, fmt.Errorf("error when retrieving CA cert: (%v)", err)
	}
	// Verify the certificate chain before returning the certificate
	roots := x509.NewCertPool()
	if roots == nil {
		return nil, nil, fmt.Errorf("failed to create cert pool")
	}
	if ok := roots.AppendCertsFromPEM(caCert); !ok {
		return nil, nil, fmt.Errorf("failed to append CA certificate")
	}
	intermediates := x509.NewCertPool()
	if len(certsParsed) > 1 {
		for _, cert := range certsParsed[1:] {
			intermediates.AddCert(cert)
		}
	}
	_, err = certsParsed[0].Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to verify the certificate chain: %v", err)
	}

	return append(certPEM, caCert...), caCert, nil
}

// Return signed CSR through a watcher. If no CSR is read, return nil.
func readSignedCsr(client clientset.Interface, csr string, watchTimeout time.Duration) ([]byte, error) {
	selector := fields.OneTermEqualSelector("metadata.name", csr).String()
	// Setup a List+Watch, like informers do
	// A simple Watch will fail if the cert is signed too quickly
	l, _ := client.CertificatesV1().CertificateSigningRequests().List(context.Background(), metav1.ListOptions{
		FieldSelector: selector,
	})
	if l != nil && len(l.Items) > 0 {
		reqSigned := l.Items[0]
		if reqSigned.Status.Certificate != nil {
			return reqSigned.Status.Certificate, nil
		}
	}
	var rv string
	if l != nil {
		rv = l.ResourceVersion
	}
	watcher, err := client.CertificatesV1().CertificateSigningRequests().Watch(context.Background(), metav1.ListOptions{
		ResourceVersion: rv,
		FieldSelector:   selector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to watch CSR %v", csr)
	}

	// Set a timeout
	timer := time.After(watchTimeout)
	for {
		select {
		case r := <-watcher.ResultChan():
			reqSigned := r.Object.(*cert.CertificateSigningRequest)
			if reqSigned.Status.Certificate != nil {
				return reqSigned.Status.Certificate, nil
			}
		case <-timer:
			return nil, fmt.Errorf("timeout when watching CSR %v", csr)
		}
	}
}

// Clean up the CSR
func cleanupCSR(client clientset.Interface, csr *cert.CertificateSigningRequest) error {
	err := client.CertificatesV1().CertificateSigningRequests().Delete(context.TODO(), csr.Name, metav1.DeleteOptions{})
	if err != nil {
		log.Errorf("failed to delete CSR (%v): %v", csr.Name, err)
	} else {
		log.Debugf("deleted CSR: %v", csr.Name)
	}
	return err
}
