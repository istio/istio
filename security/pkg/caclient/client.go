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
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/caclient/grpc"
	pkiutil "istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/platform"
	pb "istio.io/istio/security/proto"
)

// CAClient is a client to provision key and certificate from the upstream server.
type CAClient interface {
	RetrieveNewKeyCert() (newCert []byte, certChain []byte, privateKey []byte, err error)
}

// The cAClientImpl wraps details of the CSR protocol.
type cAClientImpl struct {
	platformClient platform.Client
	protocolClient grpc.CAGrpcClient
	istioCAAddress string

	identity    string
	identityOrg string
	rSAKeySize  int
	ttl         time.Duration
	forCA       bool

	maxRetries             int
	initialRetrialInterval time.Duration
}

// NewCAClient creates a new CAClient instance.
func NewCAClient(pltfmc platform.Client, ptclc grpc.CAGrpcClient, cAAddr string, org string, keySize int, ttl time.Duration,
	forCA bool, maxRetries int, interval time.Duration) (CAClient, error) {
	if !pltfmc.IsProperPlatform() {
		return nil, fmt.Errorf("CA client is not running on the right platform") // nolint
	}
	id, err := pltfmc.GetServiceIdentity()
	if err != nil {
		return nil, err
	}
	return &cAClientImpl{
		platformClient:         pltfmc,
		protocolClient:         ptclc,
		istioCAAddress:         cAAddr,
		identity:               id,
		identityOrg:            org,
		rSAKeySize:             keySize,
		ttl:                    ttl,
		forCA:                  forCA,
		maxRetries:             maxRetries,
		initialRetrialInterval: interval,
	}, nil
}

// RetrieveNewKeyCert sends the CSR to Istio CA with automatic retries. When successful, it returns the generated key
// and cert, otherwise, it returns error. This is a blocking function.
func (c *cAClientImpl) RetrieveNewKeyCert() (newCert []byte, certChain []byte, privateKey []byte, err error) {
	retries := 0
	retrialInterval := c.initialRetrialInterval
	for {
		privateKey, req, reqErr := c.createRequest()
		if reqErr != nil {
			return nil, nil, nil, reqErr
		}

		log.Infof("Sending CSR (retrial #%d) ...", retries)

		resp, err := c.protocolClient.SendCSR(req, c.platformClient, c.istioCAAddress)
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

func (c *cAClientImpl) createRequest() ([]byte, *pb.CsrRequest, error) {
	csr, privKey, err := pkiutil.GenCSR(pkiutil.CertOptions{
		Host:       c.identity,
		Org:        c.identityOrg,
		RSAKeySize: c.rSAKeySize,
	})
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
		RequestedTtlMinutes: int32(c.ttl.Minutes()),
		ForCA:               c.forCA,
	}, nil
}
