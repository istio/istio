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
	"fmt"
	"os"
	"strings"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/jwt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/security"
	"istio.io/istio/security/pkg/credentialfetcher"
	"istio.io/istio/security/pkg/nodeagent/cafile"
)

// Similar with ISTIO_META_, which is used to customize the node metadata - this customizes extra CA header.
const caHeaderPrefix = "CA_HEADER_"

func NewSecurityOptions(proxyConfig *meshconfig.ProxyConfig, stsPort int, tokenManagerPlugin string) (*security.Options, error) {
	o := &security.Options{
		CAEndpoint:                           caEndpointEnv,
		CAProviderName:                       caProviderEnv,
		PilotCertProvider:                    features.PilotCertProvider,
		OutputKeyCertToDir:                   outputKeyCertToDir,
		ProvCert:                             provCert,
		ClusterID:                            clusterIDVar.Get(),
		FileMountedCerts:                     fileMountedCertsEnv,
		WorkloadNamespace:                    PodNamespaceVar.Get(),
		ServiceAccount:                       serviceAccountVar.Get(),
		XdsAuthProvider:                      xdsAuthProvider.Get(),
		TrustDomain:                          trustDomainEnv,
		WorkloadRSAKeySize:                   workloadRSAKeySizeEnv,
		Pkcs8Keys:                            pkcs8KeysEnv,
		ECCSigAlg:                            eccSigAlgEnv,
		ECCCurve:                             eccCurvEnv,
		SecretTTL:                            secretTTLEnv,
		FileDebounceDuration:                 fileDebounceDuration,
		SecretRotationGracePeriodRatio:       secretRotationGracePeriodRatioEnv,
		SecretRotationGracePeriodRatioJitter: secretRotationGracePeriodRatioJitterEnv,
		STSPort:                              stsPort,
		CertSigner:                           certSigner.Get(),
		CARootPath:                           cafile.CACertFilePath,
		CertChainFilePath:                    security.DefaultCertChainFilePath,
		KeyFilePath:                          security.DefaultKeyFilePath,
		RootCertFilePath:                     security.DefaultRootCertFilePath,
		CAHeaders:                            map[string]string{},
	}

	o, err := SetupSecurityOptions(proxyConfig, o, jwtPolicy.Get(),
		credFetcherTypeEnv, credIdentityProvider)
	if err != nil {
		return o, err
	}

	extractCAHeadersFromEnv(o)

	return o, err
}

func SetupSecurityOptions(proxyConfig *meshconfig.ProxyConfig, secOpt *security.Options, jwtPolicy,
	credFetcherTypeEnv, credIdentityProvider string,
) (*security.Options, error) {
	jwtPath := constants.ThirdPartyJwtPath
	switch jwtPolicy {
	case jwt.PolicyThirdParty:
		log.Info("JWT policy is third-party-jwt")
		jwtPath = constants.ThirdPartyJwtPath
	case jwt.PolicyFirstParty:
		log.Warnf("Using deprecated JWT policy 'first-party-jwt'; treating as 'third-party-jwt'")
		jwtPath = constants.ThirdPartyJwtPath
	default:
		log.Info("Using existing certs")
	}

	o := secOpt

	// If not set explicitly, default to the discovery address.
	if o.CAEndpoint == "" {
		o.CAEndpoint = proxyConfig.DiscoveryAddress
		o.CAEndpointSAN = istiodSAN.Get()
	}

	o.CredIdentityProvider = credIdentityProvider
	credFetcher, err := credentialfetcher.NewCredFetcher(credFetcherTypeEnv, o.TrustDomain, jwtPath, o.CredIdentityProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to create credential fetcher: %v", err)
	}
	log.Infof("using credential fetcher of %s type in %s trust domain", credFetcherTypeEnv, o.TrustDomain)
	o.CredFetcher = credFetcher

	if o.CAProviderName == security.GkeWorkloadCertificateProvider {
		if !security.CheckWorkloadCertificate(security.GkeWorkloadCertChainFilePath,
			security.GkeWorkloadKeyFilePath, security.GkeWorkloadRootCertFilePath) {
			return nil, fmt.Errorf("GKE workload certificate files (%v, %v, %v) not present",
				security.GkeWorkloadCertChainFilePath, security.GkeWorkloadKeyFilePath, security.GkeWorkloadRootCertFilePath)
		}
		if o.ProvCert != "" {
			return nil, fmt.Errorf(
				"invalid options: PROV_CERT and FILE_MOUNTED_CERTS of GKE workload cert are mutually exclusive")
		}
		o.FileMountedCerts = true
		o.CertChainFilePath = security.GkeWorkloadCertChainFilePath
		o.KeyFilePath = security.GkeWorkloadKeyFilePath
		o.RootCertFilePath = security.GkeWorkloadRootCertFilePath
		return o, nil
	}

	// Default the CA provider where possible
	if strings.Contains(o.CAEndpoint, "googleapis.com") {
		o.CAProviderName = security.GoogleCAProvider
	}

	if o.ProvCert != "" && o.FileMountedCerts {
		return nil, fmt.Errorf("invalid options: PROV_CERT and FILE_MOUNTED_CERTS are mutually exclusive")
	}
	return o, nil
}

// extractCAHeadersFromEnv extracts CA headers from environment variables.
func extractCAHeadersFromEnv(o *security.Options) {
	envs := os.Environ()
	for _, e := range envs {
		if !strings.HasPrefix(e, caHeaderPrefix) {
			continue
		}

		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 {
			continue
		}
		o.CAHeaders[parts[0][len(caHeaderPrefix):]] = parts[1]
	}
}
