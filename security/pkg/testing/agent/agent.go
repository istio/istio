package agent

import (
	"istio.io/istio/security/pkg/nodeagent/cache"

	"istio.io/istio/security/pkg/nodeagent/sds"
)

var (
	// serverOptions is the default testing options for creating a Citadel Agent instance.
	serverOptions = sds.Options{
		WorkloadUDSPath: "unix:/var/run/sds-place-holder",
		CAEndpoint:      "unix:/var/run/citadel-place-holder",
		TrustDomain:     "cluster.local",
	}
	workloadCacheOption = cache.Options{}
	gatewayCacheOption  = cache.Options{}
)

// New creates a Citadel Agent testing instance.
func New() (*sds.Server, error) {
	server, err := sds.NewServer(serverOptions, workloadCacheOption, gatewayCacheOption)
	if err != nil {
		return nil, err
	}
	return server, nil
}
