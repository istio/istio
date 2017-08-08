// Copyright 2017 Istio Authors
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

package utils

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"

	"github.com/golang/glog"
	"google.golang.org/grpc/credentials"
)

// GetTLSCredentials creates transport credentials that are common to
// node agent and CA.
func GetTLSCredentials(certificateFile string, keyFile string,
	caCertFile string, isClient bool) credentials.TransportCredentials {

	// Load the certificate from disk
	certificate, err := tls.LoadX509KeyPair(certificateFile, keyFile)
	if err != nil {
		glog.Fatalf("Cannot load key pair: %s", err)
	}

	// Create a certificate pool
	certPool := x509.NewCertPool()
	bs, err := ioutil.ReadFile(caCertFile)
	if err != nil {
		glog.Fatalf("Failed to read CA cert: %s", err)
	}

	ok := certPool.AppendCertsFromPEM(bs)
	if !ok {
		glog.Fatalf("Failed to append certificates")
	}

	config := tls.Config{
		Certificates: []tls.Certificate{certificate},
	}

	if isClient {
		config.RootCAs = certPool
	} else {
		// The server does not always require client cert. Client provides cert for on-prem VM and JWT
		// for GCE VM.
		config.ClientAuth = tls.VerifyClientCertIfGiven
		config.ClientCAs = certPool
	}

	return credentials.NewTLS(&config)
}
