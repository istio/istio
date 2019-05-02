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
	"os"
)

// Options represents configurable options used to create client and server TLS configurations.
type Options struct {
	CaCertFile string
	// If either the KeyFile or CertFile is empty, ClientConfig() will not load them,
	// preventing Helm from authenticating to Tiller. They are required to be non-empty
	// when calling ServerConfig, otherwise an error is returned.
	KeyFile  string
	CertFile string
	// Client-only options
	InsecureSkipVerify bool
	// Overrides the server name used to verify the hostname on the returned
	// certificates from the server.
	ServerName string
	// Server-only options
	ClientAuth tls.ClientAuthType
}

// ClientConfig retusn a TLS configuration for use by a Helm client.
func ClientConfig(opts Options) (cfg *tls.Config, err error) {
	var cert *tls.Certificate
	var pool *x509.CertPool

	if opts.CertFile != "" || opts.KeyFile != "" {
		if cert, err = CertFromFilePair(opts.CertFile, opts.KeyFile); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("could not load x509 key pair (cert: %q, key: %q): %v", opts.CertFile, opts.KeyFile, err)
			}
			return nil, fmt.Errorf("could not read x509 key pair (cert: %q, key: %q): %v", opts.CertFile, opts.KeyFile, err)
		}
	}
	if !opts.InsecureSkipVerify && opts.CaCertFile != "" {
		if pool, err = CertPoolFromFile(opts.CaCertFile); err != nil {
			return nil, err
		}
	}
	cfg = &tls.Config{
		InsecureSkipVerify: opts.InsecureSkipVerify,
		Certificates:       []tls.Certificate{*cert},
		ServerName:         opts.ServerName,
		RootCAs:            pool,
	}
	return cfg, nil
}

// ServerConfig returns a TLS configuration for use by the Tiller server.
func ServerConfig(opts Options) (cfg *tls.Config, err error) {
	var cert *tls.Certificate
	var pool *x509.CertPool

	if cert, err = CertFromFilePair(opts.CertFile, opts.KeyFile); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("could not load x509 key pair (cert: %q, key: %q): %v", opts.CertFile, opts.KeyFile, err)
		}
		return nil, fmt.Errorf("could not read x509 key pair (cert: %q, key: %q): %v", opts.CertFile, opts.KeyFile, err)
	}
	if opts.ClientAuth >= tls.VerifyClientCertIfGiven && opts.CaCertFile != "" {
		if pool, err = CertPoolFromFile(opts.CaCertFile); err != nil {
			return nil, err
		}
	}

	cfg = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: opts.ClientAuth, Certificates: []tls.Certificate{*cert}, ClientCAs: pool}
	return cfg, nil
}
