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

package signingapi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/api/security/v1alpha1"
	"istio.io/istio/pkg/mcp/creds"
	"istio.io/pkg/log"
)

var (
	signingLog = log.RegisterScope("SingingAPIClient", "", 0)
)

const (
	defaultRequestTimeout = 10 * time.Second
)

// Client create Istio Certificate Signing Client
type Client struct {
	Address          string
	RequestTimeout   time.Duration
	Conn             *grpc.ClientConn
	ClientV1         v1alpha1.IstioCertificateServiceClient
	TLSClientSetting *networking.ClientTLSSettings
	DialOption       []grpc.DialOption
}

// New create new Istio Signing Certificate Client
func New(address string, tlsSetting *networking.ClientTLSSettings, requestTimeout time.Duration) *Client {
	// default timeout is 10s
	if requestTimeout.Seconds() == 0 {
		requestTimeout = defaultRequestTimeout
	}
	return &Client{
		Address:          address,
		RequestTimeout:   requestTimeout,
		TLSClientSetting: tlsSetting,
		DialOption:       []grpc.DialOption{},
	}
}

// AddDialOptions add more dial options to gRPC Client
func (c *Client) AddDialOptions(dialOpts []grpc.DialOption) {
	c.DialOption = append(c.DialOption, dialOpts...)
}

// Connect start connect to Certificate Signing API
func (c *Client) Connect(tlsConfig *tls.Config) error {
	var opts []grpc.DialOption
	var err error
	if tlsConfig != nil {
		opts = []grpc.DialOption{
			grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
		}
	} else {
		clientSecOpts, err := c.getClientSecurityOptions(context.TODO())
		if err != nil {
			signingLog.Errorf("get client security option failed: %v", err)
			return fmt.Errorf("get client security option failed: %v", err)
		}

		opts = append(opts, clientSecOpts)
	}
	c.DialOption = append(opts, c.DialOption...)

	grpcConn, err := grpc.Dial(c.Address, c.DialOption...)

	if err != nil {
		signingLog.Errorf("cannot dial grpc connect to %v: %v", c.Address, err)
		return fmt.Errorf("cannot dial grpc connect to %v: %v", c.Address, err)
	}

	c.Conn = grpcConn
	c.ClientV1 = v1alpha1.NewIstioCertificateServiceClient(grpcConn)

	signingLog.Infof("connect to CA addr: %s", c.Address)
	return nil
}

func (c *Client) getClientSecurityOptions(ctx context.Context) (grpc.DialOption, error) {
	switch c.TLSClientSetting.Mode {
	case networking.ClientTLSSettings_DISABLE:
		return grpc.WithInsecure(), nil

	case networking.ClientTLSSettings_SIMPLE:
		if c.TLSClientSetting.CaCertificates == "" {
			return nil, fmt.Errorf("missing config: TLSClientSetting.CaCertificates should not empty")
		}
		rootCAPool := x509.NewCertPool()
		rootCACert, err := ioutil.ReadFile(c.TLSClientSetting.CaCertificates)
		if err != nil {
			signingLog.Errorf("read CA Certificate (%s) failed: %v", c.TLSClientSetting.CaCertificates, err)
			return nil, fmt.Errorf("read CA Certificate (%s) failed: %v", c.TLSClientSetting.CaCertificates, err)
		}
		if ok := rootCAPool.AppendCertsFromPEM(rootCACert); !ok {
			signingLog.Errorf("append ca cert to pool failed: %v", rootCACert)
			return nil, fmt.Errorf("append ca cert to pool failed: %v", rootCACert)
		}
		return grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{RootCAs: rootCAPool})), nil

	case networking.ClientTLSSettings_MUTUAL:
		var credentialOption *creds.Options

		if c.TLSClientSetting.CaCertificates == "" {
			signingLog.Infof("missing config for CaCertificates. Using system cert pools instead")
			systemCertPool, err := x509.SystemCertPool()
			if err != nil {
				signingLog.Errorf("get system cert pool failed: %v", err)
				return nil, fmt.Errorf("get system cert pool failed: %v", err)
			}
			trans := credentials.NewClientTLSFromCert(systemCertPool, c.TLSClientSetting.Sni)
			return grpc.WithTransportCredentials(trans), nil
		}

		credentialOption = &creds.Options{
			CertificateFile:   c.TLSClientSetting.ClientCertificate,
			KeyFile:           c.TLSClientSetting.PrivateKey,
			CACertificateFile: c.TLSClientSetting.CaCertificates,
		}
		watcher, err := creds.WatchFiles(ctx.Done(), credentialOption)
		if err != nil {
			signingLog.Errorf("create certificate files watcher failed: %v", err)
			return nil, fmt.Errorf("create certificate files watcher failed: %v", err)
		}
		return grpc.WithTransportCredentials(creds.CreateForClient(c.TLSClientSetting.Sni, watcher)), nil
	default:
		signingLog.Errorf("do not support TLS Client Mode: %v", c.TLSClientSetting.Mode)
		return nil, fmt.Errorf("do not support TLS Client Mode: %v", c.TLSClientSetting.Mode)
	}
}

// CreateCertificate call CreateCertificate of Istio Certificate Signing API
func (c *Client) CreateCertificate(ctx context.Context,
	req *v1alpha1.IstioCertificateRequest) (*v1alpha1.IstioCertificateResponse, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("istio certificate request client still not connected")
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, c.RequestTimeout)
	defer cancel()
	signingLog.Infof("invoke CreateCertificate to CA Address: %v", c.Address)
	resp, err := c.ClientV1.CreateCertificate(timeoutCtx, req)
	if err != nil {
		signingLog.Errorf("invoke CreateCertificate failed: %v", err)
		return nil, fmt.Errorf("invoke CreateCertificate failed: %v", err)
	}

	certChain := resp.GetCertChain()
	if len(certChain) < 2 {
		signingLog.Errorf("invalid CreateCertificate response: %v", resp.GetCertChain())
		return nil, fmt.Errorf("invalid CreateCertificate response: %v", resp.GetCertChain())
	}

	return resp, nil
}

// IsConnected checking client is connected
func (c *Client) IsConnected() bool {
	return c.Conn != nil && c.ClientV1 != nil
}
