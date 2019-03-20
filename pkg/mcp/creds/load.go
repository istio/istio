//  Copyright 2018 Istio Authors
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

package creds

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
)

// loadCertPair from the given set of files.
func loadCertPair(certFile, keyFile string) (tls.Certificate, error) {
	certPEMBlock, err := readFile(certFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEMBlock, err := readFile(keyFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	cert, err := tls.X509KeyPair(certPEMBlock, keyPEMBlock)
	if err != nil {
		err = fmt.Errorf("error loading client certificate files (%s, %s): %v", certFile, keyFile, err)
	}
	return cert, err
}

// loadCACert, create a certPool and return.
func loadCACert(caCertFile string) (*x509.CertPool, error) {
	certPool := x509.NewCertPool()
	ca, err := readFile(caCertFile)
	if err != nil {
		err = fmt.Errorf("error loading CA certificate file (%s): %v", caCertFile, err)
		return nil, err
	}

	if ok := certPool.AppendCertsFromPEM(ca); !ok {
		err = errors.New("failed to append loaded CA certificate to the certificate pool")
		return nil, err
	}

	return certPool, nil
}
