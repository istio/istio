/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tlsutil

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"k8s.io/helm/pkg/urlutil"
)

func newTLSConfigCommon(certFile, keyFile, caFile string) (*tls.Config, error) {
	config := tls.Config{}

	if certFile != "" && keyFile != "" {
		cert, err := CertFromFilePair(certFile, keyFile)
		if err != nil {
			return nil, err
		}
		config.Certificates = []tls.Certificate{*cert}
	}

	if caFile != "" {
		cp, err := CertPoolFromFile(caFile)
		if err != nil {
			return nil, err
		}
		config.RootCAs = cp
	}

	return &config, nil
}

// NewClientTLS returns tls.Config appropriate for client auth.
func NewClientTLS(certFile, keyFile, caFile string) (*tls.Config, error) {
	return newTLSConfigCommon(certFile, keyFile, caFile)
}

// NewTLSConfig returns tls.Config appropriate for client and/or server auth.
func NewTLSConfig(url, certFile, keyFile, caFile string) (*tls.Config, error) {
	config, err := newTLSConfigCommon(certFile, keyFile, caFile)
	if err != nil {
		return nil, err
	}
	config.BuildNameToCertificate()

	serverName, err := urlutil.ExtractHostname(url)
	if err != nil {
		return nil, err
	}
	config.ServerName = serverName

	return config, nil
}

// CertPoolFromFile returns an x509.CertPool containing the certificates
// in the given PEM-encoded file.
// Returns an error if the file could not be read, a certificate could not
// be parsed, or if the file does not contain any certificates
func CertPoolFromFile(filename string) (*x509.CertPool, error) {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("can't read CA file: %v", filename)
	}
	cp := x509.NewCertPool()
	if !cp.AppendCertsFromPEM(b) {
		return nil, fmt.Errorf("failed to append certificates from file: %s", filename)
	}
	return cp, nil
}

// CertFromFilePair returns an tls.Certificate containing the
// certificates public/private key pair from a pair of given PEM-encoded files.
// Returns an error if the file could not be read, a certificate could not
// be parsed, or if the file does not contain any certificates
func CertFromFilePair(certFile, keyFile string) (*tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("can't load key pair from cert %s and key %s: %s", certFile, keyFile, err)
	}
	return &cert, err
}
