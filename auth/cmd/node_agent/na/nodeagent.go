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
	"io/ioutil"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"istio.io/auth/pkg/pki"
	"istio.io/auth/pkg/pki/ca"
	pb "istio.io/auth/proto"
)

const (
	// ONPREM Node Agent
	ONPREM int = iota // 0
	// GCP Node Agent
	GCP // 1
)

// This interface is provided for implementing platform specific code.
type platformSpecificRequest interface {
	GetDialOptions(*Config) ([]grpc.DialOption, error)
	// Whether the node agent is running on the right platform, e.g., if gcpPlatformImpl should only
	// run on GCE.
	IsProperPlatform() bool
}

// CAGrpcClient is for implementing the GRPC client to talk to CA.
type CAGrpcClient interface {
	// Send CSR to the CA and gets the response or error.
	SendCSR(*pb.Request) (*pb.Response, error)
}

// NewCAGrpcClient creates an implementation of CAGrpcClient.
func NewCAGrpcClient(cfg *Config, pr platformSpecificRequest) (CAGrpcClient, error) {
	if cfg.IstioCAAddress == "" {
		return nil, fmt.Errorf("Istio CA address is empty")
	}
	dialOptions, optionErr := pr.GetDialOptions(cfg)
	if optionErr != nil {
		return nil, optionErr
	}
	return &cAGrpcClientImpl{
		&cfg.IstioCAAddress,
		dialOptions,
	}, nil
}

// cAGrpcClientImpl is an implementation of GRPC client to talk to CA.
type cAGrpcClientImpl struct {
	cAAddress   *string
	dialOptions []grpc.DialOption
}

// SendCSR sends CSR to CA through GRPC.
func (c *cAGrpcClientImpl) SendCSR(req *pb.Request) (*pb.Response, error) {
	conn, err := grpc.Dial(*c.cAAddress, c.dialOptions...)
	if err != nil {
		return nil, fmt.Errorf("Failed to dial %s: %s", *c.cAAddress, err)
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
	config   *Config
	pr       platformSpecificRequest
	cAClient CAGrpcClient
}

// Start the node Agent.
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
	var success bool
	for {
		privKey, req, reqErr := na.createRequest()
		if reqErr != nil {
			return reqErr
		}

		glog.Infof("Sending CSR (retrial #%d) ...", retries)

		resp, err := na.cAClient.SendCSR(req)
		if err == nil && resp != nil && resp.IsApproved {
			waitTime, ttlErr := na.getWaitTimeFromCert(
				resp.SignedCertChain, time.Now(), na.config.CSRGracePeriodPercentage)
			if ttlErr != nil {
				glog.Errorf("Error getting TTL from approved cert: %v", ttlErr)
				success = false
			} else {
				writeErr := na.writeToFile(privKey, resp.SignedCertChain)
				if writeErr != nil {
					return fmt.Errorf("file write error: %v", writeErr)
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
		Host:       na.config.ServiceIdentity,
		Org:        na.config.ServiceIdentityOrg,
		RSAKeySize: na.config.RSAKeySize,
	})

	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate CSR: %v", err)
	}

	return privKey, &pb.Request{CsrPem: csr}, nil
}

func (na *nodeAgentInternal) getWaitTimeFromCert(
	certBytes []byte, now time.Time, gracePeriodPercentage int) (time.Duration, error) {
	cert, certErr := pki.ParsePemEncodedCertificate(certBytes)
	if certErr != nil {
		return time.Duration(0), certErr
	}
	timeToExpire := cert.NotAfter.Sub(now)
	if timeToExpire < 0 {
		return time.Duration(0), fmt.Errorf("certificate already expired at %s, but now is %s",
			cert.NotAfter, now)
	}
	gracePeriod := cert.NotAfter.Sub(cert.NotBefore) * time.Duration(gracePeriodPercentage) / time.Duration(100)
	// waitTime is the duration between now and the grace period starts.
	// It is the time until cert expiration minus the length of grace period.
	waitTime := timeToExpire - gracePeriod
	if waitTime < 0 {
		// We are within the grace period.
		return time.Duration(0), fmt.Errorf("got a certificate that should be renewed now")
	}
	return waitTime, nil
}

func (na *nodeAgentInternal) writeToFile(privKey []byte, cert []byte) error {
	glog.Infof("Write key and cert to local file.")
	if err := ioutil.WriteFile("/etc/certs/key.pem", privKey, 0600); err != nil {
		return fmt.Errorf("cannot write service identity private key file")
	}
	if err := ioutil.WriteFile("/etc/certs/cert-chain.pem", cert, 0644); err != nil {
		return fmt.Errorf("cannot write service identity certificate file")
	}
	return nil
}
