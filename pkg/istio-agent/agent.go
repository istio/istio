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

package istioagent

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"

	mesh "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/dns"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/nodeagent/cache"
	"istio.io/istio/security/pkg/nodeagent/caclient"
	citadel "istio.io/istio/security/pkg/nodeagent/caclient/providers/citadel"
	gca "istio.io/istio/security/pkg/nodeagent/caclient/providers/google"
	"istio.io/istio/security/pkg/nodeagent/sds"
	"istio.io/pkg/log"
)

// To debug:
// curl -X POST localhost:15000/logging?config=trace - to see SendingDiscoveryRequest

// Breakpoints in secretcache.go GenerateSecret..

// Note that istiod currently can't validate the JWT token unless it runs on k8s
// Main problem is the JWT validation check which hardcodes the k8s server address and token location.
//
// To test on a local machine, for debugging:
//
// kis exec $POD -- cat /run/secrets/istio-token/istio-token > var/run/secrets/tokens/istio-token
// kis port-forward $POD 15010:15010 &
//
// You can also copy the K8S CA and a token to be used to connect to k8s - but will need removing the hardcoded addr
// kis exec $POD -- cat /run/secrets/kubernetes.io/serviceaccount/{ca.crt,token} > var/run/secrets/kubernetes.io/serviceaccount/
//
// Or disable the jwt validation while debugging SDS problems.

const (
	// Location of K8S CA root.
	k8sCAPath = "./var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

	// CitadelCACertPath is the directory for Citadel CA certificate.
	// This is mounted from config map 'istio-ca-root-cert'. Part of startup,
	// this may be replaced with ./etc/certs, if a root-cert.pem is found, to
	// handle secrets mounted from non-citadel CAs.
	CitadelCACertPath = "./var/run/secrets/istio"
)

const (
	MetadataClientCertKey   = "ISTIO_META_TLS_CLIENT_KEY"
	MetadataClientCertChain = "ISTIO_META_TLS_CLIENT_CERT_CHAIN"
	MetadataClientRootCert  = "ISTIO_META_TLS_CLIENT_ROOT_CERT"
)

// Agent contains the configuration of the agent, based on the injected
// environment:
// - SDS hostPath if node-agent was used
// - /etc/certs/key if Citadel or other mounted Secrets are used
// - root cert to use for connecting to XDS server
// - CA address, with proper defaults and detection
type Agent struct {
	proxyConfig *mesh.ProxyConfig

	cfg     *AgentOptions
	secOpts *security.Options

	sdsServer   *sds.Server
	secretCache *cache.SecretManagerClient

	// Used when proxying envoy xds via istio-agent is enabled.
	xdsProxy *XdsProxy

	// local DNS Server that processes DNS requests locally and forwards to upstream DNS if needed.
	localDNSServer *dns.LocalDNSServer
}

// AgentOptions contains additional config for the agent, not included in ProxyConfig.
// Most are from env variables ( still experimental ) or for testing only.
// Eventually most non-test settings should graduate to ProxyConfig
// Please don't add 100 parameters to the NewAgent function (or any other)!
type AgentOptions struct {
	// ProxyXDSViaAgent if true will enable a local XDS proxy that will simply
	// ferry Envoy's XDS requests to istiod and responses back to envoy
	// This flag is temporary until the feature is stabilized.
	ProxyXDSViaAgent bool
	// DNSCapture indicates if the XDS proxy has dns capture enabled or not
	// This option will not be considered if proxyXDSViaAgent is false.
	DNSCapture bool
	// ProxyType is the type of proxy we are configured to handle
	ProxyType model.NodeType
	// ProxyNamespace to use for local dns resolution
	ProxyNamespace string
	// ProxyDomain is the DNS domain associated with the proxy (assumed
	// to include the namespace as well) (for local dns resolution)
	ProxyDomain string

	// XDSRootCerts is the location of the root CA for the XDS connection. Used for setting platform certs or
	// using custom roots.
	XDSRootCerts string

	// CARootCerts of the location of the root CA for the CA connection. Used for setting platform certs or
	// using custom roots.
	CARootCerts string

	// Extra headers to add to the XDS connection.
	XDSHeaders map[string]string

	// Is the proxy an IPv6 proxy
	IsIPv6 bool

	// Path to local UDS to communicate with Envoy
	XdsUdsPath string
}

// NewAgent hosts the functionality for local SDS and XDS. This consists of the local SDS server and
// associated clients to sign certificates (when not using files), and the local XDS proxy (including
// health checking for VMs and DNS proxying).
func NewAgent(proxyConfig *mesh.ProxyConfig, agentOpts *AgentOptions, sopts *security.Options) *Agent {
	return &Agent{
		proxyConfig: proxyConfig,
		cfg:         agentOpts,
		secOpts:     sopts,
	}
}

// Simplified SDS setup. This is called if and only if user has explicitly mounted a K8S JWT token, and is not
// using a hostPath mounted or external SDS server.
//
// 1. External CA: requires authenticating the trusted JWT AND validating the SAN against the JWT.
//    For example Google CA
//
// 2. Indirect, using istiod: using K8S cert.
func (a *Agent) Start() error {
	var err error
	a.secretCache, err = a.newSecretManager()
	if err != nil {
		return err
	}

	a.sdsServer, err = sds.NewServer(a.secOpts, a.secretCache)
	if err != nil {
		return err
	}
	a.secretCache.SetUpdateCallback(a.sdsServer.UpdateCallback)

	if err = a.initLocalDNSServer(a.cfg.ProxyType == model.SidecarProxy); err != nil {
		return fmt.Errorf("failed to start local DNS server: %v", err)
	}

	if a.cfg.ProxyXDSViaAgent {
		a.xdsProxy, err = initXdsProxy(a)
		if err != nil {
			return fmt.Errorf("failed to start xds proxy: %v", err)
		}
	}
	return nil
}

func (a *Agent) initLocalDNSServer(isSidecar bool) (err error) {
	// we dont need dns server on gateways
	if a.cfg.DNSCapture && a.cfg.ProxyXDSViaAgent && isSidecar {
		if a.localDNSServer, err = dns.NewLocalDNSServer(a.cfg.ProxyNamespace, a.cfg.ProxyDomain); err != nil {
			return err
		}
		a.localDNSServer.StartDNS()
	}
	return nil
}

func (a *Agent) Close() {
	if a.xdsProxy != nil {
		a.xdsProxy.close()
	}
	if a.localDNSServer != nil {
		a.localDNSServer.Close()
	}
	if a.sdsServer != nil {
		a.sdsServer.Stop()
	}
	if a.secretCache != nil {
		a.secretCache.Close()
	}
}

// explicit code to determine the root CA to be configured in bootstrap file.
// It may be different from the CA for the cert server - which is based on CA_ADDR
// Replaces logic in the template:
//                 {{- if .provisioned_cert }}
//                  "filename": "{{(printf "%s%s" .provisioned_cert "/root-cert.pem") }}"
//                  {{- else if eq .pilot_cert_provider "kubernetes" }}
//                  "filename": "./var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
//                  {{- else if eq .pilot_cert_provider "istiod" }}
//                  "filename": "./var/run/secrets/istio/root-cert.pem"
//                  {{- end }}
//
// In addition it deals with the case the XDS server is on port 443, expected with a proper cert.
// /etc/ssl/certs/ca-certificates.crt
//
// TODO: additional checks for existence. Fail early, instead of obscure envoy errors.
func (a *Agent) FindRootCAForXDS() string {
	if a.cfg.XDSRootCerts == security.SystemRootCerts {
		return ""
	} else if a.cfg.XDSRootCerts != "" {
		return a.cfg.XDSRootCerts
	} else if _, err := os.Stat("./etc/certs/root-cert.pem"); err == nil {
		// Old style - mounted cert. This is used for XDS auth only,
		// not connecting to CA_ADDR because this mode uses external
		// agent (Secret refresh, etc)
		return "./etc/certs/root-cert.pem"
	} else if a.secOpts.PilotCertProvider == "kubernetes" {
		// Using K8S - this is likely incorrect, may work by accident (https://github.com/istio/istio/issues/22161)
		return k8sCAPath
	} else if a.secOpts.ProvCert != "" {
		// This was never completely correct - PROV_CERT are only intended for auth with CA_ADDR,
		// and should not be involved in determining the root CA.
		return a.secOpts.ProvCert + "/root-cert.pem"
	} else if a.secOpts.FileMountedCerts {
		// FileMountedCerts - Load it from Proxy Metadata.
		return a.proxyConfig.ProxyMetadata[MetadataClientRootCert]
	} else {
		// PILOT_CERT_PROVIDER - default is istiod
		// This is the default - a mounted config map on K8S
		return path.Join(CitadelCACertPath, constants.CACertNamespaceConfigMapDataName)
	}
}

// Find the root CA to use when connecting to the CA (Istiod or external).
func (a *Agent) FindRootCAForCA() string {
	if a.cfg.CARootCerts == security.SystemRootCerts {
		return ""
	} else if a.cfg.CARootCerts != "" {
		return a.cfg.CARootCerts
	} else if a.secOpts.PilotCertProvider == "kubernetes" {
		// Using K8S - this is likely incorrect, may work by accident.
		// API is alpha.
		return k8sCAPath // ./var/run/secrets/kubernetes.io/serviceaccount/ca.crt
	} else if a.secOpts.PilotCertProvider == "custom" {
		return security.DefaultRootCertFilePath // ./etc/certs/root-cert.pem
	} else if a.secOpts.ProvCert != "" {
		// This was never completely correct - PROV_CERT are only intended for auth with CA_ADDR,
		// and should not be involved in determining the root CA.
		return a.secOpts.ProvCert + "/root-cert.pem"
	} else {
		// This is the default - a mounted config map on K8S
		return path.Join(CitadelCACertPath, constants.CACertNamespaceConfigMapDataName)
		// or: "./var/run/secrets/istio/root-cert.pem"
	}
}

// newSecretManager creates the SecretManager for workload secrets
func (a *Agent) newSecretManager() (*cache.SecretManagerClient, error) {
	// If proxy is using file mounted certs, we do not have to connect to CA.
	if a.secOpts.FileMountedCerts {
		log.Info("Workload is using file mounted certificates. Skipping connecting to CA")
		return cache.NewSecretManagerClient(nil, a.secOpts)
	}

	// TODO: this should all be packaged in a plugin, possibly with optional compilation.
	log.Infof("CA Endpoint %s, provider %s", a.secOpts.CAEndpoint, a.secOpts.CAProviderName)
	if a.secOpts.CAProviderName == "GoogleCA" || strings.Contains(a.secOpts.CAEndpoint, "googleapis.com") {
		// Use a plugin to an external CA - this has direct support for the K8S JWT token
		// This is only used if the proper env variables are injected - otherwise the existing Citadel or Istiod will be
		// used.
		caClient, err := gca.NewGoogleCAClient(a.secOpts.CAEndpoint, true, caclient.NewCATokenProvider(a.secOpts))
		if err != nil {
			return nil, err
		}
		return cache.NewSecretManagerClient(caClient, a.secOpts)
	}

	// Using citadel CA
	var rootCert []byte
	var err error
	// Special case: if Istiod runs on a secure network, on the default port, don't use TLS
	// TODO: may add extra cases or explicit settings - but this is a rare use cases, mostly debugging
	tls := true
	if strings.HasSuffix(a.secOpts.CAEndpoint, ":15010") {
		tls = false
		log.Warn("Debug mode or IP-secure network")
	}
	if tls {
		caCertFile := a.FindRootCAForCA()
		if caCertFile == "" {
			log.Infof("Using CA %s cert with system certs", a.secOpts.CAEndpoint)
		} else if rootCert, err = ioutil.ReadFile(caCertFile); err != nil {
			log.Fatalf("invalid config - %s missing a root certificate %s", a.secOpts.CAEndpoint, caCertFile)
		} else {
			log.Infof("Using CA %s cert with certs: %s", a.secOpts.CAEndpoint, caCertFile)
		}
	}

	// Will use TLS unless the reserved 15010 port is used ( istiod on an ipsec/secure VPC)
	// rootCert may be nil - in which case the system roots are used, and the CA is expected to have public key
	// Otherwise assume the injection has mounted /etc/certs/root-cert.pem
	caClient, err := citadel.NewCitadelClient(a.secOpts, tls, rootCert)
	if err != nil {
		return nil, err
	}

	return cache.NewSecretManagerClient(caClient, a.secOpts)
}
