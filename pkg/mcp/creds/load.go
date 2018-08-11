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
	"io/ioutil"
	"path"

	"google.golang.org/grpc/credentials"
)

// LoadFromFolder loads certificates from the given folder. It expects the following files:
// cert-chain.pem, key.pem: Certificate/key files for the client/server on this side.
// root-cert.pem: certificate from the CA that will be used for validating peer's certificate.
func LoadFromFolder(folder string) (credentials.TransportCredentials, error) {
	certFile := path.Join(folder, "cert-chain.pem")
	keyFile := path.Join(folder, "key.pem")
	caCertFile := path.Join(folder, "root-cert.pem")

	return LoadFromFiles(certFile, keyFile, caCertFile)
}

// LoadFromFiles loads certificate & key files from the file system and returns transport credentials to be
// used for mTLS authentication with MCP.
func LoadFromFiles(certFile, keyFile, caCertFile string) (credentials.TransportCredentials, error) {

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("error loading client certificate files (%s, %s): %v", certFile, keyFile, err)
	}

	certPool := x509.NewCertPool()
	ca, err := ioutil.ReadFile(caCertFile)
	if err != nil {
		return nil, fmt.Errorf("error loading CA certificate file (%s): %v", caCertFile, err)
	}

	if ok := certPool.AppendCertsFromPEM(ca); !ok {
		return nil, errors.New("failed to append CA certificate to the certificate pool")
	}

	creds := credentials.NewTLS(&tls.Config{
		ClientAuth:   tls.RequireAndVerifyClientCert,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    certPool,
	})

	return creds, nil
}
