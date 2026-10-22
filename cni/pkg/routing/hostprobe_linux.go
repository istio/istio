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
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
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

// hostProbeRoute holds the host-local lock until shutdown, preventing an old agent
// from deleting a replacement agent's route during an overlapping restart.
type hostProbeRoute struct {
	lock      *os.File
	statePath string
	route     *netlink.Route
	added     bool
}

type hostProbeRouteState struct {
	Address   netip.Addr
	LinkIndex int
	Priority  int
}

// HostProbeRoutes holds the ownership locks for the configured probe routes.
type HostProbeRoutes []*hostProbeRoute

// ConfigureHostProbeRoutes configures probe routes in the current network namespace.
// The caller must enter the host network namespace before calling it.
func ConfigureHostProbeRoutes(runDir string, ipv4, ipv6 netip.Addr, enabled, enableIPv6 bool) (_ HostProbeRoutes, retErr error) {
	var routes HostProbeRoutes
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, routes.Rollback())
		}
	}()
	for _, cfg := range []struct {
		address netip.Addr
		family  int
		enabled bool
	}{
		{ipv4, unix.AF_INET, enabled},
		{ipv6, unix.AF_INET6, enabled && enableIPv6},
	} {
		route, err := configureHostProbeRoute(runDir, cfg.address, cfg.enabled, cfg.family)
		if err != nil {
			return nil, err
		}
		if route != nil {
			routes = append(routes, route)
		}
	}
	return routes, nil
}

// Close releases ownership locks, optionally removing owned routes.
// The caller must enter the host network namespace when removing routes.
func (routes HostProbeRoutes) Close(remove bool) error {
	var err error
	for _, route := range routes {
		err = errors.Join(err, route.close(remove))
	}
	return err
}

// Rollback removes newly added routes but preserves inherited routes, then releases
// ownership locks. The caller must enter the host network namespace.
func (routes HostProbeRoutes) Rollback() error {
	var err error
	for _, route := range routes {
		err = errors.Join(err, route.close(route.added))
	}
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
// Managed routes use the configured probe addresses, proto static, and a persisted
// random metric for ownership instead of the experiment's default or fixed metric.
func (s hostProbeRouteState) route() (*netlink.Route, error) {
	family, scope := unix.AF_INET, netlink.SCOPE_HOST
	if s.Address.Is6() {
		// Linux reports IPv6 local routes with universe scope, unlike IPv4.
		family, scope = unix.AF_INET6, netlink.SCOPE_UNIVERSE
	}
	if !validHostProbeAddress(s.Address, family) || s.LinkIndex <= 0 || s.Priority <= 0 {
		return nil, fmt.Errorf("invalid host probe route state: %+v", s)
	}
	bits := s.Address.BitLen()
	return &netlink.Route{
		Dst:       &net.IPNet{IP: net.IP(s.Address.AsSlice()), Mask: net.CIDRMask(bits, bits)},
		Family:    family,
		Table:     unix.RT_TABLE_LOCAL,
		Type:      unix.RTN_LOCAL,
		Scope:     scope,
		Protocol:  unix.RTPROT_STATIC,
		LinkIndex: s.LinkIndex,
		Priority:  s.Priority,
	}, nil
}

// configureHostProbeRoute must run in the host network namespace.
func configureHostProbeRoute(runDir string, address netip.Addr, enabled bool, family int) (_ *hostProbeRoute, retErr error) {
	stateFile := "host-probe-route.json"
	switch family {
	case unix.AF_INET:
	case unix.AF_INET6:
		stateFile = "host-probe-route-v6.json"
	default:
		return nil, fmt.Errorf("unsupported host probe route family: %d", family)
	}
	if enabled && !validHostProbeAddress(address, family) {
		return nil, fmt.Errorf("local host probe routing requires an IPv4 link-local or IPv6 unique-local address matching family %d, got %s", family, address)
	}
	if runDir == "" {
		return nil, fmt.Errorf("host probe route state requires a CNI agent run directory")
	}
	statePath := filepath.Join(runDir, stateFile)
	if !enabled {
		if _, err := os.Stat(statePath); errors.Is(err, os.ErrNotExist) {
			return nil, nil
		} else if err != nil {
			return nil, err
		}
	}

	lock, err := os.OpenFile(statePath+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	p := &hostProbeRoute{lock: lock, statePath: statePath}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, lock.Close())
		}
	}()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, fmt.Errorf("acquire host probe route lock (another node agent may still be running): %w", err)
	}

	state := hostProbeRouteState{}
	data, err := os.ReadFile(statePath)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, fmt.Errorf("read host probe route ownership: %w", err)
		}
		if !validHostProbeAddress(state.Address, family) {
			return nil, fmt.Errorf("host probe route ownership address %s does not match family %d", state.Address, family)
		}
		p.route, err = state.route()
		if err != nil {
			return nil, err
		}
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	if !enabled {
		if err := p.remove(); err != nil {
			return nil, err
		}
		return nil, lock.Close()
	}

	lo, err := netlink.LinkByName("lo")
	if err != nil {
		return nil, err
	}
	desired, err := (hostProbeRouteState{
		Address: address, LinkIndex: lo.Attrs().Index, Priority: 1,
	}).route()
	if err != nil {
		return nil, err
	}
	routes, err := probeRoutes(desired)
	if err != nil {
		return nil, err
	}
	for _, route := range routes {
		if route.Type != unix.RTN_LOCAL || route.Scope != desired.Scope ||
			route.LinkIndex != desired.LinkIndex || len(route.Gw) != 0 || len(route.MultiPath) != 0 {
			return nil, fmt.Errorf("conflicting route for host probe address %s: %s", address, route.String())
		}
	}
	if p.route != nil && state.Address != address {
		if err := p.remove(); err != nil {
			return nil, err
		}
		p.route = nil
	}
	if len(routes) != 0 {
		for _, route := range routes {
			if p.route != nil && route.Equal(*p.route) {
				return p, nil
			}
		}
		// An equivalent route is usable, but its presence is not proof of ownership.
		p.route = nil
		if err := removeProbeRouteState(statePath); err != nil {
			return nil, err
		}
		log.Infof("using pre-existing local host probe route for %s without taking ownership", address)
		return p, nil
	}

	// Persist a per-route metric before RouteAdd, so a process crash between the
	// two operations is recoverable without adopting an unrelated static route.
	priority, err := rand.Int(rand.Reader, big.NewInt(1<<30))
	if err != nil {
		return nil, fmt.Errorf("generate host probe route priority: %w", err)
	}
	state = hostProbeRouteState{
		Address: address, LinkIndex: lo.Attrs().Index, Priority: int(priority.Int64()) + 1,
	}
	p.route, err = state.route()
	if err != nil {
		return nil, err
	}
	data, err = json.Marshal(state)
	if err != nil {
		return nil, err
	}
	if err := file.AtomicWrite(statePath, data, 0o600); err != nil {
		return nil, err
	}
	if err := netlink.RouteAdd(p.route); err != nil {
		// Do not adopt a route that raced our existence check.
		return nil, errors.Join(fmt.Errorf("add local host probe route: %w", err), removeProbeRouteState(statePath))
	}
	p.added = true
	log.Infof("added local host probe route for %s", address)
	return p, nil
}

func probeRoutes(route *netlink.Route) ([]netlink.Route, error) {
	return netlink.RouteListFiltered(route.Family, route, netlink.RT_FILTER_TABLE|netlink.RT_FILTER_DST)
}

func removeProbeRouteState(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (p *hostProbeRoute) remove() error {
	if p.route == nil {
		return nil
	}
	routes, err := probeRoutes(p.route)
	if err != nil {
		return err
	}
	for _, route := range routes {
		if route.Equal(*p.route) {
			if err := netlink.RouteDel(&route); err != nil && !errors.Is(err, unix.ESRCH) {
				return fmt.Errorf("remove local host probe route: %w", err)
			}
			return removeProbeRouteState(p.statePath)
		}
	}
	log.Warnf("owned host probe route is absent or changed; leaving current routes untouched")
	return removeProbeRouteState(p.statePath)
}

// close must run in the host network namespace when removing a route.
func (p *hostProbeRoute) close(remove bool) error {
	if p == nil || p.lock == nil {
		return nil
	}
	var err error
	if remove {
		err = p.remove()
	}
	err = errors.Join(err, p.lock.Close())
	p.lock = nil
	return err
}
