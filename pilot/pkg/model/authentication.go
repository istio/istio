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

package model

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/gogo/protobuf/types"

	authn "istio.io/api/authentication/v1alpha1"
)

const (
	// CARootCertPath is the path of ca root cert that envoy uses to validate cert got from SDS service.
	CARootCertPath = "/etc/istio/certs/ca-root-cert.pem"

	// SDSStatPrefix is the human readable prefix to use when emitting statistics for the SDS service.
	SDSStatPrefix = "sdsstat"
)

// JwtKeyResolver resolves JWT public key and JwksURI.
var JwtKeyResolver = newJwksResolver(JwtPubKeyExpireDuration, JwtPubKeyEvictionDuration, JwtPubKeyRefreshInterval)

// GetConsolidateAuthenticationPolicy returns the authentication policy for
// service specified by hostname and port, if defined. It also tries to resolve JWKS URI if necessary.
func GetConsolidateAuthenticationPolicy(store IstioConfigStore, service *Service, port *Port) *authn.Policy {
	config := store.AuthenticationPolicyByDestination(service, port)
	if config != nil {
		policy := config.Spec.(*authn.Policy)
		if err := JwtKeyResolver.SetAuthenticationPolicyJwksURIs(policy); err == nil {
			return policy
		}
	}

	return nil
}

// ConstructSdsSecretConfig constructs SDS Sececret Configuration.
func ConstructSdsSecretConfig(serviceAccount string, sdsUdsPath string) *auth.SdsSecretConfig {
	if serviceAccount == "" || sdsUdsPath == "" {
		return nil
	}

	return &auth.SdsSecretConfig{
		Name: serviceAccount,
		SdsConfig: &core.ConfigSource{
			ConfigSourceSpecifier: &core.ConfigSource_ApiConfigSource{
				ApiConfigSource: &core.ApiConfigSource{
					ApiType: core.ApiConfigSource_GRPC,
					GrpcServices: []*core.GrpcService{
						{
							TargetSpecifier: &core.GrpcService_GoogleGrpc_{
								GoogleGrpc: &core.GrpcService_GoogleGrpc{
									TargetUri:  sdsUdsPath,
									StatPrefix: SDSStatPrefix,
									ChannelCredentials: &core.GrpcService_GoogleGrpc_ChannelCredentials{
										CredentialSpecifier: &core.GrpcService_GoogleGrpc_ChannelCredentials_LocalCredentials{
											LocalCredentials: &core.GrpcService_GoogleGrpc_GoogleLocalCredentials{},
										},
									},
									CallCredentials: []*core.GrpcService_GoogleGrpc_CallCredentials{
										&core.GrpcService_GoogleGrpc_CallCredentials{
											CredentialSpecifier: &core.GrpcService_GoogleGrpc_CallCredentials_GoogleComputeEngine{
												GoogleComputeEngine: &types.Empty{},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// ConstructValidationContext constructs ValidationContext in CommonTlsContext.
func ConstructValidationContext(rootCAFilePath string, subjectAltNames []string) *auth.CommonTlsContext_ValidationContext {
	ret := &auth.CommonTlsContext_ValidationContext{
		ValidationContext: &auth.CertificateValidationContext{
			TrustedCa: &core.DataSource{
				Specifier: &core.DataSource_Filename{
					Filename: rootCAFilePath,
				},
			},
		},
	}

	if len(subjectAltNames) > 0 {
		ret.ValidationContext.VerifySubjectAltName = subjectAltNames
	}

	return ret
}

// ParseJwksURI parses the input URI and returns the corresponding hostname, port, and whether SSL is used.
// URI must start with "http://" or "https://", which corresponding to "http" or "https" scheme.
// Port number is extracted from URI if available (i.e from postfix :<port>, eg. ":80"), or assigned
// to a default value based on URI scheme (80 for http and 443 for https).
// Port name is set to URI scheme value.
// Note: this is to replace [buildJWKSURIClusterNameAndAddress]
// (https://github.com/istio/istio/blob/master/pilot/pkg/proxy/envoy/v1/mixer.go#L401),
// which is used for the old EUC policy.
func ParseJwksURI(jwksURI string) (string, *Port, bool, error) {
	u, err := url.Parse(jwksURI)
	if err != nil {
		return "", nil, false, err
	}
	var useSSL bool
	var portNumber int
	switch u.Scheme {
	case "http":
		useSSL = false
		portNumber = 80
	case "https":
		useSSL = true
		portNumber = 443
	default:
		return "", nil, false, fmt.Errorf("URI scheme %q is not supported", u.Scheme)
	}

	if u.Port() != "" {
		portNumber, err = strconv.Atoi(u.Port())
		if err != nil {
			return "", nil, useSSL, err
		}
	}

	return u.Hostname(), &Port{
		Name: u.Scheme,
		Port: portNumber,
	}, useSSL, nil
}


// GetIstioServiceAccounts returns a list of service account for a particular service.
// TODO(incfly): the interface must be changed to hold some state for optimization.
func GetIstioServiceAccounts(registry ServiceDiscovery, hostname Hostname, ports []int) []string {
	instances := make([]*ServiceInstance, 0)
	saSet := make(map[string]bool)

	// Get the service accounts running the service, if it is deployed on VMs. This is retrieved
	// from the service annotation explicitly set by the operators.
	svc, err := registry.GetService(hostname)
	if err != nil {
		log.Errorf("InstancesByPort(%s:%d) error: %v", hostname, ports, err)
		return nil
	}
	if svc == nil {
		log.Errorf("InstancesByPort(%s:%d) error: %v", hostname, ports, err)
		return nil
	}

	// Get the service accounts running service within Kubernetes. This is reflected by the pods that
	// the service is deployed on, and the service accounts of the pods.
	for _, port := range ports {
		svcinstances, err := registry.InstancesByPort(hostname, port, LabelsCollection{})
		if err != nil {
			log.Warnf("InstancesByPort(%s:%d) error: %v", hostname, port, err)
			return nil
		}
		instances = append(instances, svcinstances...)
	}

	for _, si := range instances {
		if si.ServiceAccount != "" {
			saSet[si.ServiceAccount] = true
		}
	}

	// We add Service's service account as well. This comes from Service annotation to support mesh expansion.
	for _, serviceAccount := range svc.ServiceAccounts {
		sa := serviceAccount
		saSet[sa] = true
	}

	saArray := make([]string, 0, len(saSet))
	for sa := range saSet {
		saArray = append(saArray, sa)
	}

	return saArray
}