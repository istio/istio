package cacheutil

import (
	meshconfig "istio.io/api/mesh/v1alpha1"
	securityModel "istio.io/istio/pilot/pkg/security/model"
	istioagent "istio.io/istio/pkg/istio-agent"
	"istio.io/istio/pkg/jwt"
	"istio.io/istio/pkg/util/gogoprotomarshal"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

var (
	ProxyConfig meshconfig.ProxyConfig

	cacheUtilLog       = log.RegisterScope("cacheUtil", "cache util debugging", 0)

	jwtPolicy = env.RegisterStringVar("JWT_POLICY", jwt.PolicyThirdParty,
		"The JWT validation policy.")
	pilotCertProvider = env.RegisterStringVar("PILOT_CERT_PROVIDER", "istiod",
		"the provider of Pilot DNS certificate.").Get()
	outputKeyCertToDir = env.RegisterStringVar("OUTPUT_CERTS", "",
		"The output directory for the key and certificate. If empty, key and certificate will not be saved. "+
				"Must be set for VMs using provisioning certificates.").Get()
	clusterIDVar         = env.RegisterStringVar("ISTIO_META_CLUSTER_ID", "", "")
)

const (
	// trustworthy token JWTPath
	trustworthyJWTPath = "./var/run/secrets/tokens/istio-token"
)

func GetNewAgent() (*istioagent.Agent, error) {
	var jwtPath string
	if jwtPolicy.Get() == jwt.PolicyThirdParty {
		cacheUtilLog.Info("JWT policy is third-party-jwt")
		jwtPath = trustworthyJWTPath
	} else if jwtPolicy.Get() == jwt.PolicyFirstParty {
		cacheUtilLog.Info("JWT policy is first-party-jwt")
		jwtPath = securityModel.K8sSAJwtFileName
	} else {
		cacheUtilLog.Info("Using existing certs")
	}
	if out, err := gogoprotomarshal.ToYAML(&ProxyConfig); err != nil {
		cacheUtilLog.Infof("Failed to serialize to YAML: %v", err)
	} else {
		cacheUtilLog.Infof("Effective config: %s", out)
	}
	sa := istioagent.NewAgent(&ProxyConfig, &istioagent.AgentConfig{
		PilotCertProvider:  pilotCertProvider,
		JWTPath:            jwtPath,
		OutputKeyCertToDir: outputKeyCertToDir,
		ClusterID:          clusterIDVar.Get(),
	})
	return sa, nil
}