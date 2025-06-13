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

package grpc

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"istio.io/istio/pkg/log"
	sec_model "istio.io/istio/pkg/model"
	"istio.io/istio/security/pkg/pki/util"
)

// TLSOptions include TLS options that a grpc client uses to connect with server.
type TLSOptions struct {
	RootCert      string
	Key           string
	Cert          string
	ServerAddress string
	SAN           string
}

func getTLSDialOption(opts *TLSOptions) (grpc.DialOption, error) {
	rootCert, err := getRootCertificate(opts.RootCert)
	if err != nil {
		return nil, err
	}
	// f, err := os.OpenFile("keylog", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	// if err != nil {
	// 	f = nil
	// }

	config := tls.Config{
		// KeyLogWriter: f,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			var certificate tls.Certificate
			key, cert := opts.Key, opts.Cert
			if key != "" && cert != "" {
				isExpired, err := util.IsCertExpired(opts.Cert)
				if err != nil {
					log.Warnf("cannot parse the cert chain, using token instead: %v", err)
					return &certificate, nil
				}
				if isExpired {
					log.Warnf("cert expired, using token instead")
					return &certificate, nil
				}
				// Load the certificate from disk
				certificate, err = tls.LoadX509KeyPair(cert, key)
				if err != nil {
					return nil, err
				}
			}
			return &certificate, nil
		},
		RootCAs:    rootCert,
		MinVersion: tls.VersionTLS12,
	}

	if host, _, err := net.SplitHostPort(opts.ServerAddress); err == nil {
		config.ServerName = host
	}
	// For debugging on localhost (with port forward)
	if strings.Contains(config.ServerName, "localhost") {
		config.ServerName = "istiod.istio-system.svc"
	}
	if opts.SAN != "" {
		config.ServerName = opts.SAN
	}
	// Compliance for all gRPC clients (e.g. Citadel)..
	sec_model.EnforceGoCompliance(&config)
	transportCreds := credentials.NewTLS(&config)
	return grpc.WithTransportCredentials(transportCreds), nil
}

func getRootCertificate(rootCertFile string) (*x509.CertPool, error) {
	var certPool *x509.CertPool
	var rootCert []byte
	var err error

	if rootCertFile != "" {
		rootCert, err = os.ReadFile(rootCertFile)
		if err != nil {
			return nil, err
		}

		certPool = x509.NewCertPool()
		ok := certPool.AppendCertsFromPEM(rootCert)
		if !ok {
			return nil, fmt.Errorf("failed to create TLS dial option with root certificates")
		}
	} else {
		certPool, err = x509.SystemCertPool()
		if err != nil {
			return nil, err
		}
	}
	return certPool, nil
}
