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

package caclient

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/durationpb"

	"istio.io/istio/pkg/bootstrap/platform"
	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/nodeagent/caclient"
	gcapb "istio.io/istio/security/proto/providers/google"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

const hubIDPPrefix = "https://gkehub.googleapis.com/"

var (
	googleCAClientLog = log.RegisterScope("googleca", "Google CA client debugging", 0)
	envGkeClusterURL  = env.Register("GKE_CLUSTER_URL", "", "The url of GKE cluster").Get()
)

type googleCAClient struct {
	caEndpoint string
	enableTLS  bool
	client     gcapb.MeshCertificateServiceClient
	conn       *grpc.ClientConn
}

// NewGoogleCAClient create a CA client for Google CA.
func NewGoogleCAClient(endpoint string, tls bool, provider *caclient.TokenProvider) (security.Client, error) {
	c := &googleCAClient{
		caEndpoint: endpoint,
		enableTLS:  tls,
	}

	var opts grpc.DialOption
	var err error
	if tls {
		opts, err = c.getTLSDialOption()
		if err != nil {
			return nil, err
		}
	} else {
		opts = grpc.WithTransportCredentials(insecure.NewCredentials())
	}

	conn, err := grpc.Dial(endpoint,
		opts,
		grpc.WithPerRPCCredentials(provider),
		security.CARetryInterceptor(),
	)
	if err != nil {
		googleCAClientLog.Errorf("Failed to connect to endpoint %s: %v", endpoint, err)
		return nil, fmt.Errorf("failed to connect to endpoint %s", endpoint)
	}

	c.conn = conn
	c.client = gcapb.NewMeshCertificateServiceClient(conn)
	return c, nil
}

// CSR Sign calls Google CA to sign a CSR.
func (cl *googleCAClient) CSRSign(csrPEM []byte, certValidTTLInSec int64) ([]string, error) {
	req := &gcapb.MeshCertificateRequest{
		RequestId: uuid.New().String(),
		Csr:       string(csrPEM),
		Validity:  &durationpb.Duration{Seconds: certValidTTLInSec},
	}

	out := metadata.New(nil)
	gkeClusterURL := envGkeClusterURL
	if envGkeClusterURL == "" && platform.IsGCP() {
		gkeClusterURL = platform.NewGCP().Metadata()[platform.GCPClusterURL]
	}
	zone := parseZone(gkeClusterURL)
	if zone != "" {
		out["x-goog-request-params"] = []string{fmt.Sprintf("location=locations/%s", zone)}
	}

	ctx := metadata.NewOutgoingContext(context.Background(), out)
	resp, err := cl.client.CreateCertificate(ctx, req)
	if err != nil {
		googleCAClientLog.Errorf("Failed to create certificate: %v", err)
		googleCAClientLog.Debugf("Original request %v, resp %v", req, resp)
		return nil, err
	}

	if len(resp.CertChain) <= 1 {
		googleCAClientLog.Errorf("CertChain length is %d, expected more than 1", len(resp.CertChain))
		return nil, errors.New("invalid response cert chain")
	}
	googleCAClientLog.Infof("Cert created with GoogleCA %s chain length %d", zone, len(resp.CertChain))

	return resp.CertChain, nil
}

func (cl *googleCAClient) Close() {
	if cl.conn != nil {
		cl.conn.Close()
	}
}

func (cl *googleCAClient) getTLSDialOption() (grpc.DialOption, error) {
	// Load the system default root certificates.
	pool, err := x509.SystemCertPool()
	if err != nil {
		googleCAClientLog.Errorf("could not get SystemCertPool: %v", err)
		return nil, errors.New("could not get SystemCertPool")
	}
	creds := credentials.NewClientTLSFromCert(pool, "")
	return grpc.WithTransportCredentials(creds), nil
}

// GetRootCertBundle: Google Mesh CA doesn't publish any endpoint to retrieve CA certs
func (cl *googleCAClient) GetRootCertBundle() ([]string, error) {
	return []string{}, nil
}

func parseZone(clusterURL string) string {
	// for Hub IDNS, the input is https://gkehub.googleapis.com/projects/HUB_PROJECT_ID/locations/global/memberships/MEMBERSHIP_ID which is global
	if strings.HasPrefix(clusterURL, hubIDPPrefix) {
		return ""
	}
	// input: https://container.googleapis.com/v1/projects/testproj/locations/us-central1-c/clusters/cluster1
	// output: us-central1-c
	rgx := regexp.MustCompile(`.*/projects/(.*)/locations/(.*)/clusters/.*`)
	rs := rgx.FindStringSubmatch(clusterURL)
	if len(rs) < 3 {
		return ""
	}
	return rs[2]
}
