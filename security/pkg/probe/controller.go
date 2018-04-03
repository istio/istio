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
	"context"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc/balancer"

	"google.golang.org/grpc"

	"istio.io/istio/pkg/probe"
	"istio.io/istio/security/pkg/pki/ca"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/platform"
	"istio.io/istio/security/pkg/workload"
	pb "istio.io/istio/security/proto"
)

const (
	// LivenessProbeClientIdentity is the default identity for the liveness probe check
	LivenessProbeClientIdentity   = "k8s.cluster.local"
	probeCheckRequestedTTLMinutes = 60
)

// CAChecker contains informations for prober to talk to Istio CA server.
type CAChecker struct {
	request *pb.CsrRequest
	client  pb.IstioCAServiceClient
	cleanup func()
}

// CheckProvider returns a struct containing the CsrRequest and the grpc client.
type CheckProvider func(addr string, ca *ca.IstioCA, certOpts *util.CertOptions, ttl time.Duration) (*CAChecker, error)

// LivenessCheckController updates the availability of the liveness probe of the CA instance
type LivenessCheckController struct {
	interval           time.Duration
	serviceIdentityOrg string
	rsaKeySize         int
	caAddress          string
	ca                 *ca.IstioCA
	livenessProbe      *probe.Probe
	clientProvider     CheckProvider
}

// NewLivenessCheckController creates the liveness check controller instance
func NewLivenessCheckController(probeCheckInterval time.Duration, caAddr string,
	ca *ca.IstioCA, livenessProbeOptions *probe.Options,
	provider CheckProvider) (*LivenessCheckController, error) {
	livenessProbe := probe.NewProbe()
	livenessProbeController := probe.NewFileController(livenessProbeOptions)
	livenessProbe.RegisterProbe(livenessProbeController, "liveness")
	livenessProbeController.Start()

	// set initial status to good
	livenessProbe.SetAvailable(nil)

	return &LivenessCheckController{
		interval:       probeCheckInterval,
		rsaKeySize:     2048,
		livenessProbe:  livenessProbe,
		ca:             ca,
		caAddress:      caAddr,
		clientProvider: provider,
	}, nil
}

func (c *LivenessCheckController) checkGrpcServer() error {
	checker, err := c.clientProvider(c.caAddress, c.ca, &util.CertOptions{
		Host:       LivenessProbeClientIdentity,
		Org:        c.serviceIdentityOrg,
		RSAKeySize: c.rsaKeySize,
	}, c.interval)
	if err != nil {
		return err
	}
	defer checker.cleanup()
	_, err = checker.client.HandleCSR(context.Background(), checker.request)

	// TODO(incfly): remove connectivity error once we always expose istio-ca into dns server.
	if err != nil && strings.Contains(err.Error(), balancer.ErrTransientFailure.Error()) {
		return nil
	}
	return err
}

// DefaultCheckProvider returns a CAChecker for prober to invoke.
func DefaultCheckProvider(addr string, ca *ca.IstioCA, certOpts *util.CertOptions, ttl time.Duration) (*CAChecker, error) {
	// generates certificate and private key for test
	opts := util.CertOptions{
		Host:       LivenessProbeClientIdentity,
		RSAKeySize: 2048,
	}

	csrPEM, privPEM, err := util.GenCSR(opts)
	if err != nil {
		return nil, err
	}

	certPEM, err := ca.Sign(csrPEM, ttl, false)
	if err != nil {
		return nil, err
	}

	// Store certificate chain and private key to generate CSR
	tempDir, err := ioutil.TempDir("/tmp", "caprobe")
	if err != nil {
		return nil, err
	}
	testRoot, err := ioutil.TempFile(tempDir, "root")
	if err != nil {
		return nil, err
	}

	testCert, err := ioutil.TempFile(tempDir, "cert")
	if err != nil {
		return nil, err
	}

	testKey, err := ioutil.TempFile(tempDir, "priv")
	if err != nil {
		return nil, err
	}

	err = ioutil.WriteFile(testCert.Name(), certPEM, workload.CertFilePermission)
	if err != nil {
		return nil, err
	}

	_, _, _, rootCertBytes := ca.GetCAKeyCertBundle().GetAll()
	err = ioutil.WriteFile(testRoot.Name(), rootCertBytes, workload.CertFilePermission)
	if err != nil {
		return nil, err
	}

	err = ioutil.WriteFile(testKey.Name(), privPEM, workload.CertFilePermission)
	if err != nil {
		return nil, err
	}

	// Generate csr and credential
	pc, err := platform.NewOnPremClientImpl(testRoot.Name(), testKey.Name(), testCert.Name())
	if err != nil {
		return nil, err
	}
	dialOpts, err := pc.GetDialOptions()
	if err != nil {
		return nil, err
	}
	conn, err := grpc.Dial(addr, dialOpts...)
	if err != nil {
		return nil, err
	}
	csr, _, err := util.GenCSR(*certOpts)
	if err != nil {
		return nil, err
	}

	cred, err := pc.GetAgentCredential()
	if err != nil {
		return nil, err
	}

	return &CAChecker{
		request: &pb.CsrRequest{
			CsrPem:              csr,
			NodeAgentCredential: cred,
			CredentialType:      pc.GetCredentialType(),
			RequestedTtlMinutes: probeCheckRequestedTTLMinutes,
		},
		client: pb.NewIstioCAServiceClient(conn),
		cleanup: func() {
			_ = os.RemoveAll(tempDir)
		},
	}, nil
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
