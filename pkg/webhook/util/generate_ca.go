// Copyright 2019 Istio Authors
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

package util

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"istio.io/istio/pkg/webhook/model"
)

// GenerateKeyAndCert generates self signed certificate and key
func GenerateKeyAndCert(caArgs *model.Args, service, namespace string) (caCertPEM, serverCertPEM, serverKeyPEM []byte, err error) { // nolint: lll
	var (
		notBefore = time.Now()
		notAfter  = notBefore.Add(caArgs.RequestedCertTTL)
	)

	maxSerialNumber, err := genSerialNum()
	if err != nil {
		return nil, nil, nil, err
	}

	// Generate self-signed CA cert
	caKey, err := rsa.GenerateKey(rand.Reader, caArgs.RSAKeySize)
	if err != nil {
		return nil, nil, nil, err
	}
	caSerialNumber, err := rand.Int(rand.Reader, maxSerialNumber)
	if err != nil {
		return nil, nil, nil, err
	}
	caTemplate := x509.Certificate{
		SerialNumber:          caSerialNumber,
		Subject:               pkix.Name{CommonName: fmt.Sprintf("%s_a", service), Organization: []string{caArgs.Org}},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caCert, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, err
	}

	// Generate server certificate signed by self-signed CA
	serverKey, err := rsa.GenerateKey(rand.Reader, caArgs.RSAKeySize)
	if err != nil {
		return nil, nil, nil, err
	}
	serverSerialNumber, err := rand.Int(rand.Reader, maxSerialNumber)
	if err != nil {
		return nil, nil, nil, err
	}
	serverTemplate := x509.Certificate{
		SerialNumber: serverSerialNumber,
		Subject:      pkix.Name{CommonName: fmt.Sprintf("%s.%s.svc", service, namespace), Organization: []string{caArgs.Org}},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	serverCert, err := x509.CreateCertificate(rand.Reader, &serverTemplate, &caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, err
	}

	// PEM encoding
	caCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCert})
	serverCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCert})
	serverKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)})

	return caCertPEM, serverCertPEM, serverKeyPEM, nil
}

func genSerialNum() (*big.Int, error) {
	serialNumLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNum, err := rand.Int(rand.Reader, serialNumLimit)
	if err != nil {
		return nil, fmt.Errorf("serial number generation failure (%v)", err)
	}
	return serialNum, nil
}

// ParsePemEncodedCertificate constructs a `x509.Certificate` object using the
// given a PEM-encoded certificate.
func ParsePemEncodedCertificate(certBytes []byte) (*x509.Certificate, error) {
	cb, _ := pem.Decode(certBytes)
	if cb == nil {
		return nil, fmt.Errorf("invalid PEM encoded certificate")
	}

	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse X.509 certificate")
	}

	return cert, nil
}

// CreateCAFiles creates the cert/key files, which will be used by application
func CreateCAFiles(caArgs *model.Args, certBytes, keyBytes, rootCertBytes []byte) error {

	err := createFile(caArgs.CertFile, certBytes)
	if err != nil {
		return fmt.Errorf("failed to write Certificate (%v)", err)
	}

	err = createFile(caArgs.KeyFile, keyBytes)
	if err != nil {
		return fmt.Errorf("failed to write private key (%v)", err)
	}

	err = createFile(caArgs.RootCertFile, rootCertBytes)
	if err != nil {
		return fmt.Errorf("failed to write CA KeyCertBundle (%v)", err)
	}

	return nil
}

func createFile(filename string, content []byte) (err error) {
	_, err = os.Stat(filename)
	// create file if not exists
	if os.IsNotExist(err) {
		path, _ := filepath.Split(filename)
		if err := os.Mkdir(path, 0644); err != nil {
			return err
		}
	}
	err = ioutil.WriteFile(filename, content, 0400)
	if err != nil {
		return err
	}
	return nil
}
