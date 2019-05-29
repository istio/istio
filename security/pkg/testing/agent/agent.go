package agent

import (
	"fmt"
	"istio.io/istio/security/pkg/nodeagent/sds"
)

var (
	// serverOptions is the default testing options for creating a Citadel Agent instance.
	serverOptions = sds.Options {
		WorkloadUDSPath: "unix:/var/run/sds-place-holder",
		CAEndpoint: "unix:/var/run/citadel-place-holder",
		TrustDomain: "cluster.local",
	}
)

// New creates a Citadel Agent testing instance.
func New() error {
	return fmt.Errorf("not implemetned yet")
}
