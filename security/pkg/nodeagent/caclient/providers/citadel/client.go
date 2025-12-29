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

package citadel

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/structpb"

	pb "istio.io/api/security/v1alpha1"
	istiogrpc "istio.io/istio/pilot/pkg/grpc"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/nodeagent/caclient"
)

const (
	bearerTokenPrefix = "Bearer "
)

var citadelClientLog = log.RegisterScope("citadelclient", "citadel client debugging")

type CitadelClient struct {
	// It means enable tls connection to Citadel if this is not nil.
	tlsOpts  *TLSOptions
	client   pb.IstioCertificateServiceClient
	conn     *grpc.ClientConn
	provider credentials.PerRPCCredentials
	opts     *security.Options
}

type TLSOptions struct {
	RootCert string
	Key      string
	Cert     string
}

// NewCitadelClient create a CA client for Citadel.
func NewCitadelClient(opts *security.Options, tlsOpts *TLSOptions) (*CitadelClient, error) {
	c := &CitadelClient{
		tlsOpts:  tlsOpts,
		opts:     opts,
		provider: caclient.NewDefaultTokenProvider(opts),
	}

	conn, err := c.buildConnection()
	if err != nil {
		citadelClientLog.Errorf("Failed to connect to endpoint %s: %v", opts.CAEndpoint, err)
		return nil, fmt.Errorf("failed to connect to endpoint %s", opts.CAEndpoint)
	}
	c.conn = conn
	c.client = pb.NewIstioCertificateServiceClient(conn)
	return c, nil
}

func (c *CitadelClient) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

// CSRSign calls Citadel to sign a CSR.
func (c *CitadelClient) CSRSign(csrPEM []byte, certValidTTLInSec int64) (res []string, err error) {
	crMetaStruct := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			security.CertSigner: {
				Kind: &structpb.Value_StringValue{StringValue: c.opts.CertSigner},
			},
		},
	}
	req := &pb.IstioCertificateRequest{
		Csr:              string(csrPEM),
		ValidityDuration: certValidTTLInSec,
		Metadata:         crMetaStruct,
	}
	// TODO(hzxuzhonghu): notify caclient rebuilding only when root cert is updated.
	// It can happen when the istiod dns certs is resigned after root cert is updated,
	// in this case, the ca grpc client can not automatically connect to istiod after the underlying network connection closed.
	// Because that the grpc client still use the old tls configuration to reconnect to istiod.
	// So here we need to rebuild the caClient in order to use the new root cert.
	defer func() {
		if err != nil {
			citadelClientLog.Errorf("failed to sign CSR: %v", err)
			if err := c.reconnect(); err != nil {
				citadelClientLog.Errorf("failed reconnect: %v", err)
			}
		}
	}()

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("ClusterID", c.opts.ClusterID))
	for k, v := range c.opts.CAHeaders {
		ctx = metadata.AppendToOutgoingContext(ctx, k, v)
	}

	resp, err := c.client.CreateCertificate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %v", err)
	}

	if len(resp.CertChain) <= 1 {
		return nil, errors.New("invalid empty CertChain")
	}

	return resp.CertChain, nil
}

func (c *CitadelClient) getTLSOptions() *istiogrpc.TLSOptions {
	if c.tlsOpts != nil {
		return &istiogrpc.TLSOptions{
			RootCert:      c.tlsOpts.RootCert,
			Key:           c.tlsOpts.Key,
			Cert:          c.tlsOpts.Cert,
			ServerAddress: c.opts.CAEndpoint,
			SAN:           c.opts.CAEndpointSAN,
		}
	}
	return nil
}

func (c *CitadelClient) buildConnection() (*grpc.ClientConn, error) {
	tlsOpts := c.getTLSOptions()
	opts, err := istiogrpc.ClientOptions(nil, tlsOpts)
	if err != nil {
		return nil, err
	}
	opts = append(opts,
		grpc.WithPerRPCCredentials(c.provider),
		security.CARetryInterceptor(),
	)
	conn, err := grpc.Dial(c.opts.CAEndpoint, opts...)
	if err != nil {
		citadelClientLog.Errorf("Failed to connect to endpoint %s: %v", c.opts.CAEndpoint, err)
		return nil, fmt.Errorf("failed to connect to endpoint %s", c.opts.CAEndpoint)
	}

	return conn, nil
}

func (c *CitadelClient) reconnect() error {
	if err := c.conn.Close(); err != nil {
		return fmt.Errorf("failed to close connection: %v", err)
	}

	conn, err := c.buildConnection()
	if err != nil {
		return err
	}
	c.conn = conn
	c.client = pb.NewIstioCertificateServiceClient(conn)
	citadelClientLog.Info("recreated connection")
	return nil
}

// GetRootCertBundle: Citadel (Istiod) CA doesn't publish any endpoint to retrieve CA certs
func (c *CitadelClient) GetRootCertBundle() ([]string, error) {
	return []string{}, nil
}
