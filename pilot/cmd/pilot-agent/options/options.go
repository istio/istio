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

package options

import (
	"time"

	"istio.io/istio/pilot/cmd/pilot-agent/status"
	"istio.io/istio/pkg/jwt"
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

	pilotCertProvider = env.RegisterStringVar("PILOT_CERT_PROVIDER", "istiod",
		"The provider of Pilot DNS certificate.").Get()
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
	secretRotationGracePeriodRatioEnv = env.RegisterFloatVar("SECRET_GRACE_PERIOD_RATIO", 0.5,
		"The grace period ratio for the cert rotation, by default 0.5.").Get()
	pkcs8KeysEnv = env.RegisterBoolVar("PKCS8_KEY", false,
		"Whether to generate PKCS#8 private keys").Get()
	eccSigAlgEnv        = env.RegisterStringVar("ECC_SIGNATURE_ALGORITHM", "", "The type of ECC signature algorithm to use when generating private keys").Get()
	fileMountedCertsEnv = env.RegisterBoolVar("FILE_MOUNTED_CERTS", false, "").Get()
	credFetcherTypeEnv  = env.RegisterStringVar("CREDENTIAL_FETCHER_TYPE", "",
		"The type of the credential fetcher. Currently supported types include GoogleComputeEngine").Get()
	credIdentityProvider = env.RegisterStringVar("CREDENTIAL_IDENTITY_PROVIDER", "GoogleComputeEngine",
		"The identity provider for credential. Currently default supported identity provider is GoogleComputeEngine").Get()
	proxyXDSViaAgent = env.RegisterBoolVar("PROXY_XDS_VIA_AGENT", true,
		"If set to true, envoy will proxy XDS calls via the agent instead of directly connecting to istiod. This option "+
			"will be removed once the feature is stabilized.").Get()
	// This is a copy of the env var in the init code.
	dnsCaptureByAgent = env.RegisterBoolVar("ISTIO_META_DNS_CAPTURE", false,
		"If set to true, enable the capture of outgoing DNS packets on port 53, redirecting to istio-agent on :15053").Get()
)
