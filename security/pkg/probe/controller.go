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

package probe

import (
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"google.golang.org/grpc"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
	"istio.io/istio/security/pkg/caclient/protocol"
	"istio.io/istio/security/pkg/pki/ca"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/platform"
	pb "istio.io/istio/security/proto"
)

const (
	// LivenessProbeClientIdentity is the default identity for the liveness probe check
	LivenessProbeClientIdentity   = "k8s.cluster.local"
	probeCheckRequestedTTLMinutes = 60
	// logEveryNChecks specifies we log once in every N successful checks.
	logEveryNChecks = 100
)

// CAProtocolProvider returns a CAProtocol instance for talking to CA.
type CAProtocolProvider func(caAddress string, dialOpts []grpc.DialOption) (protocol.CAProtocol, error)

// GrpcProtocolProvider returns a CAProtocol instance talking to CA via gRPC.
func GrpcProtocolProvider(caAddress string, dialOpts []grpc.DialOption) (protocol.CAProtocol, error) {
	return protocol.NewGrpcConnection(caAddress, dialOpts)
}

// LivenessCheckController updates the availability of the liveness probe of the CA instance
type LivenessCheckController struct {
	interval           time.Duration
	serviceIdentityOrg string
	rsaKeySize         int
	caAddress          string
	ca                 *ca.IstioCA
	livenessProbe      *probe.Probe
	provider           CAProtocolProvider
	checkCount         int
}

// NewLivenessCheckController creates the liveness check controller instance
func NewLivenessCheckController(probeCheckInterval time.Duration, caAddr string,
	ca *ca.IstioCA, livenessProbeOptions *probe.Options,
	provider CAProtocolProvider) (*LivenessCheckController, error) {
	livenessProbe := probe.NewProbe()
	livenessProbeController := probe.NewFileController(livenessProbeOptions)
	livenessProbe.RegisterProbe(livenessProbeController, "liveness")
	livenessProbeController.Start()

	// set initial status to good
	livenessProbe.SetAvailable(nil)

	return &LivenessCheckController{
		interval:      probeCheckInterval,
		rsaKeySize:    2048,
		livenessProbe: livenessProbe,
		ca:            ca,
		caAddress:     caAddr,
		provider:      provider,
		checkCount:    0,
	}, nil
}

func (c *LivenessCheckController) checkGrpcServer() error {
	// generates certificate and private key for test
	opts := util.CertOptions{
		Host:       LivenessProbeClientIdentity,
		RSAKeySize: 2048,
	}

	csrPEM, privPEM, err := util.GenCSR(opts)
	if err != nil {
		return err
	}

	certPEM, signErr := c.ca.Sign(csrPEM, []string{LivenessProbeClientIdentity}, c.interval, false)
	if signErr != nil {
		return signErr.(ca.Error)
	}

	// Store certificate chain and private key to generate CSR
	tempDir, err := ioutil.TempDir("/tmp", "caprobe")
	if err != nil {
		return err
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	testRoot, err := ioutil.TempFile(tempDir, "root")
	if err != nil {
		return err
	}

	testCert, err := ioutil.TempFile(tempDir, "cert")
	if err != nil {
		return err
	}

	testKey, err := ioutil.TempFile(tempDir, "priv")
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(testCert.Name(), certPEM, 0644)
	if err != nil {
		return err
	}

	_, _, _, rootCertBytes := c.ca.GetCAKeyCertBundle().GetAll()
	err = ioutil.WriteFile(testRoot.Name(), rootCertBytes, 0644)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(testKey.Name(), privPEM, 0644)
	if err != nil {
		return err
	}

	// Generate csr and credential
	pc, err := platform.NewOnPremClientImpl(testRoot.Name(), testKey.Name(), testCert.Name())
	if err != nil {
		return err
	}

	csr, privKeyBytes, err := util.GenCSR(util.CertOptions{
		Host:       LivenessProbeClientIdentity,
		Org:        c.serviceIdentityOrg,
		RSAKeySize: c.rsaKeySize,
	})
	if err != nil {
		return err
	}

	dialOpts, err := pc.GetDialOptions()
	if err != nil {
		return err
	}

	cred, err := pc.GetAgentCredential()
	if err != nil {
		return err
	}
	caProtocol, err := c.provider(c.caAddress, dialOpts)

	if err != nil {
		return err
	}

	req := &pb.CsrRequest{
		CsrPem:              csr,
		NodeAgentCredential: cred,
		CredentialType:      pc.GetCredentialType(),
		RequestedTtlMinutes: probeCheckRequestedTTLMinutes,
	}
	resp, err := caProtocol.SendCSR(req)

	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("CSR sign failure: response is nil")
	}
	if !resp.IsApproved {
		return fmt.Errorf("CSR sign failure: request is not approaved")
	}
	if vErr := util.Verify(resp.SignedCert, privKeyBytes, resp.CertChain, rootCertBytes); vErr != nil {
		err := fmt.Errorf("CSR sign failure: %v", vErr)
		log.Errora(err)
		return err
	}

	c.checkCount++
	if c.checkCount%logEveryNChecks == 1 {
		log.Infof("CSR signing service is healthy (logged every %d times).", logEveryNChecks)
	}

	log.Debugf("CSR signing service is healthy.")

	return nil
}

// Run starts the check routine
func (c *LivenessCheckController) Run() {
	go func() {
		t := time.NewTicker(c.interval)
		for range t.C {
			c.livenessProbe.SetAvailable(c.checkGrpcServer())
		}
	}()
}
