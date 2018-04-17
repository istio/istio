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
	"strings"
	"time"

	"google.golang.org/grpc/balancer"

	"istio.io/istio/pkg/probe"
	"istio.io/istio/security/pkg/caclient/grpc"
	"istio.io/istio/security/pkg/pki/ca"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/platform"
	pb "istio.io/istio/security/proto"
)

const (
	// LivenessProbeClientIdentity is the default identity for the liveness probe check
	LivenessProbeClientIdentity   = "k8s.cluster.local"
	probeCheckRequestedTTLMinutes = 60
)

// LivenessCheckController updates the availability of the liveness probe of the CA instance
type LivenessCheckController struct {
	interval           time.Duration
	grpcHostname       string
	grpcPort           int
	serviceIdentityOrg string
	rsaKeySize         int
	ca                 *ca.IstioCA
	livenessProbe      *probe.Probe
	client             grpc.CAGrpcClient
}

// NewLivenessCheckController creates the liveness check controller instance
func NewLivenessCheckController(probeCheckInterval time.Duration,
	grpcHostname string, grpcPort int, ca *ca.IstioCA, livenessProbeOptions *probe.Options,
	client grpc.CAGrpcClient) (*LivenessCheckController, error) {

	livenessProbe := probe.NewProbe()
	livenessProbeController := probe.NewFileController(livenessProbeOptions)
	livenessProbe.RegisterProbe(livenessProbeController, "liveness")
	livenessProbeController.Start()

	// set initial status to good
	livenessProbe.SetAvailable(nil)

	return &LivenessCheckController{
		interval:      probeCheckInterval,
		grpcHostname:  grpcHostname,
		grpcPort:      grpcPort,
		rsaKeySize:    2048,
		livenessProbe: livenessProbe,
		ca:            ca,
		client:        client,
	}, nil
}

func (c *LivenessCheckController) checkGrpcServer() error {
	if c.grpcPort <= 0 {
		return nil
	}

	// generates certificate and private key for test
	opts := util.CertOptions{
		Host:       LivenessProbeClientIdentity,
		RSAKeySize: 2048,
	}

	csrPEM, privPEM, err := util.GenCSR(opts)
	if err != nil {
		return err
	}

	certPEM, err := c.ca.Sign(csrPEM, c.interval, false)
	if err != nil {
		return err
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
	pc := platform.NewOnPremClientImpl(testRoot.Name(), testKey.Name(), testCert.Name())

	csr, _, err := util.GenCSR(util.CertOptions{
		Host:       LivenessProbeClientIdentity,
		Org:        c.serviceIdentityOrg,
		RSAKeySize: c.rsaKeySize,
	})

	if err != nil {
		return err
	}

	cred, err := pc.GetAgentCredential()
	if err != nil {
		return err
	}

	req := &pb.CsrRequest{
		CsrPem:              csr,
		NodeAgentCredential: cred,
		CredentialType:      pc.GetCredentialType(),
		RequestedTtlMinutes: probeCheckRequestedTTLMinutes,
	}

	_, err = c.client.SendCSR(req, pc, fmt.Sprintf("%v:%v", c.grpcHostname, c.grpcPort))
	if err != nil && strings.Contains(err.Error(), balancer.ErrTransientFailure.Error()) {
		return nil
	}

	return err
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
