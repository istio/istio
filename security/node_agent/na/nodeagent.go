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
	"io/ioutil"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"istio.io/auth/pkg/pki/ca"
	pb "istio.io/auth/proto"
)

// Config is Node agent configuration that is provided from CLI.
type Config struct {
	// Root CA cert file
	RootCACertFile *string

	// Node Identity key file
	NodeIdentityPrivateKeyFile *string

	// Node Identity certificate file
	NodeIdentityCertFile *string

	// Service Identity
	ServiceIdentity *string

	// Service Identity
	ServiceIdentityOrg *string

	// Directory where service identity private key and certificate
	// are written.
	ServiceIdentityDir *string

	RSAKeySize *int

	// cert renewal cutoff
	PercentageExpirationTime *int

	// Istio CA grpc server
	IstioCAAddress *string
}

// This interface is provided for implementing platform specific code.
type platformSpecificRequest interface {
	getTransportCredentials(*Config) credentials.TransportCredentials
	getNodeAgentCredentials(*Config) *pb.NodeAgentCredentials
}

// The real node agent implementation. This implements the "Start" function
// in the NodeAgent interface.
type nodeAgentInternal struct {
	// Configuration specific to Node Agent
	config *Config
	pr     platformSpecificRequest
}

// Start the node Agent.
func (na nodeAgentInternal) Start() {

	if na.config == nil {
		glog.Fatalf("Node Agent configuration is nil")
	}

	for {
		ok, privKey, resp := na.invokeGrpc()
		if ok && resp.IsApproved {
			timer := time.NewTimer(na.getExpTime(resp))
			na.writeToFile(privKey, resp.SignedCertChain)
			<-timer.C
		} else {
			glog.Errorf("CSR signing failed: %s", resp.Status)
		}
	}
}

func (na *nodeAgentInternal) getCertificateSignRequest() ([]byte, *pb.CertificateSignRequest) {
	csr, privKey, err := ca.GenCSR(ca.CertOptions{
		Host:       *na.config.ServiceIdentity,
		Org:        *na.config.ServiceIdentityOrg,
		RSAKeySize: *na.config.RSAKeySize,
	})

	if err != nil {
		glog.Fatalf("Failed to generate CSR: %s", err)
	}

	return privKey, &pb.CertificateSignRequest{
		Csr:                  csr,
		NodeAgentCredentials: na.pr.getNodeAgentCredentials(na.config),
	}
}

func (na *nodeAgentInternal) invokeGrpc() (bool, []byte, *pb.CertificateSignResponse) {

	transportCreds := na.pr.getTransportCredentials(na.config)
	dialOption := grpc.WithTransportCredentials(transportCreds)
	conn, err := grpc.Dial(*na.config.IstioCAAddress, dialOption)
	if err != nil {
		glog.Fatalf("Failed ot dial %s: %s", *na.config.IstioCAAddress, err)
	}

	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			glog.Fatalf("Failed ot close connection")
		}
	}()

	client := pb.NewIstioCAServiceClient(conn)
	privKey, req := na.getCertificateSignRequest()
	resp, err := client.Sign(context.Background(), req)
	if err != nil {
		glog.Errorf("CSR request failed %s", err)
		return false, nil, nil
	}

	return true, privKey, resp
}

func (na *nodeAgentInternal) writeToFile(privKey []byte, cert []byte) {
	if err := ioutil.WriteFile("serviceIdentityKey.pem", privKey, 0600); err != nil {
		glog.Fatalf("Cannot write service identity private key file")
	}
	if err := ioutil.WriteFile("serviceIdentityCert.pem", cert, 0644); err != nil {
		glog.Fatalf("Cannot write service identity certificate file")
	}
}

func (na *nodeAgentInternal) getExpTime(resp *pb.CertificateSignResponse) time.Duration {
	return 0
}
