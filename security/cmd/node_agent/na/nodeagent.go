// Copyright 2017 Istio Authors
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

package na

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"istio.io/auth/pkg/pki/ca"
	"istio.io/auth/pkg/workload"
	pb "istio.io/auth/proto"
)

type platformSpecificRequest interface {
	GetDialOptions(*Config) ([]grpc.DialOption, error)
	// Whether the node agent is running on the right platform, e.g., if gcpPlatformImpl should only
	// run on GCE.
	IsProperPlatform() bool
	// Get the service identity.
	GetServiceIdentity() (string, error)
}

// CAGrpcClient is for implementing the GRPC client to talk to CA.
type CAGrpcClient interface {
	// Send CSR to the CA and gets the response or error.
	SendCSR(*pb.Request, platformSpecificRequest, *Config) (*pb.Response, error)
}

// cAGrpcClientImpl is an implementation of GRPC client to talk to CA.
type cAGrpcClientImpl struct {
}

// SendCSR sends CSR to CA through GRPC.
func (c *cAGrpcClientImpl) SendCSR(req *pb.Request, pr platformSpecificRequest, cfg *Config) (*pb.Response, error) {
	if cfg.IstioCAAddress == "" {
		return nil, fmt.Errorf("Istio CA address is empty")
	}
	dialOptions, err := pr.GetDialOptions(cfg)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.Dial(cfg.IstioCAAddress, dialOptions...)
	if err != nil {
		return nil, fmt.Errorf("Failed to dial %s: %s", cfg.IstioCAAddress, err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			glog.Errorf("Failed to close connection")
		}
	}()
	client := pb.NewIstioCAServiceClient(conn)
	resp, err := client.HandleCSR(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("CSR request failed %v", err)
	}
	return resp, nil
}

// The real node agent implementation. This implements the "Start" function
// in the NodeAgent interface.
type nodeAgentInternal struct {
	// Configuration specific to Node Agent
	config       *Config
	pr           platformSpecificRequest
	cAClient     CAGrpcClient
	identity     string
	secretServer workload.SecretServer
	certUtil     CertUtil
}

// Start starts the node Agent.
func (na *nodeAgentInternal) Start() error {
	if na.config == nil {
		return fmt.Errorf("node Agent configuration is nil")
	}

	if !na.pr.IsProperPlatform() {
		return fmt.Errorf("node Agent is not running on the right platform")
	}

	glog.Infof("Node Agent starts successfully.")

	retries := 0
	retrialInterval := na.config.CSRInitialRetrialInterval
	identity, err := na.pr.GetServiceIdentity()
	if err != nil {
		return err
	}
	na.identity = identity
	var success bool
	for {
		privateKey, req, reqErr := na.createRequest()
		if reqErr != nil {
			return reqErr
		}

		glog.Infof("Sending CSR (retrial #%d) ...", retries)

		resp, err := na.cAClient.SendCSR(req, na.pr, na.config)
		if err == nil && resp != nil && resp.IsApproved {
			waitTime, ttlErr := na.certUtil.GetWaitTime(
				resp.SignedCertChain, time.Now(), na.config.CSRGracePeriodPercentage)
			if ttlErr != nil {
				glog.Errorf("Error getting TTL from approved cert: %v", ttlErr)
				success = false
			} else {
				if writeErr := na.secretServer.SetServiceIdentityCert(resp.SignedCertChain); writeErr != nil {
					return writeErr
				}
				if writeErr := na.secretServer.SetServiceIdentityPrivateKey(privateKey); writeErr != nil {
					return writeErr
				}
				glog.Infof("CSR is approved successfully. Will renew cert in %s", waitTime.String())
				retries = 0
				retrialInterval = na.config.CSRInitialRetrialInterval
				timer := time.NewTimer(waitTime)
				<-timer.C
				success = true
			}
		} else {
			success = false
		}

		if !success {
			if retries >= na.config.CSRMaxRetries {
				return fmt.Errorf(
					"node agent can't get the CSR approved from Istio CA after max number of retries (%d)", na.config.CSRMaxRetries)
			}
			if err != nil {
				glog.Errorf("CSR signing failed: %v. Will retry in %s", err, retrialInterval.String())
			} else if resp == nil {
				glog.Errorf("CSR signing failed: response empty. Will retry in %s", retrialInterval.String())
			} else if !resp.IsApproved {
				glog.Errorf("CSR signing failed: request not approved. Will retry in %s", retrialInterval.String())
			} else {
				glog.Errorf("Certificate parsing error. Will retry in %s", retrialInterval.String())
			}
			retries++
			timer := time.NewTimer(retrialInterval)
			// Exponentially increase the backoff time.
			retrialInterval = retrialInterval * 2
			<-timer.C
		}
	}
}

func (na *nodeAgentInternal) createRequest() ([]byte, *pb.Request, error) {
	csr, privKey, err := ca.GenCSR(ca.CertOptions{
		Host:       na.identity,
		Org:        na.config.ServiceIdentityOrg,
		RSAKeySize: na.config.RSAKeySize,
	})

	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate CSR: %v", err)
	}

	return privKey, &pb.Request{CsrPem: csr}, nil
}
