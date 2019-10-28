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
	"os"
	"path"
	"time"

	"istio.io/pkg/env"

	"google.golang.org/grpc"
	"k8s.io/client-go/kubernetes"

	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/security/pkg/cmd"
	"istio.io/istio/security/pkg/pki/ca"
	probecontroller "istio.io/istio/security/pkg/probe"
	"istio.io/istio/security/pkg/registry"
	caserver "istio.io/istio/security/pkg/server/ca"
	"istio.io/pkg/log"
)

// Based on istio_ca main - removing creation of Secrets with private keys in all namespaces and install complexity.
//
// For backward compat, will preserve support for the "cacerts" Secret used for self-signed certificates.
// It is mounted in the same location, and if found will be used - creating the secret is sufficient, no need for
// extra options.
//
// In old installer, the localCertDir is hardcoded to /etc/cacerts and mounted from "cacerts" secret.
//
// Support for signing other root CA has been removed - too dangerous, no clear use case.
//
// Default config, for backward compat with Citadel:
// - if "cacerts" secret exists in istio-system, will be mounted. It may contain an optional "root-cert.pem",
// with additional roots and optional {ca-key, ca-cert, cert-chain}.pem user-provided root CA.
// - if user-provided root CA is not found, the Secret "istio-ca-secret" is used, with ca-cert.pem and ca-key.pem files.
// - if neither is found, istio-ca-secret will be created.
// - a config map "istio-security" with a "caTLSRootCert" file will be used for root cert, created if needed.

var (
	// This replaces the "cert-chain", "signing-cert" and "signing-key" flags in citadel - Istio installer is
	// requires a secret named "cacerts" with specific files inside.
	localCertDir = env.RegisterStringVar("ROOT_CA_DIR", "./etc/cacerts",
		"Location of a local or mounted CA root")

	workloadCertTTL = env.RegisterDurationVar("MAX_WORKLOAD_CERT_TTL",
		cmd.DefaultWorkloadCertTTL,
		"The TTL of issued workload certificates.")

	maxWorkloadCertTtl = env.RegisterDurationVar("MAX_WORKLOAD_CERT_TTL",
		cmd.DefaultMaxWorkloadCertTTL,
		"The max TTL of issued workload certificates.")

	selfSignedCACertTTL = env.RegisterDurationVar("CITADEL_SELF_SIGNED_CA_CERT_TTL",
		cmd.DefaultSelfSignedCACertTTL,
		"The TTL of self-signed CA root certificate.")

	selfSignedRootCertCheckInterval = env.RegisterDurationVar("CITADEL_SELF_SIGNED_ROOT_CERT_CHECK_INTERVAL",
		cmd.DefaultSelfSignedRootCertCheckInterval,
		"The interval that self-signed CA checks its root certificate "+
			"expiration time and rotates root certificate. Setting this interval "+
			"to zero or a negative value disables automated root cert check and "+
			"rotation. This interval is suggested to be larger than 10 minutes.")

	selfSignedRootCertGracePeriodPercentile = env.RegisterIntVar("CITADEL_SELF_SIGNED_ROOT_CERT_GRACE_PERIOD_PERCENTILE",
		cmd.DefaultRootCertGracePeriodPercentile,
		"Grace period percentile for self-signed root cert.")

	workloadCertMinGracePeriod = env.RegisterDurationVar("CITADEL_WORKLOAD_CERT_MIN_GRACE_PERIOD",
		cmd.DefaultWorkloadMinCertGracePeriod,
		"The minimum workload certificate rotation grace period.")

	enableJitterForRootCertRotator = env.RegisterBoolVar("CITADEL_ENABLE_JITTER_FOR_ROOT_CERT_ROTATOR",
		true,
		"If true, set up a jitter to start root cert rotator. "+
			"Jitter selects a backoff time in seconds to start root cert rotator, "+
			"and the back off time is below root cert check interval.")
)

type CAOptions struct {
	// domain to use in SPIFFE identity URLs
	TrustDomain string
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
	// 'hostlist' must be non-empty - but is not used since a grpc server is passed.
	caServer, startErr := caserver.NewWithGRPC(grpc, ca, maxWorkloadCertTtl.Get(),
		false, []string{"istiod.istio-system"}, 0, spiffe.GetTrustDomain(),
		true)
	if startErr != nil {
		log.Fatalf("Failed to create istio ca server: %v", startErr)
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

	signingKeyFile := path.Join(localCertDir.Get(), "ca-key.pem")
	rootCertFile := path.Join(localCertDir.Get(), "root-cert.pem")
	if _, err := os.Stat(rootCertFile); err != nil {
		// In Citadel, normal self-signed doesn't use a root-cert.pem file for additional roots.
		// In Istiod, it is possible to provide one via "cacerts" secret in both cases, for consistency.
		rootCertFile = ""
	}

	if _, err := os.Stat(signingKeyFile); err != nil {
		// The user-provided certs are missing - create a self-signed cert.

		log.Info("Use self-signed certificate as the CA certificate")
		spiffe.SetTrustDomain(spiffe.DetermineTrustDomain(opts.TrustDomain, true))
		// Abort after 20 minutes.
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute*20)
		defer cancel()
		// rootCertFile will be added to "ca-cert.pem".

		// readSigningCertOnly set to false - it doesn't seem to be used in Citadel, nor do we have a way
		// to set it only for one job.
		caOpts, err = ca.NewSelfSignedIstioCAOptions(ctx, false,
			selfSignedRootCertGracePeriodPercentile.Get(), selfSignedCACertTTL.Get(),
			selfSignedRootCertCheckInterval.Get(), workloadCertTTL.Get(),
			maxWorkloadCertTtl.Get(), spiffe.GetTrustDomain(), true,
			IstiodNamespace.Get(), -1, client, rootCertFile,
			enableJitterForRootCertRotator.Get())
		if err != nil {
			log.Fatalf("Failed to create a self-signed Citadel (error: %v)", err)
		}
	} else {
		log.Info("Use local CA certificate")

		// The cert corresponding to the key, self-signed or chain.
		// rootCertFile will be added at the end, if present, to form 'rootCerts'.
		signingCertFile := path.Join(localCertDir.Get(), "ca-cert.pem")
		//
		certChainFile := path.Join(localCertDir.Get(), "cert-chain.pem")

		caOpts, err = ca.NewPluggedCertIstioCAOptions(certChainFile, signingCertFile, signingKeyFile,
			rootCertFile, workloadCertTTL.Get(), maxWorkloadCertTtl.Get(), IstiodNamespace.Get(), client)
		if err != nil {
			log.Fatalf("Failed to create an Citadel (error: %v)", err)
		}
	}

	istioCA, err := ca.NewIstioCA(caOpts)
	if err != nil {
		log.Errorf("Failed to create an Citadel (error: %v)", err)
	}

	// TODO: provide an endpoint returning all the roots. SDS can only pull a single root in current impl.
	// ca.go saves or uses the secret, but also writes to the configmap "istio-security", under caTLSRootCert

	// rootCertRotatorChan channel accepts signals to stop root cert rotator for
	// self-signed CA.
	rootCertRotatorChan := make(chan struct{})
	// Start root cert rotator in a separate goroutine.
	istioCA.Run(rootCertRotatorChan)

	return istioCA
}
