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
	"os"
	"strconv"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"istio.io/istio/pkg/log"
	caClientInterface "istio.io/istio/security/pkg/nodeagent/caclient/interface"
	gcapb "istio.io/istio/security/proto/providers/google"
)

var usePodDefaultFlag = false

const (
	podIdentityFlag   = "POD_IDENTITY"
	bearerTokenPrefix = "Bearer "
)

type googleCAClient struct {
	caEndpoint     string
	enableTLS      bool
	client         gcapb.IstioCertificateServiceClient
	usePodIdentity bool
}

// NewGoogleCAClient create a CA client for Google CA.
func NewGoogleCAClient(endpoint string, tls bool) (caClientInterface.Client, error) {
	c := &googleCAClient{
		caEndpoint: endpoint,
		enableTLS:  tls,
	}

	c.usePodIdentity = usePodDefaultFlag
	b, err := strconv.ParseBool(os.Getenv(podIdentityFlag))
	if err == nil && b == true {
		c.usePodIdentity = true
	}

	var opts grpc.DialOption
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

	c.client = gcapb.NewIstioCertificateServiceClient(conn)
	return c, nil
}

// CSR Sign calls Google CA to sign a CSR.
func (cl *googleCAClient) CSRSign(ctx context.Context, csrPEM []byte, token string,
	certValidTTLInSec int64) ([]string /*PEM-encoded certificate chain*/, error) {
	req := &gcapb.IstioCertificateRequest{
		Csr:              string(csrPEM),
		ValidityDuration: certValidTTLInSec,
	}

	// If the token doesn't have "Bearer " prefix, add it.
	if !strings.HasPrefix(token, bearerTokenPrefix) {
		token = bearerTokenPrefix + token
	}

	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("Authorization", token))

	var resp *gcapb.IstioCertificateResponse
	var err error
	if cl.usePodIdentity {
		resp, err = cl.client.CreatePodCertificate(ctx, req)
	} else {
		resp, err = cl.client.CreateCertificate(ctx, req)
	}
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

func (cl *googleCAClient) getTLSDialOption() (grpc.DialOption, error) {
	// Load the system default root certificates.
	pool, err := x509.SystemCertPool()
	if err != nil {
		log.Errorf("could not get SystemCertPool: %v", err)
		return nil, errors.New("could not get SystemCertPool")
	}
	creds := credentials.NewClientTLSFromCert(pool, "")
	return grpc.WithTransportCredentials(creds), nil
}
