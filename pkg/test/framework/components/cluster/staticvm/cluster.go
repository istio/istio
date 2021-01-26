package staticvm

import (
	"bytes"
	"fmt"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/resource"
)

var _ resource.Cluster = &vmcluster{}
var _ echo.Claimer = &vmcluster{}

type staticVm struct {
	config  echo.Config
	address string
}

type vmcluster struct {
	kube.ExtendedClient
	cluster.Topology

	vms []echo.Config
}

func (v *vmcluster) hasPrimary() bool {
	return v.Primary() != v
}

func (v *vmcluster) Claim(configs []echo.Config) (claimed []echo.Config, unclaimed []echo.Config) {
	for _, config := range configs {
		// the generic connfig and the vm cluster have a cluster defined
		// make sure they point to the same primary cluster
		if config.Cluster != nil && v.hasPrimary() && v.Primary() != config.Cluster.Primary() {
			unclaimed = append(unclaimed, config)
			continue
		}
		for _, vmConfig := range v.vms {
			compare := vmConfig
			// address isn't expected to match
			compare.StaticAddress = ""
			// If the config to match requires a specific cluster, try to match
			// by using the primary cluster configured for the entire VM cluster.
			if config.Cluster != nil && compare.Cluster == nil && v.hasPrimary() {
				compare.Cluster = v.Primary()
			}
			if !config.Equals(compare) {
				unclaimed = append(unclaimed, config)
				continue
			}
			claimed = append(claimed, config)
		}
	}
	return
}

func (v *vmcluster) String() string {
	buf := &bytes.Buffer{}

	_, _ = fmt.Fprintf(buf, v.Topology.String())
	_, _ = fmt.Fprintf(buf, "VMs:           %d\n", len(v.vms))

	return buf.String()
}
