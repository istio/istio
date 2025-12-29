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

package util

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path"
	"time"

	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/file"
)

var k8sInCluster = env.Register("KUBERNETES_SERVICE_HOST", "",
	"Kubernetes service host, set automatically when running in-cluster")

// ParseCertAndGetExpiryTimestamp parses the first certificate in certByte and returns cert expire
// time, or return error if fails to parse certificate.
func ParseCertAndGetExpiryTimestamp(certByte []byte) (time.Time, error) {
	block, _ := pem.Decode(certByte)
	if block == nil {
		return time.Time{}, fmt.Errorf("failed to decode certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse certificate: %v", err)
	}
	return cert.NotAfter, nil
}

// OutputKeyCertToDir output the key and certificate to the given directory.
// If directory string is empty, return nil.
func OutputKeyCertToDir(dir string, privateKey, certChain, rootCert []byte) error {
	var err error
	if len(dir) == 0 {
		return nil
	}

	certFileMode := os.FileMode(0o600)
	if k8sInCluster.Get() != "" {
		// If this is running on k8s, give more permission to the file certs.
		// This is typically used to share the certs with non-proxy containers in the pod which does not run as root or 1337.
		// For example, prometheus server could use proxy provisioned certs to scrape application metrics through mTLS.
		certFileMode = os.FileMode(0o644)
	}
	// Depending on the SDS resource to output, some fields may be nil
	if privateKey == nil && certChain == nil && rootCert == nil {
		return fmt.Errorf("the input private key, cert chain, and root cert are nil")
	}

	writeIfNotEqual := func(fileName string, newData []byte) error {
		if newData == nil {
			return nil
		}
		oldData, _ := os.ReadFile(path.Join(dir, fileName))
		if !bytes.Equal(oldData, newData) {
			if err := file.AtomicWrite(path.Join(dir, fileName), newData, certFileMode); err != nil {
				return fmt.Errorf("failed to write data to file %v: %v", fileName, err)
			}
		}
		return nil
	}

	if err = writeIfNotEqual("key.pem", privateKey); err != nil {
		return err
	}
	if err = writeIfNotEqual("cert-chain.pem", certChain); err != nil {
		return err
	}
	if err = writeIfNotEqual("root-cert.pem", rootCert); err != nil {
		return err
	}
	return nil
}
