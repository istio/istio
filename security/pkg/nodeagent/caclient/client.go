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
	"crypto/x509"
	"errors"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"istio.io/istio/pkg/log"
	capb "istio.io/istio/security/proto"
)

// Client interface defines the clients need to implement to talk to CA for CSR.
type Client interface {
	CSRSign(ctx context.Context, csrPEM []byte, subjectID string,
		certValidTTLInSec int64) ([]byte /*PEM-encoded certificate chain*/, error)
}

type caClient struct {
	client capb.IstioCertificateServiceClient
}

// NewCAClient create an CA client.
func NewCAClient(endpoint string, tlsFlag bool) (Client, error) {
	var opts grpc.DialOption
	if tlsFlag {
		pool, err := x509.SystemCertPool()
		if err != nil {
			log.Errorf("could not get SystemCertPool: %v", err)
			return nil, errors.New("could not get SystemCertPool")
		}
		creds := credentials.NewClientTLSFromCert(pool, "")
		opts = grpc.WithTransportCredentials(creds)
	} else {
		opts = grpc.WithInsecure()
	}

	conn, err := grpc.Dial(endpoint, opts)
	if err != nil {
		log.Errorf("Failed to connect to endpoint %q: %v", endpoint, err)
		return nil, fmt.Errorf("failed to connect to endpoint %q", endpoint)
	}

	return &caClient{
		client: capb.NewIstioCertificateServiceClient(conn),
	}, nil
}

func (cl *caClient) CSRSign(ctx context.Context, csrPEM []byte, token string,
	certValidTTLInSec int64) ([]byte /*PEM-encoded certificate chain*/, error) {
	req := &capb.IstioCertificateRequest{
		Csr:              string(csrPEM),
		ValidityDuration: certValidTTLInSec,
	}

	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("Authorization", token))
	resp, err := cl.client.CreateCertificate(ctx, req)
	if err != nil {
		log.Errorf("Failed to create certificate: %v", err)
		return nil, err
	}

	if len(resp.CertChain) <= 1 {
		log.Errorf("CertChain length is %d, expected more than 1", len(resp.CertChain))
		return nil, errors.New("invalid response cert chain")
	}

	// Returns the leaf cert(Leaf cert is element '0', Root cert is element 'n').
	ret := []byte{}
	for _, c := range resp.CertChain {
		ret = append(ret, []byte(c)...)
	}

	return ret, nil
}
