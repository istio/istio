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

package security

import (
	"context"
	"time"

	"google.golang.org/grpc"
)

const (
	// etc/certs files are used with external CA managing the certs,
	// i.e. mounted Secret or external plugin.
	// If present, FileMountedCerts should be true.

	// The well-known path for an existing certificate chain file
	DefaultCertChainFilePath = "./etc/certs/cert-chain.pem"

	// The well-known path for an existing key file
	DefaultKeyFilePath = "./etc/certs/key.pem"

	// DefaultRootCertFilePath is the well-known path for an existing root certificate file
	DefaultRootCertFilePath = "./etc/certs/root-cert.pem"

	// Credential fetcher type
	GCE  = "GoogleComputeEngine"
	Mock = "Mock" // testing only
)

// Options provides all of the configuration parameters for secret discovery service
// and CA configuration. Used in both Istiod and Agent.
// TODO: ProxyConfig should have most of those, and be passed to all components
// (as source of truth)
type Options struct {
	// PluginNames is plugins' name for certain authentication provider.
	PluginNames []string

	// WorkloadUDSPath is the unix domain socket through which SDS server communicates with workload proxies.
	WorkloadUDSPath string

	// IngressGatewayUDSPath is the unix domain socket through which SDS server communicates with
	// ingress gateway proxies.
	GatewayUDSPath string

	// CertFile is the path of Cert File for gRPC server TLS settings.
	CertFile string

	// KeyFile is the path of Key File for gRPC server TLS settings.
	KeyFile string

	// CAEndpoint is the CA endpoint to which node agent sends CSR request.
	CAEndpoint string

	// The CA provider name.
	CAProviderName string

	// TrustDomain corresponds to the trust root of a system.
	// https://github.com/spiffe/spiffe/blob/master/standards/SPIFFE-ID.md#21-trust-domain
	TrustDomain string

	// The Vault CA address.
	VaultAddress string

	// The Vault auth path.
	VaultAuthPath string

	// The Vault role.
	VaultRole string

	// The Vault sign CSR path.
	VaultSignCsrPath string

	// The Vault TLS root certificate.
	VaultTLSRootCert string

	// GrpcServer is an already configured (shared) grpc server. If set, the agent will just register on the server.
	GrpcServer *grpc.Server

	// Recycle job running interval (to clean up staled sds client connections).
	RecycleInterval time.Duration

	// Debug server port from which node_agent serves SDS configuration dumps
	DebugPort int

	// EnableWorkloadSDS indicates whether node agent works as SDS server for workload proxies.
	EnableWorkloadSDS bool

	// EnableGatewaySDS indicates whether node agent works as ingress gateway agent.
	EnableGatewaySDS bool

	// UseLocalJWT is set when the sds server should use its own local JWT, and not expect one
	// from the UDS caller. Used when it runs in the same container with Envoy.
	UseLocalJWT bool

	// Whether to generate PKCS#8 private keys.
	Pkcs8Keys bool

	// Location of JWTPath to connect to CA.
	JWTPath string

	// OutputKeyCertToDir is the directory for output the key and certificate
	OutputKeyCertToDir string

	// ProvCert is the directory for client to provide the key and certificate to server
	// when do mtls
	ProvCert string

	// Existing certs, for VM or existing certificates
	CertsDir string

	// whether  ControlPlaneAuthPolicy is MUTUAL_TLS
	TLSEnabled bool

	// ClusterID is the cluster where the agent resides.
	// Normally initialized from ISTIO_META_CLUSTER_ID - after a tortuous journey it
	// makes its way into the ClusterID metadata of Citadel gRPC request to create the cert.
	// Didn't find much doc - but I suspect used for 'central cluster' use cases - so should
	// match the cluster name set in the MC setup.
	ClusterID string

	// The type of Elliptical Signature algorithm to use
	// when generating private keys. Currently only ECDSA is supported.
	ECCSigAlg string

	// FileMountedCerts indicates whether the proxy is using file
	// mounted certs created by a foreign CA. Refresh is managed by the external
	// CA, by updating the Secret or VM file. We will watch the file for changes
	// or check before the cert expires. This assumes the certs are in the
	// well-known ./etc/certs location.
	FileMountedCerts bool

	// PilotCertProvider is the provider of the Pilot certificate (PILOT_CERT_PROVIDER env)
	// Determines the root CA file to use for connecting to CA gRPC:
	// - istiod
	// - kubernetes
	// - custom
	PilotCertProvider string

	// secret TTL.
	SecretTTL time.Duration

	// The initial backoff time in millisecond to avoid the thundering herd problem.
	InitialBackoffInMilliSec int64

	// secret should be rotated if:
	// time.Now.After(<secret ExpireTime> - <secret TTL> * SecretRotationGracePeriodRatio)
	SecretRotationGracePeriodRatio float64

	// Key rotation job running interval.
	RotationInterval time.Duration

	// Cached secret will be removed from cache if (time.now - secretItem.CreatedTime >= evictionDuration), this prevents cache growing indefinitely.
	EvictionDuration time.Duration

	// authentication provider specific plugins, will exchange the token
	// For example exchange long lived refresh with access tokens.
	// Used by the secret fetcher when signing CSRs.
	TokenExchangers []TokenExchanger

	// CSR requires a token. This is a property of the CA.
	// The default value is false because Istiod does not require a token in CSR.
	UseTokenForCSR bool

	// credential fetcher.
	CredFetcher CredFetcher

	// whether need to skip parsing token to inspect information like expiration time
	// Default is false.
	SkipParseToken bool
}

// Client interface defines the clients need to implement to talk to CA for CSR.
// The Agent will create a key pair and a CSR, and use an implementation of this
// interface to get back a signed certificate. There is no guarantee that the SAN
// in the request will be returned - server may replace it.
type Client interface {
	CSRSign(ctx context.Context, reqID string, csrPEM []byte, subjectID string,
		certValidTTLInSec int64) ([]string /*PEM-encoded certificate chain*/, error)
}

// SecretManager defines secrets management interface which is used by SDS.
type SecretManager interface {
	// GenerateSecret generates new secret and cache the secret.
	// Current implementation constructs the SAN based on the token's 'sub'
	// claim, expected to be in the K8S format. No other JWTs are currently supported
	// due to client logic. If JWT is missing/invalid, the resourceName is used.
	GenerateSecret(ctx context.Context, connectionID, resourceName, token string) (*SecretItem, error)

	// ShouldWaitForIngressGatewaySecret indicates whether a valid ingress gateway secret is expected.
	ShouldWaitForGatewaySecret(connectionID, resourceName, token string, fileMountedCertsOnly bool) bool

	// SecretExist checks if secret already existed.
	// This API is used for sds server to check if coming request is ack request.
	SecretExist(connectionID, resourceName, token, version string) bool

	// DeleteSecret deletes a secret by its key from cache.
	DeleteSecret(connectionID, resourceName string)
}

// TokenExchanger provides common interfaces so that authentication providers could choose to implement their specific logic.
type TokenExchanger interface {
	ExchangeToken(ctx context.Context, credFetcher CredFetcher, trustDomain,
		serviceAccountToken string) (string /*access token*/, time.Time /*expireTime*/, int /*httpRespCode*/, error)
}

// SecretItem is the cached item in in-memory secret store.
type SecretItem struct {
	CertificateChain []byte
	PrivateKey       []byte

	RootCert []byte

	// RootCertOwnedByCompoundSecret is true if this SecretItem was created by a
	// K8S secret having both server cert/key and client ca and should be deleted
	// with the secret.
	RootCertOwnedByCompoundSecret bool

	// ResourceName passed from envoy SDS discovery request.
	// "ROOTCA" for root cert request, "default" for key/cert request.
	ResourceName string

	// Credential token passed from envoy, caClient uses this token to send
	// CSR to CA to sign certificate.
	Token string

	// Version is used(together with token and ResourceName) to identify discovery request from
	// envoy which is used only for confirm purpose.
	Version string

	CreatedTime time.Time

	ExpireTime time.Time
}

type CredFetcher interface {
	// GetPlatformCredential fetches workload credential provided by the platform.
	GetPlatformCredential() (string, error)

	// GetType returns credential fetcher type. Currently the supported type is "GoogleComputeEngine".
	GetType() string

	// The name of the IdentityProvider that can authenticate the workload credential.
	GetIdentityProvider() string
}
