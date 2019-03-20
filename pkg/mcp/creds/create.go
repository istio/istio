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

	"google.golang.org/grpc/credentials"
)

// CreateForClient creates TransportCredentials for MCP clients.
func CreateForClient(serverName string, watcher CertificateWatcher) credentials.TransportCredentials {
	config := tls.Config{
		ServerName: serverName,
		RootCAs:    watcher.certPool(),
		GetClientCertificate: func(info *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			c := watcher.Get()
			return &c, nil
		},
	}

	return credentials.NewTLS(&config)
}

// CreateForClientSkipVerify creates TransportCredentials for MCP clients which skips verify the
// server's certificate chain and host name..
func CreateForClientSkipVerify() credentials.TransportCredentials {
	config := tls.Config{
		InsecureSkipVerify: true,
	}

	return credentials.NewTLS(&config)
}

// CreateForServer creates TransportCredentials for MCP servers.
func CreateForServer(watcher CertificateWatcher) credentials.TransportCredentials {
	config := tls.Config{
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  watcher.certPool(),
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			c := watcher.Get()
			return &c, nil
		},
	}

	return credentials.NewTLS(&config)
}
