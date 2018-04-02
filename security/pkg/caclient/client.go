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
	"fmt"
	"net"
	"time"

	"io/ioutil"

	"context"

	rgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"istio.io/istio/pkg/log"
	pkiutil "istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/platform"
	"istio.io/istio/security/pkg/workload"
	pb "istio.io/istio/security/proto"
)

// CAClient is a client to provision key and certificate from the upstream CA via CSR protocol.
type CAClient struct {
	platformClient         platform.Client
	maxRetries             int
	initialRetrialInterval time.Duration
	grpcClient             pb.IstioCAServiceClient
}

// NewCAClient creates a new CAClient instance.
func NewCAClient(pltfmc platform.Client, caAddr string, maxRetries int, interval time.Duration) (*CAClient, error) {
	if !pltfmc.IsProperPlatform() {
		return nil, fmt.Errorf("CA client is not running on the right platform") // nolint
	}
	if caAddr == "" {
		return nil, fmt.Errorf("istio CA address is empty")
	}
	dialOptions, err := pltfmc.GetDialOptions()
	if err != nil {
		return nil, fmt.Errorf("can't get platform dial options, %v", err)
	}
	conn, err := rgrpc.Dial(caAddr, dialOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to dial %s: %v", caAddr, err)
	}
	client := pb.NewIstioCAServiceClient(conn)
	return &CAClient{
		platformClient:         pltfmc,
		maxRetries:             maxRetries,
		initialRetrialInterval: interval,
		grpcClient:             client,
	}, nil
}

type TestCAServer struct {
	response *pb.CsrResponse
	errorMsg string
	counter  int
}

func (s *TestCAServer) HandleCSR(ctx context.Context, req *pb.CsrRequest) (*pb.CsrResponse, error) {
	s.counter++
	if len(s.errorMsg) > 0 {
		return nil, fmt.Errorf(s.errorMsg)
	}
	if s.response != nil {
		return s.response, nil
	}
	return &pb.CsrResponse{}, nil
}

func (s *TestCAServer) InvokeTimes() int {
	return s.counter
}

type TestCAServerOptions struct {
	Response *pb.CsrResponse
	Error    string
}

func NewTestCAServer(opts *TestCAServerOptions) (server *TestCAServer, addr string, err error) {
	s := rgrpc.NewServer()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, "", fmt.Errorf("failed to allocate address for server %v", err)
	}
	ca := &TestCAServer{
		response: opts.Response,
		errorMsg: opts.Error,
	}
	pb.RegisterIstioCAServiceServer(s, ca)
	reflection.Register(s)
	go s.Serve(lis)
	return ca, lis.Addr().String(), nil
}

// Retrieve sends the CSR to Istio CA with automatic retries. When successful, it returns the generated key
// and cert, otherwise, it returns error. This is a blocking function.
func (c *CAClient) Retrieve(options *pkiutil.CertOptions) (newCert []byte, certChain []byte, privateKey []byte, err error) {
	retries := 0
	retrialInterval := c.initialRetrialInterval
	for {
		privateKey, req, reqErr := c.CreateCSRRequest(options)
		if reqErr != nil {
			return nil, nil, nil, reqErr
		}
		log.Infof("Sending CSR (retrial #%d) ...", retries)

		resp, err := c.grpcClient.HandleCSR(context.Background(), req)
		if err == nil && resp != nil && resp.IsApproved {
			return resp.SignedCert, resp.CertChain, privateKey, nil
		}

		if retries >= c.maxRetries {
			return nil, nil, nil, fmt.Errorf(
				"CA client cannot get the CSR approved from Istio CA after max number of retries (%d)", c.maxRetries)
		}
		if err != nil {
			log.Errorf("CSR signing failed: %v. Will retry in %v", err, retrialInterval)
		} else if resp == nil {
			log.Errorf("CSR signing failed: response empty. Will retry in %v", retrialInterval)
		} else if !resp.IsApproved {
			log.Errorf("CSR signing failed: request not approved. Will retry in %v", retrialInterval)
		} else {
			log.Errorf("Certificate parsing error. Will retry in %v", retrialInterval)
		}
		retries++
		timer := time.NewTimer(retrialInterval)
		// Exponentially increase the backoff time.
		retrialInterval = retrialInterval * 2
		<-timer.C
	}
}

// CreateCSRRequest returns a CsrRequest based on the specified CertOptions.
// TODO(incfly): add SendCSR method directly to CAClient.
func (c *CAClient) CreateCSRRequest(opts *pkiutil.CertOptions) ([]byte, *pb.CsrRequest, error) {
	csr, privKey, err := pkiutil.GenCSR(*opts)
	if err != nil {
		return nil, nil, err
	}

	cred, err := c.platformClient.GetAgentCredential()
	if err != nil {
		return nil, nil, fmt.Errorf("request creation fails on getting platform credential (%v)", err)
	}

	return privKey, &pb.CsrRequest{
		CsrPem:              csr,
		NodeAgentCredential: cred,
		CredentialType:      c.platformClient.GetCredentialType(),
		// TODO(inclfy): verify current value matches default value.
		RequestedTtlMinutes: int32(opts.TTL.Minutes()),
		ForCA:               opts.IsCA,
	}, nil
}

// SaveKeyCert stores the specified key/cert into file specified by the path.
// TODO(incfly): move this into CAClient struct's own method later.
func SaveKeyCert(keyFile, certFile string, privKey, cert []byte) error {
	if err := ioutil.WriteFile(keyFile, privKey, workload.KeyFilePermission); err != nil {
		return err
	}
	return ioutil.WriteFile(certFile, cert, workload.CertFilePermission)
}
