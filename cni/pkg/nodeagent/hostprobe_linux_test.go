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

package nodeagent

import (
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"istio.io/istio/pkg/test/util/assert"
)

type probeRouteFamily struct {
	name, ip, otherIP, stateFile string
	family                       int
}

func testProbeRouteFamilies(t *testing.T, test func(*testing.T, probeRouteFamily)) {
	t.Helper()
	for _, family := range []probeRouteFamily{
		{"ipv4", "169.254.240.1", "169.254.240.2", "host-probe-route.json", unix.AF_INET},
		{"ipv6", "fd16::1", "fd16::2", "host-probe-route-v6.json", unix.AF_INET6},
	} {
		t.Run(family.name, func(t *testing.T) { test(t, family) })
	}
}

func startProbeRoute(t *testing.T, dir, ip string) *hostProbeRoute {
	t.Helper()
	address := netip.MustParseAddr(ip)
	family := unix.AF_INET
	if address.Is6() {
		family = unix.AF_INET6
	}
	p, err := configureHostProbeRoute(dir, address, true, family)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { assert.NoError(t, p.close(true)) })
	if p.route != nil {
		assert.Equal(t, p.route.Priority > 0 && p.route.Priority <= 1<<30, true)
	}
	return p
}

func requireProbeRoutes(t *testing.T, route *netlink.Route, count int) {
	t.Helper()
	routes, err := probeRoutes(route)
	assert.NoError(t, err)
	assert.Equal(t, len(routes), count)
}

func TestHostProbeRouteLifecycle(t *testing.T) {
	testProbeRouteFamilies(t, func(t *testing.T, family probeRouteFamily) {
		setup(t)
		dir := t.TempDir()
		p := startProbeRoute(t, dir, family.ip)
		assert.Equal(t, p.added, true)
		requireProbeRoutes(t, p.route, 1)
		actual, err := probeRoutes(p.route)
		assert.NoError(t, err)
		assert.Equal(t, actual[0].Equal(*p.route), true)
		routes, err := netlink.RouteGet(p.route.Dst.IP)
		assert.NoError(t, err)
		assert.Equal(t, routes[0].Type, unix.RTN_LOCAL)

		// A second agent must not take ownership while the first can still clean up.
		_, err = configureHostProbeRoute(dir, netip.MustParseAddr(family.ip), true, family.family)
		assert.Error(t, err)
		requireProbeRoutes(t, p.route, 1)

		assert.NoError(t, p.close(false))
		requireProbeRoutes(t, p.route, 1)
		next := startProbeRoute(t, dir, family.ip)
		assert.Equal(t, next.added, false)
		assert.Equal(t, next.route.Priority, p.route.Priority)

		// A failed replacement startup must preserve an inherited route.
		assert.NoError(t, next.close(next.added))
		requireProbeRoutes(t, p.route, 1)
		last := startProbeRoute(t, dir, family.ip)
		assert.NoError(t, last.close(true))
		requireProbeRoutes(t, p.route, 0)
		_, err = os.Stat(last.statePath)
		assert.Equal(t, os.IsNotExist(err), true)
	})
}

func TestHostProbeRouteDisableAndAddressChange(t *testing.T) {
	testProbeRouteFamilies(t, func(t *testing.T, family probeRouteFamily) {
		setup(t)
		dir := t.TempDir()
		p := startProbeRoute(t, dir, family.ip)
		assert.NoError(t, p.close(false))

		next := startProbeRoute(t, dir, family.otherIP)
		requireProbeRoutes(t, p.route, 0)
		requireProbeRoutes(t, next.route, 1)
		assert.NoError(t, next.close(false))

		disabled, err := configureHostProbeRoute(dir, netip.Addr{}, false, family.family)
		assert.NoError(t, err)
		assert.Equal(t, disabled == nil, true)
		requireProbeRoutes(t, next.route, 0)
	})
}

func TestHostProbeRouteForeignRoutes(t *testing.T) {
	testProbeRouteFamilies(t, func(t *testing.T, family probeRouteFamily) {
		setup(t)
		lo, err := netlink.LinkByName("lo")
		assert.NoError(t, err)
		for _, tt := range []struct {
			name     string
			ip       string
			conflict bool
		}{
			{name: "equivalent", ip: family.ip},
			{name: "conflicting", ip: family.otherIP, conflict: true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				route, err := (hostProbeRouteState{
					Address: netip.MustParseAddr(tt.ip), LinkIndex: lo.Attrs().Index, Priority: 123,
				}).route()
				assert.NoError(t, err)
				if tt.conflict {
					route.Type = unix.RTN_UNICAST
					route.Scope = netlink.SCOPE_LINK
				}
				assert.NoError(t, netlink.RouteAdd(route))
				t.Cleanup(func() { assert.NoError(t, netlink.RouteDel(route)) })
				p, err := configureHostProbeRoute(t.TempDir(), netip.MustParseAddr(tt.ip), true, family.family)
				if tt.conflict {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
					assert.Equal(t, p.route == nil, true)
					assert.NoError(t, p.close(true))
				}
				requireProbeRoutes(t, route, 1)
			})
		}
	})
}

func TestHostProbeRouteChangedByOperator(t *testing.T) {
	testProbeRouteFamilies(t, func(t *testing.T, family probeRouteFamily) {
		setup(t)
		dir := t.TempDir()
		p := startProbeRoute(t, dir, family.ip)
		assert.NoError(t, netlink.RouteDel(p.route))
		foreign := *p.route
		foreign.Priority++
		assert.NoError(t, netlink.RouteAdd(&foreign))
		t.Cleanup(func() { assert.NoError(t, netlink.RouteDel(&foreign)) })
		assert.NoError(t, p.close(true))
		requireProbeRoutes(t, &foreign, 1)
	})
}

func TestHostProbeRouteInterruptedCreation(t *testing.T) {
	testProbeRouteFamilies(t, func(t *testing.T, family probeRouteFamily) {
		setup(t)
		dir := t.TempDir()
		lo, err := netlink.LinkByName("lo")
		assert.NoError(t, err)
		state := hostProbeRouteState{
			Address: netip.MustParseAddr(family.ip), LinkIndex: lo.Attrs().Index, Priority: 123,
		}
		data, err := json.Marshal(state)
		assert.NoError(t, err)
		// Simulate termination after writing intent, before RouteAdd.
		assert.NoError(t, os.WriteFile(filepath.Join(dir, family.stateFile), data, 0o600))
		p := startProbeRoute(t, dir, state.Address.String())
		assert.Equal(t, p.added, true)
		requireProbeRoutes(t, p.route, 1)
		assert.NoError(t, p.close(p.added))
		requireProbeRoutes(t, p.route, 0)
	})
}

func TestHostProbeRouteRestartAfterDeletion(t *testing.T) {
	testProbeRouteFamilies(t, func(t *testing.T, family probeRouteFamily) {
		setup(t)
		dir := t.TempDir()
		p := startProbeRoute(t, dir, family.ip)
		assert.NoError(t, p.close(false))
		// Routes can disappear on node reboot while the ownership record survives.
		assert.NoError(t, netlink.RouteDel(p.route))
		next := startProbeRoute(t, dir, family.ip)
		assert.Equal(t, next.added, true)
		requireProbeRoutes(t, next.route, 1)
	})
}

func TestHostProbeRoutePendingStateDoesNotAdoptForeignRoute(t *testing.T) {
	testProbeRouteFamilies(t, func(t *testing.T, family probeRouteFamily) {
		setup(t)
		dir := t.TempDir()
		p := startProbeRoute(t, dir, family.ip)
		assert.NoError(t, p.close(false))
		assert.NoError(t, netlink.RouteDel(p.route))
		foreign := *p.route
		foreign.Priority++
		assert.NoError(t, netlink.RouteAdd(&foreign))
		t.Cleanup(func() { assert.NoError(t, netlink.RouteDel(&foreign)) })
		next := startProbeRoute(t, dir, family.ip)
		assert.Equal(t, next.route == nil, true)
		assert.NoError(t, next.close(true))
		requireProbeRoutes(t, &foreign, 1)
	})
}

func TestHostProbeRouteInvalidInputs(t *testing.T) {
	testProbeRouteFamilies(t, func(t *testing.T, family probeRouteFamily) {
		invalid := []string{"0.0.0.0", "127.0.0.6", "10.0.0.1", "224.0.0.1", "fd16::1", "::ffff:169.254.7.127"}
		if family.family == unix.AF_INET6 {
			invalid = []string{"::", "::1", "ff02::1", "fe80::1", "2001:db8::1", "fd16::1%lo", "::ffff:10.0.0.1", "169.254.7.127"}
		}
		for _, ip := range invalid {
			t.Run(ip, func(t *testing.T) {
				dir := t.TempDir()
				_, err := configureHostProbeRoute(dir, netip.MustParseAddr(ip), true, family.family)
				assert.Error(t, err)
				files, err := os.ReadDir(dir)
				assert.NoError(t, err)
				assert.Equal(t, len(files), 0)
			})
		}
		t.Run("disabled", func(t *testing.T) {
			dir := t.TempDir()
			p, err := configureHostProbeRoute(dir, netip.Addr{}, false, family.family)
			assert.NoError(t, err)
			assert.Equal(t, p == nil, true)
			files, err := os.ReadDir(dir)
			assert.NoError(t, err)
			assert.Equal(t, len(files), 0)
		})
		t.Run("corrupt ownership", func(t *testing.T) {
			dir := t.TempDir()
			assert.NoError(t, os.WriteFile(filepath.Join(dir, family.stateFile), []byte("{"), 0o600))
			_, err := configureHostProbeRoute(dir, netip.MustParseAddr(family.ip), true, family.family)
			assert.Error(t, err)
		})
	})
}

func TestHostProbeRoutesDualStackLifecycle(t *testing.T) {
	setup(t)
	dir := t.TempDir()
	ipv4, ipv6 := netip.MustParseAddr("169.254.240.1"), netip.MustParseAddr("fd16::1")
	routes, err := configureHostProbeRoutes(dir, ipv4, ipv6, true, true)
	assert.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, routes.close(true)) })
	assert.Equal(t, len(routes), 2)
	for _, route := range routes {
		requireProbeRoutes(t, route.route, 1)
	}
	assert.NoError(t, routes.close(false))

	v4Only, err := configureHostProbeRoutes(dir, ipv4, ipv6, true, false)
	assert.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, v4Only.close(true)) })
	assert.Equal(t, len(v4Only), 1)
	assert.Equal(t, v4Only[0].route.Equal(*routes[0].route), true)
	requireProbeRoutes(t, routes[1].route, 0)
	_, err = os.Stat(filepath.Join(dir, "host-probe-route-v6.json"))
	assert.Equal(t, os.IsNotExist(err), true)
	assert.NoError(t, v4Only.close(false))

	both, err := configureHostProbeRoutes(dir, ipv4, ipv6, true, true)
	assert.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, both.close(true)) })
	assert.Equal(t, both[0].added, false)
	assert.Equal(t, both[1].added, true)
	// A later startup failure preserves inherited IPv4 but removes newly added IPv6.
	assert.NoError(t, both.rollback())
	requireProbeRoutes(t, routes[0].route, 1)
	requireProbeRoutes(t, routes[1].route, 0)

	last, err := configureHostProbeRoutes(dir, ipv4, ipv6, true, true)
	assert.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, last.close(true)) })
	assert.NoError(t, last.close(false))
	disabled, err := configureHostProbeRoutes(dir, ipv4, ipv6, false, true)
	assert.NoError(t, err)
	assert.Equal(t, len(disabled), 0)
	for _, route := range last {
		requireProbeRoutes(t, route.route, 0)
	}
}

func TestHostProbeRoutesPartialFailure(t *testing.T) {
	for _, inherited := range []bool{false, true} {
		name := "new-ipv4"
		if inherited {
			name = "inherited-ipv4"
		}
		t.Run(name, func(t *testing.T) {
			setup(t)
			dir := t.TempDir()
			ipv4, ipv6 := netip.MustParseAddr("169.254.240.1"), netip.MustParseAddr("fd16::1")
			lo, err := netlink.LinkByName("lo")
			assert.NoError(t, err)
			v4, err := (hostProbeRouteState{Address: ipv4, LinkIndex: lo.Attrs().Index, Priority: 1}).route()
			assert.NoError(t, err)
			if inherited {
				old := startProbeRoute(t, dir, ipv4.String())
				assert.NoError(t, old.close(false))
				v4 = old.route
			}
			held := startProbeRoute(t, dir, ipv6.String())
			_, err = configureHostProbeRoutes(dir, ipv4, ipv6, true, true)
			assert.Error(t, err)
			if inherited {
				requireProbeRoutes(t, v4, 1)
			} else {
				requireProbeRoutes(t, v4, 0)
			}
			requireProbeRoutes(t, held.route, 1)
			assert.NoError(t, held.close(true))
			// The failed attempt must also release the IPv4 lock.
			retry, err := configureHostProbeRoutes(dir, ipv4, ipv6, true, true)
			assert.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, retry.close(true)) })
			assert.Equal(t, len(retry), 2)
		})
	}
}

func TestHostProbeRouteRejectsWrongFamilyOwnership(t *testing.T) {
	setup(t)
	dir := t.TempDir()
	v4 := startProbeRoute(t, dir, "169.254.240.1")
	data, err := os.ReadFile(v4.statePath)
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "host-probe-route-v6.json"), data, 0o600))
	_, err = configureHostProbeRoute(dir, netip.Addr{}, false, unix.AF_INET6)
	assert.Error(t, err)
	requireProbeRoutes(t, v4.route, 1)
}
