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

package matcher

import (
	"fmt"
	"net"
	"strings"

	"github.com/golang/protobuf/ptypes/wrappers"

	corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
)

// CidrRange converts a CIDR or a single IP string to a corresponding CidrRange. For a single IP
// string the converted CidrRange prefix is either 32 (for ipv4) or 128 (for ipv6).
func CidrRange(v string) (*corepb.CidrRange, error) {
	var address string
	var prefixLen int

	if strings.Contains(v, "/") {
		if ip, ipnet, err := net.ParseCIDR(v); err == nil {
			address = ip.String()
			prefixLen, _ = ipnet.Mask.Size()
		} else {
			return nil, fmt.Errorf("invalid cidr range: %v", err)
		}
	} else {
		if ip := net.ParseIP(v); ip != nil {
			address = ip.String()
			if strings.Contains(v, ".") {
				// Set the prefixLen to 32 for ipv4 address.
				prefixLen = 32
			} else if strings.Contains(v, ":") {
				// Set the prefixLen to 128 for ipv6 address.
				prefixLen = 128
			} else {
				return nil, fmt.Errorf("invalid ip address: %s", v)
			}
		} else {
			return nil, fmt.Errorf("invalid ip address: %s", v)
		}
	}

	return &corepb.CidrRange{
		AddressPrefix: address,
		PrefixLen:     &wrappers.UInt32Value{Value: uint32(prefixLen)},
	}, nil
}
