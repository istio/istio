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
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"go.uber.org/atomic"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/structpb"

	pb "istio.io/api/security/v1alpha1"
	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/nodeagent/caclient"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/pkg/log"
)

const (
	bearerTokenPrefix = "Bearer "
)

var citadelClientLog = log.RegisterScope("citadelclient", "citadel client debugging", 0)

type CitadelClient struct {
	// It means enable tls connection to Citadel if this is not nil.
	tlsOpts   *TLSOptions
	client    pb.IstioCertificateServiceClient
	conn      *grpc.ClientConn
	provider  *caclient.TokenProvider
	opts      *security.Options
	usingMtls *atomic.Bool
}

type TLSOptions struct {
	RootCert string
	Key      string
	Cert     string
}

// NewCitadelClient create a CA client for Citadel.
func NewCitadelClient(opts *security.Options, tlsOpts *TLSOptions) (*CitadelClient, error) {
	c := &CitadelClient{
		tlsOpts:   tlsOpts,
		opts:      opts,
		provider:  caclient.NewCATokenProvider(opts),
		usingMtls: atomic.NewBool(false),
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
func (c *CitadelClient) CSRSign(csrPEM []byte, certValidTTLInSec int64) ([]string, error) {
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

	if err := c.reconnectIfNeeded(); err != nil {
		return nil, err
	}

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("ClusterID", c.opts.ClusterID))
	resp, err := c.client.CreateCertificate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %v", err)
	}

	if len(resp.CertChain) <= 1 {
		return nil, errors.New("invalid empty CertChain")
	}

	return resp.CertChain, nil
}

func (c *CitadelClient) getTLSDialOption() (grpc.DialOption, error) {
	certPool, err := getRootCertificate(c.tlsOpts.RootCert)
	if err != nil {
		return nil, err
	}
	config := tls.Config{
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			var certificate tls.Certificate
			key, cert := c.tlsOpts.Key, c.tlsOpts.Cert
			if cert != "" {
				var isExpired bool
				isExpired, err = util.IsCertExpired(cert)
				if err != nil {
					citadelClientLog.Warnf("cannot parse the cert chain, using token instead: %v", err)
					return &certificate, nil
				}
				if isExpired {
					citadelClientLog.Warnf("cert expired, using token instead")
					return &certificate, nil
				}

				// Load the certificate from disk
				certificate, err = tls.LoadX509KeyPair(cert, key)
				if err != nil {
					return nil, err
				}
				c.usingMtls.Store(true)
			}
			return &certificate, nil
		},
		RootCAs:    certPool,
		MinVersion: tls.VersionTLS12,
	}

	if host, _, err := net.SplitHostPort(c.opts.CAEndpoint); err != nil {
		config.ServerName = host
	}
	// For debugging on localhost (with port forward)
	// TODO: remove once istiod is stable and we have a way to validate JWTs locally
	if strings.Contains(c.opts.CAEndpoint, "localhost") {
		config.ServerName = "istiod.istio-system.svc"
	}
	if c.opts.CAEndpointSAN != "" {
		config.ServerName = c.opts.CAEndpointSAN
	}

	transportCreds := credentials.NewTLS(&config)
	return grpc.WithTransportCredentials(transportCreds), nil
}

func getRootCertificate(rootCertFile string) (*x509.CertPool, error) {
	if rootCertFile == "" {
		// No explicit certificate - assume the citadel-compatible server uses a public cert
		certPool, err := x509.SystemCertPool()
		if err != nil {
			return nil, err
		}
		citadelClientLog.Info("Citadel client using system cert")
		return certPool, nil
	}

	certPool := x509.NewCertPool()
	rootCert, err := os.ReadFile(rootCertFile)
	if err != nil {
		return nil, err
	}
	ok := certPool.AppendCertsFromPEM(rootCert)
	if !ok {
		return nil, fmt.Errorf("failed to append certificates")
	}
	citadelClientLog.Info("Citadel client using custom root cert: ", rootCertFile)
	return certPool, nil
}

func (c *CitadelClient) buildConnection() (*grpc.ClientConn, error) {
	var opts grpc.DialOption
	var err error
	// CA tls disabled
	if c.tlsOpts == nil {
		opts = grpc.WithTransportCredentials(insecure.NewCredentials())
	} else {
		opts, err = c.getTLSDialOption()
		if err != nil {
			return nil, err
		}
	}

	conn, err := grpc.Dial(c.opts.CAEndpoint,
		opts,
		grpc.WithPerRPCCredentials(c.provider),
		security.CARetryInterceptor())
	if err != nil {
		citadelClientLog.Errorf("Failed to connect to endpoint %s: %v", c.opts.CAEndpoint, err)
		return nil, fmt.Errorf("failed to connect to endpoint %s", c.opts.CAEndpoint)
	}

	return conn, nil
}

func (c *CitadelClient) reconnectIfNeeded() error {
	if c.opts.ProvCert == "" || c.usingMtls.Load() {
		// No need to reconnect, already using mTLS or never will use it
		return nil
	}
	_, err := tls.LoadX509KeyPair(c.tlsOpts.Cert, c.tlsOpts.Key)
	if err != nil {
		// Cannot load the certificates yet, don't both reconnecting
		return nil
	}

	if err := c.conn.Close(); err != nil {
		return fmt.Errorf("failed to close connection")
	}

	conn, err := c.buildConnection()
	if err != nil {
		return err
	}
	c.conn = conn
	c.client = pb.NewIstioCertificateServiceClient(conn)
	citadelClientLog.Errorf("recreated connection")
	return nil
}

// GetRootCertBundle: Citadel (Istiod) CA doesn't publish any endpoint to retrieve CA certs
func (c *CitadelClient) GetRootCertBundle() ([]string, error) {
	return []string{}, nil
}
