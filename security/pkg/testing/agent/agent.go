package agent

import (
	"fmt"
	"istio.io/istio/security/pkg/nodeagent/sds"
)

var (
	serverOptions = sds.Options {
		// WorkloadUDSPath is the unix domain socket through which SDS server communicates with workload proxies.
		 WorkloadUDSPath: "unix:/var/run/sds",
		//IngressGatewayUDSPath string

		// CertFile is the path of Cert File for gRPC server TLS settings.
		//CertFile string

		// KeyFile is the path of Key File for gRPC server TLS settings.
		//KeyFile string

		// CAEndpoint is the CA endpoint to which node agent sends CSR request.
		//CAEndpoint string

		// The CA provider name.
		//CAProviderName string
		TrustDomain: "cluster.local",
	}
)

func New() error {
	return fmt.Errorf("not implemetned yet")
}
