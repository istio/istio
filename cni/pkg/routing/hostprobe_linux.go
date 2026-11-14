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

package routing

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"istio.io/istio/cni/pkg/scopes"
	"istio.io/istio/pkg/file"
)

var log = scopes.CNIAgent

const (
	// Locally assigned, not globally reserved. Only exact local probe routes on
	// loopback with this protocol are Istio-managed; see cni/README.md.
	hostProbeRouteProtocol   = 196
	hostProbeRoutesStateFile = "host-probe-routes"
)

// HostProbeRoutes holds one host-local lock for both address families until shutdown.
type HostProbeRoutes struct {
	lock      *os.File
	statePath string
	linkIndex int
	added     []netlink.Route
}

// HostProbeRoutesRequired checks whether routes need configuration or cleanup.
// It may be called outside the host network namespace.
func HostProbeRoutesRequired(runDir string, enabled bool) (bool, error) {
	if enabled {
		return true, nil
	}
	if runDir == "" {
		return false, nil
	}
	if info, err := os.Stat(filepath.Join(runDir, hostProbeRoutesStateFile)); err == nil {
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("host probe route cleanup marker is not a regular file")
		}
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("check host probe route cleanup marker: %w", err)
	}
	return false, nil
}

// ConfigureHostProbeRoutes configures probe routes in the current network namespace.
// The caller must enter the host network namespace before calling it.
func ConfigureHostProbeRoutes(runDir string, ipv4, ipv6 netip.Addr, enabled, enableIPv6 bool) (_ *HostProbeRoutes, retErr error) {
	required, err := HostProbeRoutesRequired(runDir, enabled)
	if err != nil || !required {
		return nil, err
	}
	if runDir == "" {
		return nil, fmt.Errorf("host probe routes require a CNI agent run directory")
	}
	if enabled && (!validHostProbeAddress(ipv4, unix.AF_INET) ||
		(enableIPv6 && !validHostProbeAddress(ipv6, unix.AF_INET6))) {
		return nil, fmt.Errorf("local host probe routing requires an IPv4 link-local and enabled IPv6 unique-local address, got %s and %s", ipv4, ipv6)
	}
	statePath := filepath.Join(runDir, hostProbeRoutesStateFile)
	lock, err := os.OpenFile(statePath+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, errors.Join(fmt.Errorf("acquire host probe route lock (another node agent may still be running): %w", err), lock.Close())
	}
	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return nil, errors.Join(err, lock.Close())
	}
	p := &HostProbeRoutes{lock: lock, statePath: statePath, linkIndex: lo.Attrs().Index}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, p.Rollback())
		}
	}()
	if !enabled {
		return nil, p.Close(true)
	}

	// Persist cleanup intent before touching routes, including after a process crash.
	if err := file.AtomicWrite(statePath, nil, 0o600); err != nil {
		return nil, errors.Join(err, p.release())
	}
	var desired []netlink.Route
	for _, cfg := range []struct {
		address netip.Addr
		enabled bool
	}{
		{ipv4, true},
		{ipv6, enableIPv6},
	} {
		if !cfg.enabled {
			continue
		}
		route := newHostProbeRoute(cfg.address, p.linkIndex)
		desired = append(desired, route)
		existing, err := probeRoutes(&route)
		if err != nil {
			return nil, err
		}
		for _, current := range existing {
			if !isHostProbeRoute(current, p.linkIndex) {
				return nil, fmt.Errorf("conflicting route for host probe address %s (expected Istio protocol %d): %s",
					cfg.address, hostProbeRouteProtocol, current.String())
			}
		}
		if len(existing) == 0 {
			if err := netlink.RouteAdd(&route); err != nil {
				return nil, fmt.Errorf("add local host probe route: %w", err)
			}
			p.added = append(p.added, route)
			log.Infof("added local host probe route for %s", cfg.address)
		}
	}
	owned, err := ownedHostProbeRoutes(p.linkIndex)
	if err != nil {
		return nil, err
	}
	for _, route := range owned {
		keep := false
		for _, want := range desired {
			if route.Dst.String() == want.Dst.String() {
				keep = true
				break
			}
		}
		if !keep {
			if err := removeHostProbeRoute(route); err != nil {
				return nil, err
			}
		}
	}
	return p, nil
}

// Close releases the shared ownership lock, optionally removing owned routes.
// The caller must enter the host network namespace when removing routes.
func (p *HostProbeRoutes) Close(remove bool) error {
	if remove {
		return p.close(false)
	}
	return p.release()
}

// Rollback removes newly added routes but preserves inherited routes, then releases
// the shared lock. The caller must enter the host network namespace.
func (p *HostProbeRoutes) Rollback() error {
	return p.close(true)
}

func (p *HostProbeRoutes) close(onlyAdded bool) (retErr error) {
	if p == nil || p.lock == nil {
		return nil
	}
	defer func() { retErr = errors.Join(retErr, p.release()) }()
	routes, err := ownedHostProbeRoutes(p.linkIndex)
	if err != nil {
		return err
	}
	remaining := len(routes)
	for _, route := range routes {
		remove := !onlyAdded
		for _, added := range p.added {
			if added.Equal(route) {
				remove = true
				break
			}
		}
		if remove {
			if err := removeHostProbeRoute(route); err != nil {
				retErr = errors.Join(retErr, err)
			} else {
				remaining--
			}
		}
	}
	if remaining == 0 {
		if err := os.Remove(p.statePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			retErr = errors.Join(retErr, err)
		}
	}
	return retErr
}

func (p *HostProbeRoutes) release() error {
	if p == nil || p.lock == nil {
		return nil
	}
	err := p.lock.Close()
	p.lock = nil
	return err
}

func validHostProbeAddress(address netip.Addr, family int) bool {
	switch family {
	case unix.AF_INET:
		return address.Is4() && address.IsLinkLocalUnicast()
	case unix.AF_INET6:
		return address.Is6() && !address.Is4In6() && address.IsPrivate() && address.Zone() == ""
	default:
		return false
	}
}

// The manual experiments used these commands in the host network namespace:
//
//	ip route add local 169.254.7.127/32 dev lo table local
//	ip -6 route add local fd16:9254:7127:1337:ffff:ffff:ffff:ffff/128 dev lo table local proto static metric 774913
//
// Managed routes use the configured probe addresses, proto 196, and metric 1.
func newHostProbeRoute(address netip.Addr, linkIndex int) netlink.Route {
	family, scope := unix.AF_INET, netlink.SCOPE_HOST
	if address.Is6() {
		// Linux reports IPv6 local routes with universe scope, unlike IPv4.
		family, scope = unix.AF_INET6, netlink.SCOPE_UNIVERSE
	}
	bits := address.BitLen()
	return netlink.Route{
		Dst:       &net.IPNet{IP: net.IP(address.AsSlice()), Mask: net.CIDRMask(bits, bits)},
		Family:    family,
		Table:     unix.RT_TABLE_LOCAL,
		Type:      unix.RTN_LOCAL,
		Scope:     scope,
		Protocol:  hostProbeRouteProtocol,
		LinkIndex: linkIndex,
		Priority:  1,
	}
}

func isHostProbeRoute(route netlink.Route, linkIndex int) bool {
	if route.Dst == nil {
		return false
	}
	address, ok := netip.AddrFromSlice(route.Dst.IP)
	if !ok {
		return false
	}
	address = address.Unmap()
	ones, bits := route.Dst.Mask.Size()
	if !validHostProbeAddress(address, route.Family) || ones != address.BitLen() || bits != address.BitLen() {
		return false
	}
	want := newHostProbeRoute(address, linkIndex)
	return route.Table == want.Table && route.Type == want.Type && route.Scope == want.Scope &&
		route.Protocol == want.Protocol && route.LinkIndex == want.LinkIndex && len(route.Gw) == 0 && len(route.MultiPath) == 0
}

func ownedHostProbeRoutes(linkIndex int) ([]netlink.Route, error) {
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_ALL, &netlink.Route{
		Table: unix.RT_TABLE_LOCAL, Protocol: hostProbeRouteProtocol,
	}, netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PROTOCOL)
	if err != nil {
		return nil, err
	}
	var owned []netlink.Route
	for _, route := range routes {
		if isHostProbeRoute(route, linkIndex) {
			owned = append(owned, route)
		}
	}
	return owned, nil
}

func probeRoutes(route *netlink.Route) ([]netlink.Route, error) {
	return netlink.RouteListFiltered(route.Family, route, netlink.RT_FILTER_TABLE|netlink.RT_FILTER_DST)
}

func removeHostProbeRoute(route netlink.Route) error {
	if err := netlink.RouteDel(&route); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("remove local host probe route: %w", err)
	}
	return nil
}
