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
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/metadata"

	"istio.io/pkg/env"
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

	// LocalSDS is the location of the in-process SDS server - must be in a writeable dir.
	DefaultLocalSDSPath = "./etc/istio/proxy/SDS"

	// SystemRootCerts is special case input for root cert configuration to use system root certificates.
	SystemRootCerts = "SYSTEM"

	// RootCertReqResourceName is resource name of discovery request for root certificate.
	RootCertReqResourceName = "ROOTCA"

	// WorkloadKeyCertResourceName is the resource name of the discovery request for workload
	// identity.
	// TODO: change all the pilot one reference definition here instead.
	WorkloadKeyCertResourceName = "default"

	// Credential fetcher type
	GCE  = "GoogleComputeEngine"
	Mock = "Mock" // testing only
)

// TODO: For 1.8, make sure MeshConfig is updated with those settings,
// they should be dynamic to allow migrations without restart.
// Both are critical.
var (
	// Require 3P TOKEN disables the use of K8S 1P tokens. Note that 1P tokens can be used to request
	// 3P TOKENS. A 1P token is the token automatically mounted by Kubelet and used for authentication with
	// the Apiserver.
	Require3PToken = env.RegisterBoolVar("REQUIRE_3P_TOKEN", false,
		"Reject k8s default tokens, without audience. If false, default K8S token will be accepted")

	// TokenAudiences specifies a list of audiences for SDS trustworthy JWT. This is to make sure that the CSR requests
	// contain the JWTs intended for Citadel.
	TokenAudiences = strings.Split(env.RegisterStringVar("TOKEN_AUDIENCES", "istio-ca",
		"A list of comma separated audiences to check in the JWT token before issuing a certificate. "+
			"The token is accepted if it matches with one of the audiences").Get(), ",")

	BearerTokenPrefix = "Bearer" + " "
)

// Options provides all of the configuration parameters for secret discovery service
// and CA configuration. Used in both Istiod and Agent.
// TODO: ProxyConfig should have most of those, and be passed to all components
// (as source of truth)
type Options struct {
	// WorkloadUDSPath is the unix domain socket through which SDS server communicates with workload proxies.
	WorkloadUDSPath string

	// CAEndpoint is the CA endpoint to which node agent sends CSR request.
	CAEndpoint string

	// The CA provider name.
	CAProviderName string

	// TrustDomain corresponds to the trust root of a system.
	// https://github.com/spiffe/spiffe/blob/master/standards/SPIFFE-ID.md#21-trust-domain
	TrustDomain string

	// Whether to generate PKCS#8 private keys.
	Pkcs8Keys bool

	// Location of JWTPath to connect to CA.
	JWTPath string

	// OutputKeyCertToDir is the directory for output the key and certificate
	OutputKeyCertToDir string

	// ProvCert is the directory for client to provide the key and certificate to CA server when authenticating
	// with mTLS. This is not used for workload mTLS communication, and is
	ProvCert string

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

	// The ratio of cert lifetime to refresh a cert. For example, at 0.10 and 1 hour TTL,
	// we would refresh 6 minutes before expiration.
	SecretRotationGracePeriodRatio float64

	// authentication provider specific plugins, will exchange the token
	// For example exchange long lived refresh with access tokens.
	// Used by the secret fetcher when signing CSRs.
	// Optional; if not present the token will be used directly
	TokenExchanger TokenExchanger

	// credential fetcher.
	CredFetcher CredFetcher

	// credential identity provider
	CredIdentityProvider string

	// Namespace corresponding to workload
	WorkloadNamespace string

	// Name of the Service Account
	ServiceAccount string

	// XDS auth provider
	XdsAuthProvider string

	// Token manager for the token exchange of XDS
	TokenManager TokenManager
}

// TokenManager contains methods for generating token.
type TokenManager interface {
	// GenerateToken takes STS request parameters and generates token. Returns
	// StsResponseParameters in JSON.
	GenerateToken(parameters StsRequestParameters) ([]byte, error)
	// DumpTokenStatus dumps status of all generated tokens and returns status in JSON.
	DumpTokenStatus() ([]byte, error)
	// GetMetadata returns the metadata headers related to the token
	GetMetadata(forCA bool, xdsAuthProvider, token string) (map[string]string, error)
}

// StsRequestParameters stores all STS request attributes defined in
// https://tools.ietf.org/html/draft-ietf-oauth-token-exchange-16#section-2.1
type StsRequestParameters struct {
	// REQUIRED. The value "urn:ietf:params:oauth:grant-type:token- exchange"
	// indicates that a token exchange is being performed.
	GrantType string
	// OPTIONAL. Indicates the location of the target service or resource where
	// the client intends to use the requested security token.
	Resource string
	// OPTIONAL. The logical name of the target service where the client intends
	// to use the requested security token.
	Audience string
	// OPTIONAL. A list of space-delimited, case-sensitive strings, that allow
	// the client to specify the desired Scope of the requested security token in the
	// context of the service or Resource where the token will be used.
	Scope string
	// OPTIONAL. An identifier, for the type of the requested security token.
	RequestedTokenType string
	// REQUIRED. A security token that represents the identity of the party on
	// behalf of whom the request is being made.
	SubjectToken string
	// REQUIRED. An identifier, that indicates the type of the security token in
	// the "subject_token" parameter.
	SubjectTokenType string
	// OPTIONAL. A security token that represents the identity of the acting party.
	ActorToken string
	// An identifier, that indicates the type of the security token in the
	// "actor_token" parameter.
	ActorTokenType string
}

// Client interface defines the clients need to implement to talk to CA for CSR.
// The Agent will create a key pair and a CSR, and use an implementation of this
// interface to get back a signed certificate. There is no guarantee that the SAN
// in the request will be returned - server may replace it.
type Client interface {
	CSRSign(csrPEM []byte, certValidTTLInSec int64) ([]string, error)
	Close()
}

// SecretManager defines secrets management interface which is used by SDS.
type SecretManager interface {
	// GenerateSecret generates new secret for the given resource.
	//
	// The current implementation also watched the generated secret and trigger a callback when it is
	// near expiry. It will constructs the SAN based on the token's 'sub' claim, expected to be in
	// the K8S format. No other JWTs are currently supported due to client logic. If JWT is
	// missing/invalid, the resourceName is used.
	GenerateSecret(resourceName string) (*SecretItem, error)
}

// TokenExchanger provides common interfaces so that authentication providers could choose to implement their specific logic.
type TokenExchanger interface {
	// ExchangeToken provides a common interface to exchange an existing token for a new one.
	ExchangeToken(serviceAccountToken string) (string, error)
}

// SecretItem is the cached item in in-memory secret store.
type SecretItem struct {
	CertificateChain []byte
	PrivateKey       []byte

	RootCert []byte

	// ResourceName passed from envoy SDS discovery request.
	// "ROOTCA" for root cert request, "default" for key/cert request.
	ResourceName string

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

	// Stop releases resources and cleans up.
	Stop()
}

// AuthSource represents where authentication result is derived from.
type AuthSource int

const (
	AuthSourceClientCertificate AuthSource = iota
	AuthSourceIDToken
)

const (
	// IdentityTemplate is the SPIFFE format template of the identity.
	IdentityTemplate = "spiffe://%s/ns/%s/sa/%s"

	authorizationMeta = "authorization"
)

// Caller carries the identity and authentication source of a caller.
type Caller struct {
	AuthSource AuthSource
	Identities []string
}

type Authenticator interface {
	Authenticate(ctx context.Context) (*Caller, error)
	AuthenticatorType() string
}

func ExtractBearerToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", fmt.Errorf("no metadata is attached")
	}

	authHeader, exists := md[authorizationMeta]
	if !exists {
		return "", fmt.Errorf("no HTTP authorization header exists")
	}

	for _, value := range authHeader {
		if strings.HasPrefix(value, BearerTokenPrefix) {
			return strings.TrimPrefix(value, BearerTokenPrefix), nil
		}
	}

	return "", fmt.Errorf("no bearer token exists in HTTP authorization header")
}
