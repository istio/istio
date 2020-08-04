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

package creds

import (
	"crypto/tls"
	"crypto/x509"

	"istio.io/pkg/log"
)

var scope = log.RegisterScope("mcp", "mcp debugging", 0)

const (
	// defaultCertDir is the default directory in which MCP options reside.
	defaultCertDir = "/etc/certs/"
	// defaultCertificateFile is the default name to use for the certificate file.
	defaultCertificateFile = "cert-chain.pem"
	// defaultKeyFile is the default name to use for the key file.
	defaultKeyFile = "key.pem"
	// defaultCACertificateFile is the default name to use for the Certificate Authority's certificate file.
	defaultCACertificateFile = "root-cert.pem"
)

// CertificateWatcher watches a x509 cert/key file and loads it up in memory as needed.
type CertificateWatcher interface {
	Get() tls.Certificate
	certPool() *x509.CertPool
}
