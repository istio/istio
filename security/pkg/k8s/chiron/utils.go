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
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	certv1 "k8s.io/api/certificates/v1"
	certv1beta1 "k8s.io/api/certificates/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	rand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/watch"
	clientset "k8s.io/client-go/kubernetes"

	"istio.io/istio/security/pkg/pki/util"
	"istio.io/pkg/log"
)

const (
	randomLength  = 18
	csrRetriesMax = 3
	// cert-manager use below annotation on kubernetes CSR to control TTL for the generated cert.
	RequestLifeTimeAnnotationForCertManager = "experimental.cert-manager.io/request-duration"
)

type CsrNameGenerator func(string, string) string

// GenCsrName : Generate CSR Name for Resource. Guarantees returning a resource name that doesn't already exist
func GenCsrName() string {
	name := fmt.Sprintf("csr-workload-%s", rand.String(randomLength))
	return name
}

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
	usages := []certv1.KeyUsage{
		certv1.UsageDigitalSignature,
		certv1.UsageKeyEncipherment,
		certv1.UsageServerAuth,
		certv1.UsageClientAuth,
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
func SignCSRK8s(client clientset.Interface, csrData []byte, signerName string, usages []certv1.KeyUsage,
	dnsName, caFilePath string, approveCsr, appendCaCert bool, requestedLifetime time.Duration,
) ([]byte, []byte, error) {
	var err error
	v1Req := false

	// 1. Submit the CSR

	csrName, v1CsrReq, v1Beta1CsrReq, err := submitCSR(client, csrData, signerName, usages, csrRetriesMax, requestedLifetime)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to submit CSR request (%v). Error: %v", csrName, err)
	}
	log.Debugf("CSR (%v) has been created", csrName)
	if v1CsrReq != nil {
		v1Req = true
	}

	// clean up certificate request after deletion
	defer func() {
		_ = cleanUpCertGen(client, v1Req, csrName)
	}()

	// 2. Approve the CSR
	if approveCsr {
		csrMsg := fmt.Sprintf("CSR (%s) for the certificate (%s) is approved", csrName, dnsName)
		err = approveCSR(csrName, csrMsg, client, v1CsrReq, v1Beta1CsrReq)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to approve CSR request. Error: %v", err)
		}
		log.Debugf("CSR (%v) is approved", csrName)
	}

	// 3. Read the signed certificate
	certChain, caCert, err := readSignedCertificate(client,
		csrName, certWatchTimeout, certReadInterval, maxNumCertRead, caFilePath, appendCaCert, v1Req)
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
	conn.Close()
	return true
}

// Reload CA cert from file and return whether CA cert is changed
func reloadCACert(wc *WebhookController) (bool, error) {
	certChanged := false
	wc.certMutex.Lock()
	defer wc.certMutex.Unlock()
	caCert, err := readCACert(wc.k8sCaCertFile)
	if err != nil {
		return certChanged, err
	}
	if !bytes.Equal(caCert, wc.CACert) {
		wc.CACert = append([]byte(nil), caCert...)
		certChanged = true
	}
	return certChanged, nil
}

func submitCSR(clientset clientset.Interface,
	csrData []byte, signerName string,
	usages []certv1.KeyUsage, numRetries int, requestedLifetime time.Duration) (string, *certv1.CertificateSigningRequest,
	*certv1beta1.CertificateSigningRequest, error,
) {
	var lastErr error
	useV1 := true
	csrName := ""
	for i := 0; i < numRetries; i++ {
		if csrName == "" {
			csrName = GenCsrName()
		}
		if useV1 && len(usages) > 0 && len(signerName) > 0 && signerName != "kubernetes.io/legacy-unknown" {
			log.Debugf("trial %v using v1 api to create CSR (%v)", i+1, csrName)
			csr := &certv1.CertificateSigningRequest{
				// Username, UID, Groups will be injected by API server.
				TypeMeta: metav1.TypeMeta{Kind: "CertificateSigningRequest"},
				ObjectMeta: metav1.ObjectMeta{
					Name: csrName,
				},
				Spec: certv1.CertificateSigningRequestSpec{
					Request:    csrData,
					Usages:     usages,
					SignerName: signerName,
				},
			}
			if requestedLifetime != time.Duration(0) {
				csr.ObjectMeta.Annotations = map[string]string{RequestLifeTimeAnnotationForCertManager: requestedLifetime.String()}
			}
			v1req, err := clientset.CertificatesV1().CertificateSigningRequests().Create(context.TODO(), csr, metav1.CreateOptions{})
			if err == nil {
				return csrName, v1req, nil, nil
			}
			lastErr = err
			if apierrors.IsAlreadyExists(err) {
				csrName = ""
				continue
			} else if apierrors.IsNotFound(err) {
				// don't attempt to use older api unless we get an API error
				useV1 = false
			} else {
				continue
			}
		}
		// Only exercise v1beta1 logic if v1 api was not found
		log.Debugf("trial %v using v1beta1 api for csr %v", i+1, csrName)
		// convert relevant bits to v1beta1
		v1beta1csr := &certv1beta1.CertificateSigningRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name: csrName,
			},
			Spec: certv1beta1.CertificateSigningRequestSpec{
				SignerName: &signerName,
				Request:    csrData,
			},
		}
		for _, usage := range usages {
			v1beta1csr.Spec.Usages = append(v1beta1csr.Spec.Usages, certv1beta1.KeyUsage(usage))
		}
		if requestedLifetime != time.Duration(0) {
			v1beta1csr.ObjectMeta.Annotations = map[string]string{RequestLifeTimeAnnotationForCertManager: requestedLifetime.String()}
		}
		// create v1beta1 certificate request
		v1beta1req, err := clientset.CertificatesV1beta1().CertificateSigningRequests().Create(context.TODO(), v1beta1csr, metav1.CreateOptions{})
		if err == nil {
			return csrName, nil, v1beta1req, nil
		}
		lastErr = err
		if apierrors.IsAlreadyExists(err) {
			csrName = ""
		}
	}
	log.Errorf("retry attempts exceeded when creating csr request %v", csrName)
	return "", nil, nil, lastErr
}

func approveCSR(csrName string, csrMsg string, client clientset.Interface,
	v1CsrReq *certv1.CertificateSigningRequest, v1Beta1CsrReq *certv1beta1.CertificateSigningRequest,
) error {
	err := errors.New("invalid CSR")

	if v1Beta1CsrReq != nil {
		v1Beta1CsrReq.Status.Conditions = append(v1Beta1CsrReq.Status.Conditions, certv1beta1.CertificateSigningRequestCondition{
			Type:    certv1beta1.CertificateApproved,
			Reason:  csrMsg,
			Message: csrMsg,
		})
		_, err = client.CertificatesV1beta1().CertificateSigningRequests().UpdateApproval(context.TODO(), v1Beta1CsrReq, metav1.UpdateOptions{})
		if err != nil {
			log.Errorf("failed to approve CSR (%v): %v", csrName, err)
			return err
		}
	} else if v1CsrReq != nil {
		v1CsrReq.Status.Conditions = append(v1CsrReq.Status.Conditions, certv1.CertificateSigningRequestCondition{
			Type:    certv1.CertificateApproved,
			Reason:  csrMsg,
			Message: csrMsg,
			Status:  corev1.ConditionTrue,
		})
		_, err = client.CertificatesV1().CertificateSigningRequests().UpdateApproval(context.TODO(), csrName, v1CsrReq, metav1.UpdateOptions{})
		if err != nil {
			log.Errorf("failed to approve CSR (%v): %v", csrName, err)
			return err
		}
	}
	return err
}

// Read the signed certificate
// verify and append CA certificate to certChain if appendCaCert is true
func readSignedCertificate(client clientset.Interface, csrName string,
	watchTimeout, readInterval time.Duration,
	maxNumRead int, caCertPath string, appendCaCert bool, usev1 bool,
) ([]byte, []byte, error) {
	// First try to read the signed CSR through a watching mechanism
	certPEM := readSignedCsr(client, csrName, watchTimeout, readInterval, maxNumRead, usev1)

	if len(certPEM) == 0 {
		return []byte{}, []byte{}, fmt.Errorf("no certificate returned for the CSR: %q", csrName)
	}
	certsParsed, _, err := util.ParsePemEncodedCertificateChain(certPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("decoding certificate failed")
	}
	caCert := []byte{}
	certChain := []byte{}
	certChain = append(certChain, certPEM...)
	if appendCaCert && caCertPath != "" {
		caCert, err = readCACert(caCertPath)
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
		certChain = append(certChain, caCert...)
	}
	return certChain, caCert, nil
}

func checkDuplicateCsr(client clientset.Interface, csrName string) (*certv1.CertificateSigningRequest, *certv1beta1.CertificateSigningRequest) {
	v1CsrReq, err := client.CertificatesV1().CertificateSigningRequests().Get(context.TODO(), csrName, metav1.GetOptions{})
	if err == nil {
		return v1CsrReq, nil
	}
	v1Beta1CsrReq, err := client.CertificatesV1beta1().CertificateSigningRequests().Get(context.TODO(), csrName, metav1.GetOptions{})
	if err == nil {
		return nil, v1Beta1CsrReq
	}
	return nil, nil
}

func getSignedCsr(client clientset.Interface, csrName string, readInterval time.Duration, maxNumRead int, usev1 bool) []byte {
	var err error
	if usev1 {
		var r *certv1.CertificateSigningRequest
		for i := 0; i < maxNumRead; i++ {
			r, err = client.CertificatesV1().CertificateSigningRequests().Get(context.TODO(), csrName, metav1.GetOptions{})
			if err == nil && r.Status.Certificate != nil {
				// Certificate is ready
				return r.Status.Certificate
			}
			time.Sleep(readInterval)
		}
		if err != nil || r.Status.Certificate == nil {
			if err != nil {
				log.Errorf("failed to read the CSR (%v): %v", csrName, err)
			} else if r.Status.Certificate == nil {
				for _, c := range r.Status.Conditions {
					if c.Type == certv1.CertificateDenied {
						log.Errorf("CertificateDenied, name: %v, uid: %v, cond-type: %v, cond: %s",
							r.Name, r.UID, c.Type, c.String())
						break
					}
				}
			}
			return []byte{}
		}
	} else {
		var r *certv1beta1.CertificateSigningRequest
		for i := 0; i < maxNumRead; i++ {
			r, err = client.CertificatesV1beta1().CertificateSigningRequests().Get(context.TODO(), csrName, metav1.GetOptions{})
			if err == nil && r.Status.Certificate != nil {
				// Certificate is ready
				return r.Status.Certificate
			}
			time.Sleep(readInterval)
		}
		if err != nil || r.Status.Certificate == nil {
			if err != nil {
				log.Errorf("failed to read the CSR (%v): %v", csrName, err)
			} else if r.Status.Certificate == nil {
				for _, c := range r.Status.Conditions {
					if c.Type == certv1beta1.CertificateDenied {
						log.Errorf("CertificateDenied, name: %v, uid: %v, cond-type: %v, cond: %s",
							r.Name, r.UID, c.Type, c.String())
						break
					}
				}
			}
			return []byte{}
		}
	}
	return []byte{}
}

// Return signed CSR through a watcher. If no CSR is read, return nil.
func readSignedCsr(client clientset.Interface, csrName string, watchTimeout time.Duration, readInterval time.Duration,
	maxNumRead int, usev1 bool,
) []byte {
	var watcher watch.Interface
	var err error
	selector := fields.OneTermEqualSelector("metadata.name", csrName).String()
	if usev1 {
		// Setup a List+Watch, like informers do
		// A simple Watch will fail if the cert is signed too quickly
		l, _ := client.CertificatesV1().CertificateSigningRequests().List(context.TODO(), metav1.ListOptions{
			FieldSelector: selector,
		})
		if l != nil && len(l.Items) > 0 {
			reqSigned := l.Items[0]
			if reqSigned.Status.Certificate != nil {
				return reqSigned.Status.Certificate
			}
		}
		var rv string
		if l != nil {
			rv = l.ResourceVersion
		}
		watcher, err = client.CertificatesV1().CertificateSigningRequests().Watch(context.TODO(), metav1.ListOptions{
			ResourceVersion: rv,
			FieldSelector:   selector,
		})
	} else {
		l, _ := client.CertificatesV1beta1().CertificateSigningRequests().List(context.TODO(), metav1.ListOptions{
			FieldSelector: selector,
		})
		if l != nil && len(l.Items) > 0 {
			reqSigned := l.Items[0]
			if reqSigned.Status.Certificate != nil {
				return reqSigned.Status.Certificate
			}
		}
		var rv string
		if l != nil {
			rv = l.ResourceVersion
		}
		watcher, err = client.CertificatesV1beta1().CertificateSigningRequests().Watch(context.TODO(), metav1.ListOptions{
			ResourceVersion: rv,
			FieldSelector:   selector,
		})
	}
	if err == nil {
		timeout := false
		// Set a timeout
		timer := time.After(watchTimeout)
		for {
			select {
			case r := <-watcher.ResultChan():
				if usev1 {
					reqSigned := r.Object.(*certv1.CertificateSigningRequest)
					if reqSigned.Status.Certificate != nil {
						return reqSigned.Status.Certificate
					}
				} else {
					reqSigned := r.Object.(*certv1beta1.CertificateSigningRequest)
					if reqSigned.Status.Certificate != nil {
						return reqSigned.Status.Certificate
					}
				}
			case <-timer:
				log.Debugf("timeout when watching CSR %v", csrName)
				timeout = true
			}
			if timeout {
				break
			}
		}
	}

	return getSignedCsr(client, csrName, readInterval, maxNumRead, usev1)
}

// Clean up the CSR
func cleanUpCertGen(client clientset.Interface, usev1 bool, csrName string) error {
	var err error

	if usev1 {
		err = client.CertificatesV1().CertificateSigningRequests().Delete(context.TODO(), csrName, metav1.DeleteOptions{})
	} else {
		err = client.CertificatesV1beta1().CertificateSigningRequests().Delete(context.TODO(), csrName, metav1.DeleteOptions{})
	}

	if err != nil {
		log.Errorf("failed to delete CSR (%v): %v", csrName, err)
	} else {
		log.Debugf("deleted CSR: %v", csrName)
	}
	return err
}
