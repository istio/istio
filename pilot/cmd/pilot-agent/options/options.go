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

package options // import "istio.io/istio/pilot/cmd/pilot-agent/options"

import (
	"path/filepath"
	"time"

	"istio.io/istio/pilot/cmd/pilot-agent/status"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/jwt"
	"istio.io/istio/pkg/security"
	"istio.io/pkg/env"
)

var (
	InstanceIPVar        = env.RegisterStringVar("INSTANCE_IP", "", "")
	PodNameVar           = env.RegisterStringVar("POD_NAME", "", "")
	PodNamespaceVar      = env.RegisterStringVar("POD_NAMESPACE", "", "")
	kubeAppProberNameVar = env.RegisterStringVar(status.KubeAppProberEnvName, "", "")
	ProxyConfigEnv       = env.RegisterStringVar(
		"PROXY_CONFIG",
		"",
		"The proxy configuration. This will be set by the injection - gateways will use file mounts.",
	).Get()

	serviceAccountVar = env.RegisterStringVar("SERVICE_ACCOUNT", "", "Name of service account")
	clusterIDVar      = env.RegisterStringVar("ISTIO_META_CLUSTER_ID", "", "")
	// Provider for XDS auth, e.g., gcp. By default, it is empty, meaning no auth provider.
	xdsAuthProvider = env.RegisterStringVar("XDS_AUTH_PROVIDER", "", "Provider for XDS auth")

	jwtPolicy = env.RegisterStringVar("JWT_POLICY", jwt.PolicyThirdParty,
		"The JWT validation policy.")
	// ProvCert is the environment controlling the use of pre-provisioned certs, for VMs.
	// May also be used in K8S to use a Secret to bootstrap (as a 'refresh key'), but use short-lived tokens
	// with extra SAN (labels, etc) in data path.
	provCert = env.RegisterStringVar("PROV_CERT", "",
		"Set to a directory containing provisioned certs, for VMs").Get()

	// set to "SYSTEM" for ACME/public signed XDS servers.
	xdsRootCA = env.RegisterStringVar("XDS_ROOT_CA", "",
		"Explicitly set the root CA to expect for the XDS connection.").Get()

	// set to "SYSTEM" for ACME/public signed CA servers.
	caRootCA = env.RegisterStringVar("CA_ROOT_CA", "",
		"Explicitly set the root CA to expect for the CA connection.").Get()

	outputKeyCertToDir = env.RegisterStringVar("OUTPUT_CERTS", "",
		"The output directory for the key and certificate. If empty, key and certificate will not be saved. "+
			"Must be set for VMs using provisioning certificates.").Get()

	caProviderEnv = env.RegisterStringVar("CA_PROVIDER", "Citadel", "name of authentication provider").Get()
	caEndpointEnv = env.RegisterStringVar("CA_ADDR", "", "Address of the spiffe certificate provider. Defaults to discoveryAddress").Get()

	trustDomainEnv = env.RegisterStringVar("TRUST_DOMAIN", "cluster.local",
		"The trust domain for spiffe certificates").Get()

	secretTTLEnv = env.RegisterDurationVar("SECRET_TTL", 24*time.Hour,
		"The cert lifetime requested by istio agent").Get()

	fileDebounceDuration = env.RegisterDurationVar("FILE_DEBOUNCE_DURATION", 100*time.Millisecond,
		"The duration for which the file read operation is delayed once file update is detected").Get()

	secretRotationGracePeriodRatioEnv = env.RegisterFloatVar("SECRET_GRACE_PERIOD_RATIO", 0.5,
		"The grace period ratio for the cert rotation, by default 0.5.").Get()
	pkcs8KeysEnv = env.RegisterBoolVar("PKCS8_KEY", false,
		"Whether to generate PKCS#8 private keys").Get()
	eccSigAlgEnv        = env.RegisterStringVar("ECC_SIGNATURE_ALGORITHM", "", "The type of ECC signature algorithm to use when generating private keys").Get()
	fileMountedCertsEnv = env.RegisterBoolVar("FILE_MOUNTED_CERTS", false, "").Get()
	credFetcherTypeEnv  = env.RegisterStringVar("CREDENTIAL_FETCHER_TYPE", security.JWT,
		"The type of the credential fetcher. Currently supported types include GoogleComputeEngine").Get()
	credIdentityProvider = env.RegisterStringVar("CREDENTIAL_IDENTITY_PROVIDER", "GoogleComputeEngine",
		"The identity provider for credential. Currently default supported identity provider is GoogleComputeEngine").Get()
	proxyXDSDebugViaAgent = env.RegisterBoolVar("PROXY_XDS_DEBUG_VIA_AGENT", true,
		"If set to true, the agent will listen on tap port and offer pilot's XDS istio.io/debug debug API there.").Get()
	proxyXDSDebugViaAgentPort = env.RegisterIntVar("PROXY_XDS_DEBUG_VIA_AGENT_PORT", 15004,
		"Agent debugging port.").Get()
	// DNSCaptureByAgent is a copy of the env var in the init code.
	DNSCaptureByAgent = env.RegisterBoolVar("ISTIO_META_DNS_CAPTURE", false,
		"If set to true, enable the capture of outgoing DNS packets on port 53, redirecting to istio-agent on :15053")

	// DNSCaptureAddr is the address to listen.
	DNSCaptureAddr = env.RegisterStringVar("DNS_PROXY_ADDR", "localhost:15053",
		"Custom address for the DNS proxy. If it ends with :53 and running as root allows running without iptable DNS capture")

	// Ability of istio-agent to retrieve proxyConfig via XDS for dynamic configuration updates
	enableProxyConfigXdsEnv = env.RegisterBoolVar("PROXY_CONFIG_XDS_AGENT", false,
		"If set to true, agent retrieves dynamic proxy-config updates via xds channel").Get()

	wasmInsecureRegistries = env.RegisterStringVar("WASM_INSECURE_REGISTRIES", "",
		"allow agent pull wasm plugin from insecure registries, for example: 'localhost:5000,docker-registry:5000'").Get()

	// Ability of istio-agent to retrieve bootstrap via XDS
	enableBootstrapXdsEnv = env.RegisterBoolVar("BOOTSTRAP_XDS_AGENT", false,
		"If set to true, agent retrieves the bootstrap configuration prior to starting Envoy").Get()

	envoyStatusPortEnv = env.RegisterIntVar("ENVOY_STATUS_PORT", 15021,
		"Envoy health status port value").Get()
	envoyPrometheusPortEnv = env.RegisterIntVar("ENVOY_PROMETHEUS_PORT", 15090,
		"Envoy prometheus redirection port value").Get()

	// Defined by https://github.com/grpc/proposal/blob/c5722a35e71f83f07535c6c7c890cf0c58ec90c0/A27-xds-global-load-balancing.md#xdsclient-and-bootstrap-file
	grpcBootstrapEnv = env.RegisterStringVar("GRPC_XDS_BOOTSTRAP", filepath.Join(constants.ConfigPathDir, "grpc-bootstrap.json"),
		"Path where gRPC expects to read a bootstrap file. Agent will generate one if set.").Get()

	disableEnvoyEnv = env.RegisterBoolVar("DISABLE_ENVOY", false,
		"Disables all Envoy agent features.").Get()

	// certSigner is cert signer for workload cert
	certSigner = env.RegisterStringVar("ISTIO_META_CERT_SIGNER", "",
		"The cert signer info for workload cert")

	istiodSAN = env.RegisterStringVar("ISTIOD_SAN", "",
		"Override the ServerName used to validate Istiod certificate. "+
			"Can be used as an alternative to setting /etc/hosts for VMs - discovery address will be an IP:port")

	minimumDrainDurationEnv = env.RegisterDurationVar("MINIMUM_DRAIN_DURATION",
		5*time.Second,
		"The minimum duration for which agent waits before it checks for active connections and terminates proxy"+
			"when number of active connections become zero").Get()

	exitOnZeroActiveConnectionsEnv = env.RegisterBoolVar("EXIT_ON_ZERO_ACTIVE_CONNECTIONS",
		false,
		"When set to true, terminates proxy when number of active connections become zero during draining").Get()
)
