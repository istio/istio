// Copyright 2019 Istio Authors
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

package istiod

import (
	"context"
	"fmt"
	"google.golang.org/grpc"
	"k8s.io/client-go/kubernetes"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/security/pkg/caclient"
	"istio.io/istio/security/pkg/cmd"
	"istio.io/istio/security/pkg/pki/ca"
	probecontroller "istio.io/istio/security/pkg/probe"
	"istio.io/istio/security/pkg/registry"
	caserver "istio.io/istio/security/pkg/server/ca"
	"istio.io/pkg/log"
)

// Based on istio_ca main - removing the Secret creation controller and other options

const (
	selfSignedCaCertTTL                     = "CITADEL_SELF_SIGNED_CA_CERT_TTL"
	selfSignedRootCertCheckInterval         = "CITADEL_SELF_SIGNED_ROOT_CERT_CHECK_INTERVAL"
	selfSignedRootCertGracePeriodPercentile = "CITADEL_SELF_SIGNED_ROOT_CERT_GRACE_PERIOD_PERCENTILE"
	workloadCertMinGracePeriod              = "CITADEL_WORKLOAD_CERT_MIN_GRACE_PERIOD"
	enableJitterForRootCertRotator          = "CITADEL_ENABLE_JITTER_FOR_ROOT_CERT_ROTATOR"
)

type CAOptions struct { // nolint: maligned
	IstioNamespace      string
	ReadSigningCertOnly bool

	certChainFile   string
	signingCertFile string
	signingKeyFile  string
	rootCertFile    string

	SelfSignedCA                            bool
	selfSignedCACertTTL                     time.Duration
	selfSignedRootCertCheckInterval         time.Duration
	selfSignedRootCertGracePeriodPercentile int
	enableJitterForRootCertRotator          bool

	WorkloadCertTTL    time.Duration
	MaxWorkloadCertTTL time.Duration
	// The length of certificate rotation grace period, configured as the ratio of the certificate TTL.
	// If workloadCertGracePeriodRatio is 0.2, and cert TTL is 24 hours, then the rotation will happen
	// after 24*(1-0.2) hours since the cert is issued.
	workloadCertGracePeriodRatio float32
	// The minimum grace period for workload cert rotation.
	workloadCertMinGracePeriod time.Duration

	// Comma separated string containing all possible host name that clients may use to connect to.
	grpcHosts  string

	// Whether the CA signs certificates for other CAs.
	signCACerts bool
	// Whether to generate PKCS#8 private keys.
	pkcs8Keys bool

	cAClientConfig caclient.Config

	// domain to use in SPIFFE identity URLs
	TrustDomain string

	// Enable dual-use certs - SPIFFE in SAN and in CommonName
	DualUse bool
}

func fatalf(template string, args ...interface{}) {
	if len(args) > 0 {
		log.Errorf(template, args...)
	} else {
		log.Errorf(template)
	}
	os.Exit(-1)
}

// fqdn returns the k8s cluster dns name for the Citadel service.
func fqdn(opts *CAOptions) string {
	return fmt.Sprintf("istio-citadel.%v.svc.cluster.local", opts.IstioNamespace)
}

func RunCA(grpc *grpc.Server, cs kubernetes.Interface, opts *CAOptions) {

	ca := createCA(cs.CoreV1(), opts)

		// start registry if gRPC server is to be started
		reg := registry.GetIdentityRegistry()

		// add certificate identity to the identity registry for the liveness probe check
		if registryErr := reg.AddMapping(probecontroller.LivenessProbeClientIdentity,
			probecontroller.LivenessProbeClientIdentity); registryErr != nil {
			log.Errorf("Failed to add indentity mapping: %v", registryErr)
		}

		ch := make(chan struct{})

		// The CA API uses cert with the max workload cert TTL.
		hostnames := append(strings.Split(opts.grpcHosts, ","), fqdn(opts))
		caServer, startErr := caserver.NewWithGRPC(grpc, ca, opts.MaxWorkloadCertTTL,
			opts.signCACerts, hostnames, 0, spiffe.GetTrustDomain(),
			true)
		if startErr != nil {
			fatalf("Failed to create istio ca server: %v", startErr)
		}
		if serverErr := caServer.Run(); serverErr != nil {
			// stop the registry-related controllers
			ch <- struct{}{}

			log.Warnf("Failed to start GRPC server with error: %v", serverErr)
		}
	log.Info("Istiod CA has started")
}

func createCA(client corev1.CoreV1Interface, opts *CAOptions) *ca.IstioCA {
	var caOpts *ca.IstioCAOptions
	var err error

	if opts.SelfSignedCA {
		log.Info("Use self-signed certificate as the CA certificate")
		spiffe.SetTrustDomain(spiffe.DetermineTrustDomain(opts.TrustDomain, true))
		// Abort after 20 minutes.
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute*20)
		defer cancel()
		var checkInterval time.Duration
		if opts.ReadSigningCertOnly {
			checkInterval = cmd.ReadSigningCertRetryInterval
		} else {
			checkInterval = -1
		}
		caOpts, err = ca.NewSelfSignedIstioCAOptions(ctx, opts.ReadSigningCertOnly,
			opts.selfSignedRootCertGracePeriodPercentile, opts.selfSignedCACertTTL,
			opts.selfSignedRootCertCheckInterval, opts.WorkloadCertTTL,
			opts.MaxWorkloadCertTTL, spiffe.GetTrustDomain(), opts.DualUse,
			opts.IstioNamespace, checkInterval, client, opts.rootCertFile,
			opts.enableJitterForRootCertRotator)
		if err != nil {
			fatalf("Failed to create a self-signed Citadel (error: %v)", err)
		}
	} else {
		log.Info("Use certificate from argument as the CA certificate")
		caOpts, err = ca.NewPluggedCertIstioCAOptions(opts.certChainFile, opts.signingCertFile, opts.signingKeyFile,
			opts.rootCertFile, opts.WorkloadCertTTL, opts.MaxWorkloadCertTTL, opts.IstioNamespace, client)
		if err != nil {
			fatalf("Failed to create an Citadel (error: %v)", err)
		}
	}

	istioCA, err := ca.NewIstioCA(caOpts)
	if err != nil {
		log.Errorf("Failed to create an Citadel (error: %v)", err)
	}

	// rootCertRotatorChan channel accepts signals to stop root cert rotator for
	// self-signed CA.
	rootCertRotatorChan := make(chan struct{})
	// Start root cert rotator in a separate goroutine.
	istioCA.Run(rootCertRotatorChan)

	return istioCA
}

