// Copyright Istio Authors
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

package bootstrap

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"

	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	securityModel "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/jwt"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/cmd"
	"istio.io/istio/security/pkg/pki/ca"
	"istio.io/istio/security/pkg/pki/ra"
	caserver "istio.io/istio/security/pkg/server/ca"
	"istio.io/istio/security/pkg/server/ca/authenticate"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

type caOptions struct {
	// Either extCAK8s or extCAGrpc
	ExternalCAType   ra.CaExternalType
	ExternalCASigner string
	// domain to use in SPIFFE identity URLs
	TrustDomain    string
	Namespace      string
	Authenticators []security.Authenticator
}

// Based on istio_ca main - removing creation of Secrets with private keys in all namespaces and install complexity.
//
// For backward compat, will preserve support for the "cacerts" Secret used for self-signed certificates.
// It is mounted in the same location, and if found will be used - creating the secret is sufficient, no need for
// extra options.
//
// In old installer, the LocalCertDir is hardcoded to /etc/cacerts and mounted from "cacerts" secret.
//
// Support for signing other root CA has been removed - too dangerous, no clear use case.
//
// Default config, for backward compat with Citadel:
// - if "cacerts" secret exists in istio-system, will be mounted. It may contain an optional "root-cert.pem",
// with additional roots and optional {ca-key, ca-cert, cert-chain}.pem user-provided root CA.
// - if user-provided root CA is not found, the Secret "istio-ca-secret" is used, with ca-cert.pem and ca-key.pem files.
// - if neither is found, istio-ca-secret will be created.
//
// - a config map "istio-security" with a "caTLSRootCert" file will be used for root cert, and created if needed.
//   The config map was used by node agent - no longer possible to use in sds-agent, but we still save it for
//   backward compat. Will be removed with the node-agent. sds-agent is calling NewCitadelClient directly, using
//   K8S root.

var (
	// LocalCertDir replaces the "cert-chain", "signing-cert" and "signing-key" flags in citadel - Istio installer is
	// requires a secret named "cacerts" with specific files inside.
	LocalCertDir = env.RegisterStringVar("ROOT_CA_DIR", "./etc/cacerts",
		"Location of a local or mounted CA root")

	useRemoteCerts = env.RegisterBoolVar("USE_REMOTE_CERTS", false,
		"Whether to try to load CA certs from a remote Kubernetes cluster. Used for external Istiod.")

	workloadCertTTL = env.RegisterDurationVar("DEFAULT_WORKLOAD_CERT_TTL",
		cmd.DefaultWorkloadCertTTL,
		"The default TTL of issued workload certificates. Applied when the client sets a "+
			"non-positive TTL in the CSR.")

	maxWorkloadCertTTL = env.RegisterDurationVar("MAX_WORKLOAD_CERT_TTL",
		cmd.DefaultMaxWorkloadCertTTL,
		"The max TTL of issued workload certificates.")

	SelfSignedCACertTTL = env.RegisterDurationVar("CITADEL_SELF_SIGNED_CA_CERT_TTL",
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

	enableJitterForRootCertRotator = env.RegisterBoolVar("CITADEL_ENABLE_JITTER_FOR_ROOT_CERT_ROTATOR",
		true,
		"If true, set up a jitter to start root cert rotator. "+
			"Jitter selects a backoff time in seconds to start root cert rotator, "+
			"and the back off time is below root cert check interval.")

	k8sInCluster = env.RegisterStringVar("KUBERNETES_SERVICE_HOST", "",
		"Kuberenetes service host, set automatically when running in-cluster")

	// This value can also be extracted from the mounted token
	trustedIssuer = env.RegisterStringVar("TOKEN_ISSUER", "",
		"OIDC token issuer. If set, will be used to check the tokens.")

	audience = env.RegisterStringVar("AUDIENCE", "",
		"Expected audience in the tokens. ")

	caRSAKeySize = env.RegisterIntVar("CITADEL_SELF_SIGNED_CA_RSA_KEY_SIZE", 2048,
		"Specify the RSA key size to use for self-signed Istio CA certificates.")

	// TODO: Likely to be removed and added to mesh config
	externalCaType = env.RegisterStringVar("EXTERNAL_CA", "",
		"External CA Integration Type. Permitted Values are ISTIOD_RA_KUBERNETES_API or "+
			"ISTIOD_RA_ISTIO_API").Get()

	// TODO: Likely to be removed and added to mesh config
	k8sSigner = env.RegisterStringVar("K8S_SIGNER", "",
		"Kubernates CA Signer type. Valid from Kubernates 1.18").Get()
)

// EnableCA returns whether CA functionality is enabled in istiod.
// The logic of this function is from the logic of whether running CA
// in RunCA(). The reason for moving this logic from RunCA into EnableCA() is
// to have a central consistent endpoint to get whether CA functionality is
// enabled in istiod. EnableCA() is called in multiple places.
func (s *Server) EnableCA() bool {
	if !features.EnableCAServer {
		return false
	}
	// Log if we're using self-signed certs without K8S, in debug mode
	if s.kubeClient == nil {
		// Without K8S the user needs to have the private key in a local file.
		// If that is missing - we'll generate an in-memory root for testing, and warn.
		signingKeyFile := path.Join(LocalCertDir.Get(), "ca-key.pem")
		if _, err := os.Stat(signingKeyFile); err != nil {
			log.Warnf("Will use in-memory root CA, no K8S access and no ca key file %s", signingKeyFile)
		}
	}

	return true
}

// RunCA will start the cert signing GRPC service on an existing server.
// Protected by installer options: the CA will be started only if the JWT token in /var/run/secrets
// is mounted. If it is missing - for example old versions of K8S that don't support such tokens -
// we will not start the cert-signing server, since pods will have no way to authenticate.
func (s *Server) RunCA(grpc *grpc.Server, ca caserver.CertificateAuthority, opts *caOptions) {
	if !s.EnableCA() {
		return
	}
	if ca == nil {
		// When the CA to run is nil, return
		log.Warn("the CA to run is nil")
		return
	}
	iss := trustedIssuer.Get()
	aud := audience.Get()

	token, err := ioutil.ReadFile(getJwtPath())
	if err == nil {
		tok, err := detectAuthEnv(string(token))
		if err != nil {
			log.Warn("Starting with invalid K8S JWT token", err, string(token))
		} else {
			if iss == "" {
				iss = tok.Iss
			}
			if len(tok.Aud) > 0 && len(aud) == 0 {
				aud = tok.Aud[0]
			}
		}
	}

	// The CA API uses cert with the max workload cert TTL.
	// 'hostlist' must be non-empty - but is not used since a grpc server is passed.
	// Adds client cert auth and kube (sds enabled)
	caServer, startErr := caserver.New(ca, maxWorkloadCertTTL.Get(), opts.Authenticators)
	if startErr != nil {
		log.Fatalf("failed to create istio ca server: %v", startErr)
	}

	// TODO: if not set, parse Istiod's own token (if present) and get the issuer. The same issuer is used
	// for all tokens - no need to configure twice. The token may also include cluster info to auto-configure
	// networking properties.
	if iss != "" && // issuer set explicitly or extracted from our own JWT
		k8sInCluster.Get() == "" { // not running in cluster - in cluster use direct call to apiserver
		// Add a custom authenticator using standard JWT validation, if not running in K8S
		// When running inside K8S - we can use the built-in validator, which also check pod removal (invalidation).
		jwtRule := v1beta1.JWTRule{Issuer: iss, Audiences: []string{aud}}
		oidcAuth, err := authenticate.NewJwtAuthenticator(&jwtRule, opts.TrustDomain)
		if err == nil {
			caServer.Authenticators = append(caServer.Authenticators, oidcAuth)
			log.Info("Using out-of-cluster JWT authentication")
		} else {
			log.Info("K8S token doesn't support OIDC, using only in-cluster auth")
		}
	}

	caServer.Register(grpc)

	log.Info("Istiod CA has started")
}

// detectAuthEnv will use the JWT token that is mounted in istiod to set the default audience
// and trust domain for Istiod, if not explicitly defined.
// K8S will use the same kind of tokens for the pods, and the value in istiod's own token is
// simplest and safest way to have things match.
//
// Note that K8S is not required to use JWT tokens - we will fallback to the defaults
// or require explicit user option for K8S clusters using opaque tokens.
func detectAuthEnv(jwt string) (*authenticate.JwtPayload, error) {
	jwtSplit := strings.Split(jwt, ".")
	if len(jwtSplit) != 3 {
		return nil, fmt.Errorf("invalid JWT parts: %s", jwt)
	}
	payload := jwtSplit[1]

	payloadBytes, err := base64.RawStdEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to decode jwt: %v", err.Error())
	}

	structuredPayload := &authenticate.JwtPayload{}
	err = json.Unmarshal(payloadBytes, &structuredPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal jwt: %v", err.Error())
	}

	return structuredPayload, nil
}

// Save the root public key file and initialize the path the the file, to be used by other
// components.
func (s *Server) initPublicKey() error {
	// Setup the root cert chain and caBundlePath - before calling initDNSListener.
	if features.PilotCertProvider.Get() == KubernetesCAProvider {
		s.caBundlePath = defaultCACertPath
	} else if features.PilotCertProvider.Get() == IstiodCAProvider {
		signingKeyFile := path.Join(LocalCertDir.Get(), "ca-key.pem")
		if _, err := os.Stat(signingKeyFile); err != nil {
			// When Citadel is configured to use self-signed certs, keep a local copy so other
			// components can load it via file (e.g. webhook config controller).
			if err := os.MkdirAll(dnsCertDir, 0700); err != nil {
				return err
			}
			// We have direct access to the self-signed
			internalSelfSignedRootPath := path.Join(dnsCertDir, "self-signed-root.pem")

			rootCert := s.CA.GetCAKeyCertBundle().GetRootCertPem()
			if err = ioutil.WriteFile(internalSelfSignedRootPath, rootCert, 0600); err != nil {
				return err
			}

			s.caBundlePath = internalSelfSignedRootPath
			s.addStartFunc(func(stop <-chan struct{}) error {
				go func() {
					for {
						select {
						case <-stop:
							return
						case <-time.After(controller.NamespaceResyncPeriod):
							newRootCert := s.CA.GetCAKeyCertBundle().GetRootCertPem()
							if !bytes.Equal(rootCert, newRootCert) {
								rootCert = newRootCert
								if err = ioutil.WriteFile(internalSelfSignedRootPath, rootCert, 0600); err != nil {
									log.Errorf("Failed to update local copy of self-signed root: %v", err)
								} else {
									log.Info("Updtaed local copy of self-signed root")
								}
							}
						}
					}
				}()
				return nil
			})

		} else {
			s.caBundlePath = path.Join(LocalCertDir.Get(), "cert-chain.pem")
		}
	} else {
		s.caBundlePath = path.Join(features.PilotCertProvider.Get(), "cert-chain.pem")
	}
	return nil
}

// loadRemoteCACerts mounts an existing cacerts Secret if the files aren't mounted locally.
// By default, a cacerts Secret would be mounted during pod startup due to the
// Istiod Deployment configuration. But with external Istiod, we want to be
// able to load cacerts from a remote cluster instead.
func (s *Server) loadRemoteCACerts(caOpts *caOptions, dir string) error {
	if s.kubeClient == nil {
		return nil
	}

	signingKeyFile := path.Join(dir, "ca-key.pem")
	if _, err := os.Stat(signingKeyFile); !os.IsNotExist(err) {
		return fmt.Errorf("signing key file %s already exists", signingKeyFile)
	}

	secret, err := s.kubeClient.Kube().CoreV1().Secrets(caOpts.Namespace).Get(
		context.TODO(), "cacerts", metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	log.Infof("cacerts Secret found in remote cluster, saving contents to %s", dir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	for key, data := range secret.Data {
		filename := path.Join(dir, key)
		if err := ioutil.WriteFile(filename, data, 0600); err != nil {
			return err
		}
	}
	return nil
}

// createIstioCA initializes the Istio CA signing functionality.
// - for 'plugged in', uses ./etc/cacert directory, mounted from 'cacerts' secret in k8s.
//   Inside, the key/cert are 'ca-key.pem' and 'ca-cert.pem'. The root cert signing the intermeidate is root-cert.pem,
//   which may contain multiple roots. A 'cert-chain.pem' file has the full cert chain.
func (s *Server) createIstioCA(client corev1.CoreV1Interface, opts *caOptions) (*ca.IstioCA, error) {
	var caOpts *ca.IstioCAOptions
	var err error

	// In pods, this is the optional 'cacerts' Secret.
	// TODO: also check for key.pem ( for interop )
	signingKeyFile := path.Join(LocalCertDir.Get(), "ca-key.pem")

	// If not found, will default to ca-cert.pem. May contain multiple roots.
	rootCertFile := path.Join(LocalCertDir.Get(), "root-cert.pem")
	if _, err := os.Stat(rootCertFile); err != nil {
		// In Citadel, normal self-signed doesn't use a root-cert.pem file for additional roots.
		// In Istiod, it is possible to provide one via "cacerts" secret in both cases, for consistency.
		rootCertFile = ""
	}
	if _, err := os.Stat(signingKeyFile); err != nil && client != nil {
		// The user-provided certs are missing - create a self-signed cert.
		log.Info("Use self-signed certificate as the CA certificate")
		// Abort after 20 minutes.
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute*20)
		defer cancel()
		// rootCertFile will be added to "ca-cert.pem".
		// readSigningCertOnly set to false - it doesn't seem to be used in Citadel, nor do we have a way
		// to set it only for one job.
		caOpts, err = ca.NewSelfSignedIstioCAOptions(ctx,
			selfSignedRootCertGracePeriodPercentile.Get(), SelfSignedCACertTTL.Get(),
			selfSignedRootCertCheckInterval.Get(), workloadCertTTL.Get(),
			maxWorkloadCertTTL.Get(), opts.TrustDomain, true,
			opts.Namespace, -1, client, rootCertFile,
			enableJitterForRootCertRotator.Get(), caRSAKeySize.Get())
		if err != nil {
			return nil, fmt.Errorf("failed to create a self-signed istiod CA: %v", err)
		}
	} else {
		if err == nil {
			log.Info("Use local CA certificate")
		} else {
			log.Info("Use local self-signed CA certificate")
		}
		// The cert corresponding to the key, self-signed or chain.
		// rootCertFile will be added at the end, if present, to form 'rootCerts'.
		signingCertFile := path.Join(LocalCertDir.Get(), "ca-cert.pem")
		//
		certChainFile := path.Join(LocalCertDir.Get(), "cert-chain.pem")
		s.caBundlePath = certChainFile
		caOpts, err = ca.NewPluggedCertIstioCAOptions(certChainFile, signingCertFile, signingKeyFile,
			rootCertFile, workloadCertTTL.Get(), maxWorkloadCertTTL.Get(), caRSAKeySize.Get())
		if err != nil {
			return nil, fmt.Errorf("failed to create an istiod CA: %v", err)
		}
	}
	istioCA, err := ca.NewIstioCA(caOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create an istiod CA: %v", err)
	}
	// TODO: provide an endpoint returning all the roots. SDS can only pull a single root in current impl.
	// ca.go saves or uses the secret, but also writes to the configmap "istio-security", under caTLSRootCert
	// rootCertRotatorChan channel accepts signals to stop root cert rotator for
	// self-signed CA.
	rootCertRotatorChan := make(chan struct{})
	// Start root cert rotator in a separate goroutine.
	istioCA.Run(rootCertRotatorChan)
	return istioCA, nil
}

// createIstioRA initializes the Istio RA signing functionality.
// the caOptions defines the external provider
func (s *Server) createIstioRA(client kubelib.Client,
	opts *caOptions) (ra.RegistrationAuthority, error) {
	caCertFile := path.Join(ra.DefaultExtCACertDir, constants.CACertNamespaceConfigMapDataName)
	if _, err := os.Stat(caCertFile); err != nil {
		caCertFile = defaultCACertPath
	}
	raOpts := &ra.IstioRAOptions{
		ExternalCAType: opts.ExternalCAType,
		DefaultCertTTL: workloadCertTTL.Get(),
		MaxCertTTL:     maxWorkloadCertTTL.Get(),
		CaSigner:       opts.ExternalCASigner,
		CaCertFile:     caCertFile,
		VerifyAppendCA: true,
		K8sClient:      client.CertificatesV1beta1(),
		TrustDomain:    opts.TrustDomain,
	}
	return ra.NewIstioRA(raOpts)
}

// getJwtPath returns jwt path.
func getJwtPath() string {
	log.Info("JWT policy is ", features.JwtPolicy.Get())
	switch features.JwtPolicy.Get() {
	case jwt.PolicyThirdParty:
		return securityModel.K8sSATrustworthyJwtFileName
	case jwt.PolicyFirstParty:
		return securityModel.K8sSAJwtFileName
	default:
		log.Infof("unknown JWT policy %v, default to certificates ", features.JwtPolicy.Get())
		return ""
	}
}
