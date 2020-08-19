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

	"github.com/golang/protobuf/ptypes/duration"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"istio.io/istio/pkg/bootstrap/platform"
	"istio.io/istio/pkg/security"
	gcapb "istio.io/istio/security/proto/providers/google"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

const bearerTokenPrefix = "Bearer "

var (
	googleCAClientLog = log.RegisterScope("googleca", "Google CA client debugging", 0)
	envGkeClusterURL  = env.RegisterStringVar("GKE_CLUSTER_URL", "", "The url of GKE cluster").Get()
)

type googleCAClient struct {
	caEndpoint string
	enableTLS  bool
	client     gcapb.MeshCertificateServiceClient
}

// NewGoogleCAClient create a CA client for Google CA.
func NewGoogleCAClient(endpoint string, tls bool) (security.Client, error) {
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
		opts = grpc.WithInsecure()
	}

	// TODO(JimmyCYJ): This connection is create at construction time. If conn is broken at anytime,
	//  need a way to reconnect.
	conn, err := grpc.Dial(endpoint, opts)
	if err != nil {
		googleCAClientLog.Errorf("Failed to connect to endpoint %s: %v", endpoint, err)
		return nil, fmt.Errorf("failed to connect to endpoint %s", endpoint)
	}

	c.client = gcapb.NewMeshCertificateServiceClient(conn)
	return c, nil
}

// CSR Sign calls Google CA to sign a CSR.
func (cl *googleCAClient) CSRSign(ctx context.Context, reqID string, csrPEM []byte, token string,
	certValidTTLInSec int64) ([]string /*PEM-encoded certificate chain*/, error) {
	req := &gcapb.MeshCertificateRequest{
		RequestId: reqID,
		Csr:       string(csrPEM),
		Validity:  &duration.Duration{Seconds: certValidTTLInSec},
	}

	// If the token doesn't have "Bearer " prefix, add it.
	if !strings.HasPrefix(token, bearerTokenPrefix) {
		token = bearerTokenPrefix + token
	}

	out, _ := metadata.FromOutgoingContext(ctx)
	// preventing races by modification.
	out = out.Copy()
	out["authorization"] = []string{token}

	gkeClusterURL := envGkeClusterURL
	if envGkeClusterURL == "" && platform.IsGCP() {
		gkeClusterURL = platform.NewGCP().Metadata()[platform.GCPClusterURL]
	}
	zone := parseZone(gkeClusterURL)
	if zone != "" {
		out["x-goog-request-params"] = []string{fmt.Sprintf("location=locations/%s", zone)}
	}

	ctx = metadata.NewOutgoingContext(ctx, out)
	resp, err := cl.client.CreateCertificate(ctx, req)
	if err != nil {
		googleCAClientLog.Errorf("Failed to create certificate: %v", err)
		return nil, err
	}

	if len(resp.CertChain) <= 1 {
		googleCAClientLog.Errorf("CertChain length is %d, expected more than 1", len(resp.CertChain))
		return nil, errors.New("invalid response cert chain")
	}

	return resp.CertChain, nil
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

func parseZone(clusterURL string) string {
	// input: https://container.googleapis.com/v1/projects/testproj/locations/us-central1-c/clusters/cluster1
	// output: us-central1-c
	var rgx = regexp.MustCompile(`.*/projects/(.*)/locations/(.*)/clusters/.*`)
	rs := rgx.FindStringSubmatch(clusterURL)
	if len(rs) < 3 {
		return ""
	}
	return rs[2]
}
