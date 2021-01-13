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
	"fmt"
	"strings"
	"time"

	meshconfig "istio.io/api/mesh/v1alpha1"
	securityModel "istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/jwt"
	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/credentialfetcher"
	"istio.io/istio/security/pkg/nodeagent/plugin/providers/google/stsclient"
	"istio.io/pkg/log"
)

func SetupSecurityOptions(proxyConfig meshconfig.ProxyConfig, jwtPolicy, clusterIDVar, podNamespaceVar,
	serviceAccountVar, caEndpointEnv, caProviderEnv, pilotCertProvider, outputKeyCertToDir, provCert, trustDomainEnv,
	eccSigAlgEnv, credFetcherTypeEnv, credIdentityProvider, xdsAuthProvider string, fileMountedCertsEnv, pkcs8KeysEnv bool,
	secretTTLEnv time.Duration, secretRotationGracePeriodRatioEnv float64) (security.Options, error) {
	var jwtPath string
	if jwtPolicy == jwt.PolicyThirdParty {
		log.Info("JWT policy is third-party-jwt")
		jwtPath = constants.TrustworthyJWTPath
	} else if jwtPolicy == jwt.PolicyFirstParty {
		log.Info("JWT policy is first-party-jwt")
		jwtPath = securityModel.K8sSAJwtFileName
	} else {
		log.Info("Using existing certs")
	}

	o := security.Options{
		CAEndpoint:                     caEndpointEnv,
		CAProviderName:                 caProviderEnv,
		PilotCertProvider:              pilotCertProvider,
		OutputKeyCertToDir:             outputKeyCertToDir,
		ProvCert:                       provCert,
		JWTPath:                        jwtPath,
		WorkloadUDSPath:                security.DefaultLocalSDSPath,
		ClusterID:                      clusterIDVar,
		FileMountedCerts:               fileMountedCertsEnv,
		WorkloadNamespace:              podNamespaceVar,
		ServiceAccount:                 serviceAccountVar,
		XdsAuthProvider:                xdsAuthProvider,
		TrustDomain:                    trustDomainEnv,
		Pkcs8Keys:                      pkcs8KeysEnv,
		ECCSigAlg:                      eccSigAlgEnv,
		SecretTTL:                      secretTTLEnv,
		SecretRotationGracePeriodRatio: secretRotationGracePeriodRatioEnv,
	}
	// If not set explicitly, default to the discovery address.
	if o.CAEndpoint == "" {
		o.CAEndpoint = proxyConfig.DiscoveryAddress
	}

	// TODO (liminw): CredFetcher is a general interface. In 1.7, we limit the use on GCE only because
	// GCE is the only supported plugin at the moment.
	if credFetcherTypeEnv == security.GCE {
		o.CredIdentityProvider = credIdentityProvider
		credFetcher, err := credentialfetcher.NewCredFetcher(credFetcherTypeEnv, o.TrustDomain, jwtPath, o.CredIdentityProvider)
		if err != nil {
			return security.Options{}, fmt.Errorf("failed to create credential fetcher: %v", err)
		}
		log.Infof("using credential fetcher of %s type in %s trust domain", credFetcherTypeEnv, o.TrustDomain)
		o.CredFetcher = credFetcher
	}
	// TODO extract this logic out to a plugin
	if o.CAProviderName == "GoogleCA" || strings.Contains(o.CAEndpoint, "googleapis.com") {
		o.TokenExchanger = stsclient.NewSecureTokenServiceExchanger(o.CredFetcher, o.TrustDomain)
	}

	if o.ProvCert != "" && o.FileMountedCerts {
		return security.Options{}, fmt.Errorf("invalid options: PROV_CERT and FILE_MOUNTED_CERTS are mutually exclusive")
	}
	return o, nil
}
