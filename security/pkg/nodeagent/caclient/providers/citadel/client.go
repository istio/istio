// Copyright 2018 Istio Authors
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

package caclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"istio.io/istio/pkg/log"
	caClientInterface "istio.io/istio/security/pkg/nodeagent/caclient/interface"
	pb "istio.io/istio/security/proto"
)

const (
	caServerName      = "istio-citadel"
	bearerTokenPrefix = "Bearer "
)

type citadelClient struct {
	caEndpoint    string
	enableTLS     bool
	caTLSRootCert []byte
	client        pb.IstioCertificateServiceClient
}

// NewCitadelClient create a CA client for Citadel.
func NewCitadelClient(endpoint string, tls bool, rootCert []byte) (caClientInterface.Client, error) {
	c := &citadelClient{
		caEndpoint:    endpoint,
		enableTLS:     tls,
		caTLSRootCert: rootCert,
	}

	var opts grpc.DialOption
	var err error
	if tls {
		opts, err = c.getTLSDialOption()
		if err != nil {
			return nil, err
		}
	} else {
		opts = grpc.WithInsecure()
	}

	conn, err := grpc.Dial(endpoint, opts)
	if err != nil {
		log.Errorf("Failed to connect to endpoint %s: %v", endpoint, err)
		return nil, fmt.Errorf("failed to connect to endpoint %s", endpoint)
	}

	c.client = pb.NewIstioCertificateServiceClient(conn)
	return c, nil
}

// CSR Sign calls Citadel to sign a CSR.
func (c *citadelClient) CSRSign(ctx context.Context, csrPEM []byte, token string,
	certValidTTLInSec int64) ([]string /*PEM-encoded certificate chain*/, error) {
	req := &pb.IstioCertificateRequest{
		Csr:              string(csrPEM),
		ValidityDuration: certValidTTLInSec,
	}

	// add Bearer prefix, which is required by Citadel.
	token = bearerTokenPrefix + token
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("Authorization", token))
	resp, err := c.client.CreateCertificate(ctx, req)
	if err != nil {
		log.Errorf("Failed to create certificate: %v", err)
		return nil, err
	}

	if len(resp.CertChain) <= 1 {
		log.Errorf("CertChain length is %d, expected more than 1", len(resp.CertChain))
		return nil, errors.New("invalid response cert chain")
	}

	return resp.CertChain, nil
}

func (c *citadelClient) getTLSDialOption() (grpc.DialOption, error) {
	// Load the TLS root certificate from the specified file.
	// Create a certificate pool
	certPool := x509.NewCertPool()
	ok := certPool.AppendCertsFromPEM(c.caTLSRootCert)
	if !ok {
		return nil, fmt.Errorf("failed to append certificates")
	}

	config := tls.Config{}
	config.RootCAs = certPool
	config.ServerName = caServerName

	transportCreds := credentials.NewTLS(&config)
	return grpc.WithTransportCredentials(transportCreds), nil
}
