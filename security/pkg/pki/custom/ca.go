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

package custom

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"istio.io/pkg/log"

	pb "istio.io/api/security/v1alpha1"

	"istio.io/istio/security/pkg/pki/util"
)

const (
	// rsaKeySize the standard key size to use when generating an RSA private key
	rsaKeySize = 2048
)

var cLog = log.RegisterScope("CustomCAClient", "Custom CA Integration Log", 0)

// CAClientOpts options
type CAClientOpts struct {
	CAAddr           string
	KeyCertBundle    util.KeyCertBundle
	RootCertFilePath string
}

// CAClient generates keys and certificates for Istio identities.
type CAClient struct {
	opts         *CAClientOpts
	caClientConn *grpc.ClientConn
	pbClient     pb.IstioCertificateServiceClient
	serverTLS    *tls.Config
}

const (
	retryPolicy = `{
		"methodConfig": [{
		  "name": [{"service": "istio.v1.auth.IstioCertificateService"}],
		  "waitForReady": true,
		  "retryPolicy": {
			  "MaxAttempts": 4,
			  "InitialBackoff": ".01s",
			  "MaxBackoff": ".01s",
			  "BackoffMultiplier": 1.0,
			  "RetryableStatusCodes": [ "UNAVAILABLE" ]
		  }
		}]}`
	requestTimeout = 10 * time.Second
	connectTimeout = 30 * time.Second
)

// NewCAClient returns a new CAClient instance.
func NewCAClient(opts *CAClientOpts) (*CAClient, error) {

	c := &CAClient{
		opts: opts,
	}

	err := c.connectToCustomCA()
	if err != nil {
		cLog.Errorf("can not connect to Custom CA Address: %v", err)
		return nil, fmt.Errorf("can not connect to Custom CA Address: %v", err)
	}

	return c, nil
}

func (c *CAClient) connectToCustomCA() error {

	cLog.Infof("connect to custom CA addr: %s", c.opts.CAAddr)
	cLog.Infof("using root-cert from KeyCertBundle: %v", string(c.opts.KeyCertBundle.GetRootCertPem()))

	grpcConn, err := grpc.Dial(c.opts.CAAddr, grpc.WithInsecure(), grpc.WithDefaultServiceConfig(retryPolicy))

	if err != nil {
		cLog.Errorf("cannot dial grpc connect to %v: %v", c.opts.CAAddr, err)
		return fmt.Errorf("cannot dial grpc connect to %v: %v", c.opts.CAAddr, err)
	}

	timeout := time.After(connectTimeout)
	tick := time.Tick(300 * time.Millisecond)

	// Keep trying until we're timed out or got a connection state READY
	for {
		select {
		case <-timeout:
			cLog.Errorf("cannot connect to Custom CA with timeout, state: %v", grpcConn.GetState())
			return fmt.Errorf("cannot connect to Custom CA with timeout, state: %v", grpcConn.GetState())
		case <-tick:
			if grpcConn.GetState() == connectivity.Ready {
				c.caClientConn = grpcConn
				c.pbClient = pb.NewIstioCertificateServiceClient(grpcConn)
				cLog.Info("Custom CA connection is ready")
				return nil
			}
			cLog.Warnf("conection of custom CA is not ready: %v, waiting...", grpcConn.GetState())
		}
	}
}

// CreateCertificate is similar to Sign but returns the leaf cert and the entire cert chain.
func (c *CAClient) CreateCertificate(ctx context.Context,
	req *pb.IstioCertificateRequest) (*pb.IstioCertificateResponse, error) {
	csr, err := util.ParsePemEncodedCSR([]byte(req.Csr))
	if err != nil {
		cLog.Errorf("request invalid CSR: %v", err)
		return nil, fmt.Errorf("request invalid CSR: %v", err)
	}

	cLog.Infof("CreateCertificate by Custom CA with URI: %v", csr.URIs)

	timeoutCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	resp, err := c.pbClient.CreateCertificate(timeoutCtx, req)
	certChain := resp.GetCertChain()

	if err != nil {
		cLog.Errorf("cannot call CreateCertificate from Custom CA: %v", err)
		return nil, fmt.Errorf("cannot call CreateCertificate from Custom CA: %v", err)
	}

	if len(certChain) < 2 {
		cLog.Errorf("invalid certificate response: %v", resp.GetCertChain())
		return nil, fmt.Errorf("invalid certificate response: %v", resp.GetCertChain())
	}
	var responseCertChains []string

	for _, cert := range certChain {
		parsedCert, err := validateAndParseCert(cert)
		if err != nil {
			cLog.Errorf("response certificate from Custom CA is invalid: %v", err)
			return nil, fmt.Errorf("response certificate from Custom CA is invalid: %v", err)
		}
		responseCertChains = append(responseCertChains, parsedCert)
	}
	rootCertBytes := c.opts.KeyCertBundle.GetRootCertPem()
	// if len(certChain) > 0 {
	// 	responseCertChains = append(responseCertChains, string(certChains))
	// }
	responseCertChains = append(responseCertChains, string(rootCertBytes))

	return &pb.IstioCertificateResponse{
		CertChain: responseCertChains,
	}, nil
}

func validateAndParseCert(cert string) (string, error) {
	certBytes, _ := pem.Decode([]byte(cert))

	if certBytes == nil {
		return "", fmt.Errorf("input cert is invalid")
	}

	block := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes.Bytes,
	}

	c := pem.EncodeToMemory(block)
	return string(c), nil
}
