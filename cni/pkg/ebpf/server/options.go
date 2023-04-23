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

package server

import (
	"net"
	"net/netip"
)

// RedirectArgs provides all the configuration parameters for the redirection.
type RedirectArgs struct {
	// IPAddrs is IP address list of the POD
	IPAddrs []netip.Addr

	// MacAddr is mac address of veth in POD namespace
	MacAddr net.HardwareAddr

	// Ifindex is ifIndex of veth in host namespace
	Ifindex int

	// PeerIndex is ifIndex of veth in POD namespace
	PeerIndex int

	// IsZtunnel indicates if the POD is ztunnel
	IsZtunnel bool

	// PeerNs is POD's network namespace
	PeerNs string

	// Remove indicates if the operation is 'add' or 'remove'
	Remove bool

	// CaptureDNS indicates if CaptureDNS enabled. Only valid for ztunnel
	CaptureDNS bool
}
