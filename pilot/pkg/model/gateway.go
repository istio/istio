// Copyright 2018 Istio Authors.
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
	"reflect"
	"strings"

	routing "istio.io/api/networking/v1alpha3"
)

// MergeGateways merges servers from src into the server set on dst
//
// Merging happens on a per-server basis.
// When comparing two servers, there are three possible results:
//   1. If the servers have distinct port numbers and port names,
//      then the resulting Gateway (dst) will contain both
//   2. If the servers have identical port number, port name, and tls config
//      then their hosts are merged into a single server on the result (dst)
//   3. Otherwise, the servers are in considered to be in conflict
//      and an error will be returned
//
// Missing features (TODO)
//   - respect the 'selector' field
//   - allow merging when ports exactly match but tls config differs (SNI)
//   - allow merging h1, h2 and grpc protocols
//
// See also: Merging Gateways and RouteRules
//  https://docs.google.com/document/d/1z9jOZ1f4MhC3Fvisduio8IoUqd1_Eqrd3kG65M6n854
func MergeGateways(dst, src *routing.Gateway) error {
	// Simplify the loop logic below by handling the case where either Gateway is empty.
	if len(dst.Servers) == 0 {
		dst.Servers = src.Servers
		return nil
	} else if len(src.Servers) == 0 {
		return nil
	}

	servers := make([]*routing.Server, 0, len(dst.Servers))
	for _, ss := range src.Servers {
		for _, ds := range dst.Servers {
			if serversEqual(ss, ds) {
				ds.Hosts = append(ds.Hosts, ss.Hosts...)
			} else if portsAreDistinct(ss.Port, ds.Port) {
				servers = append(servers, ss)
			} else {
				return fmt.Errorf("unable to merge gateways: conflicting ports: %v and %v", ds.Port, ss.Port)
			}
			servers = append(servers, ds)
		}
	}
	dst.Servers = servers
	return nil
}

func serversEqual(a, b *routing.Server) bool {
	return portsEqual(a.Port, b.Port) && tlsEqual(a.Tls, b.Tls)
}

// Two ports are distinct if they have different names and different numbers
// In this case, they can each have their own listener without any
// ambiguity about which routing rules apply to which.
//
// Note it is possible for two ports to be neither distinct nor equal.
// In that case, they conflict.
//
// TODO: once SNI and TLS-snooping work, we should be able to
// avoid many of those conflicts.
func portsAreDistinct(a, b *routing.Port) bool {
	return a.Number != b.Number && a.Name != b.Name
}

// Two ports are equal if they expose the same protocol on the same port number
// with the same name.  They can only be merged if they are equal.
func portsEqual(a, b *routing.Port) bool {
	return a.Number == b.Number && a.Name == b.Name &&
		strings.ToLower(a.Protocol) == strings.ToLower(b.Protocol)

}

// Two TLS Options are equal if all of their fields are equal.
func tlsEqual(a, b *routing.Server_TLSOptions) bool {
	return reflect.DeepEqual(a, b)
}
