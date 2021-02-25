package util

import (
	"fmt"
	"net"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	kubenet "k8s.io/utils/net"
)

func CIDRRangeOverlap(a, b *core.CidrRange) bool {
	// make it so a has the shorter prefix (larger range)
	if a.PrefixLen.Value > b.PrefixLen.Value {
		swp := a
		a = b
		b = swp
	}
	nets, err := toIPNets(a, b)
	if err != nil || len(nets) != 2 {
		panic(err)
	}
	neta, netb := nets[0], nets[1]

	// the smaller network starts either before or after the larger network
	if !neta.Contains(netb.IP) {
		return false
	}

	return true
}

func toIPNets(ranges ...*core.CidrRange) ([]*net.IPNet, error) {
	cidrs := make([]string, 0, len(ranges))
	for _, cidrRange := range ranges {
		cidrs = append(cidrs, fmt.Sprintf("%s/%d", cidrRange.AddressPrefix, cidrRange.PrefixLen.GetValue()))
	}
	return kubenet.ParseCIDRs(cidrs)
}
