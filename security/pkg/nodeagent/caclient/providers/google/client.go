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
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"istio.io/istio/pkg/log"
	caClientInterface "istio.io/istio/security/pkg/nodeagent/caclient/interface"
	gcapb "istio.io/istio/security/proto/providers/google"
)

type googleCAClient struct {
	client gcapb.IstioCertificateServiceClient
}

// NewGoogleCAClient create an CA client for Google CA.
func NewGoogleCAClient(conn *grpc.ClientConn) caClientInterface.Client {
	return &googleCAClient{
		client: gcapb.NewIstioCertificateServiceClient(conn),
	}
}

func (cl *googleCAClient) CSRSign(ctx context.Context, csrPEM []byte, token string,
	certValidTTLInSec int64) ([]string /*PEM-encoded certificate chain*/, error) {
	log.Infof("******google ca take CSR")

	req := &gcapb.IstioCertificateRequest{
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

	return resp.CertChain, nil
}
