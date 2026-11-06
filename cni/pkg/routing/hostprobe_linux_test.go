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
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"

	// Run netlink operations in an isolated network namespace.
	_ "github.com/howardjohn/unshare-go/netns"
	"github.com/howardjohn/unshare-go/userns"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"istio.io/istio/pkg/test/util/assert"
)

var initialized sync.Once

func setup(t *testing.T) {
	t.Helper()
	initialized.Do(func() {
		assert.NoError(t, userns.WriteGroupMap(map[uint32]uint32{userns.OriginalGID(): 0}))
	})
}

type probeRouteFamily struct {
	name, ip, otherIP string
	family            int
}

func testProbeRouteFamilies(t *testing.T, test func(*testing.T, probeRouteFamily)) {
	t.Helper()
	for _, family := range []probeRouteFamily{
		{"ipv4", "169.254.240.1", "169.254.240.2", unix.AF_INET},
		{"ipv6", "fd16::1", "fd16::2", unix.AF_INET6},
	} {
		t.Run(family.name, func(t *testing.T) { test(t, family) })
	}
}

func configureProbeRoute(dir, ip string) (*HostProbeRoutes, error) {
	address := netip.MustParseAddr(ip)
	if address.Is6() {
		return ConfigureHostProbeRoutes(dir, netip.MustParseAddr("169.254.240.254"), address, true, true)
	}
	return ConfigureHostProbeRoutes(dir, address, netip.Addr{}, true, false)
}

func startProbeRoute(t *testing.T, dir, ip string) (*HostProbeRoutes, netlink.Route) {
	t.Helper()
	p, err := configureProbeRoute(dir, ip)
	assert.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, p.Close(true)) })
	return p, newHostProbeRoute(netip.MustParseAddr(ip), p.linkIndex)
}

func requireProbeRoutes(t *testing.T, route netlink.Route, count int) {
	t.Helper()
	routes, err := probeRoutes(&route)
	assert.NoError(t, err)
	assert.Equal(t, len(routes), count)
}

func requireCleanupMarker(t *testing.T, dir string, want bool) {
	t.Helper()
	required, err := HostProbeRoutesRequired(dir, false)
	assert.NoError(t, err)
	assert.Equal(t, required, want)
}

func TestHostProbeRoutesRequired(t *testing.T) {
	for _, tt := range []struct {
		name    string
		enabled bool
		marker  bool
		want    bool
	}{
		{name: "disabled"},
		{name: "enabled", enabled: true, want: true},
		{name: "cleanup", marker: true, want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.marker {
				assert.NoError(t, os.WriteFile(filepath.Join(dir, hostProbeRoutesStateFile), nil, 0o600))
			}
			required, err := HostProbeRoutesRequired(dir, tt.enabled)
			assert.NoError(t, err)
			assert.Equal(t, required, tt.want)
			files, err := os.ReadDir(dir)
			assert.NoError(t, err)
			wantFiles := 0
			if tt.marker {
				wantFiles = 1
			}
			assert.Equal(t, len(files), wantFiles)
		})
	}
	for _, dir := range []string{"", filepath.Join(t.TempDir(), "absent")} {
		required, err := HostProbeRoutesRequired(dir, false)
		assert.NoError(t, err)
		assert.Equal(t, required, false)
	}
	t.Run("marker check error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "not-a-directory")
		assert.NoError(t, os.WriteFile(path, nil, 0o600))
		_, err := HostProbeRoutesRequired(path, false)
		assert.Error(t, err)
	})
	t.Run("invalid marker", func(t *testing.T) {
		dir := t.TempDir()
		assert.NoError(t, os.Mkdir(filepath.Join(dir, hostProbeRoutesStateFile), 0o700))
		_, err := HostProbeRoutesRequired(dir, false)
		assert.Error(t, err)
		_, err = configureProbeRoute(dir, "169.254.240.1")
		assert.Error(t, err)
		info, err := os.Stat(filepath.Join(dir, hostProbeRoutesStateFile))
		assert.NoError(t, err)
		assert.Equal(t, info.IsDir(), true)
	})
}

func TestHostProbeRouteLifecycle(t *testing.T) {
	testProbeRouteFamilies(t, func(t *testing.T, family probeRouteFamily) {
		setup(t)
		dir := t.TempDir()
		p, route := startProbeRoute(t, dir, family.ip)
		requireProbeRoutes(t, route, 1)
		actual, err := probeRoutes(&route)
		assert.NoError(t, err)
		assert.Equal(t, actual[0].Equal(route), true)
		routes, err := netlink.RouteGet(route.Dst.IP)
		assert.NoError(t, err)
		assert.Equal(t, routes[0].Type, unix.RTN_LOCAL)
		requireCleanupMarker(t, dir, true)

		_, err = configureProbeRoute(dir, family.ip)
		assert.Error(t, err)
		requireProbeRoutes(t, route, 1)
		requireCleanupMarker(t, dir, true)

		assert.NoError(t, p.Close(false))
		next, _ := startProbeRoute(t, dir, family.ip)
		assert.Equal(t, len(next.added), 0)
		// An old, already-closed handle cannot remove a replacement's routes.
		assert.NoError(t, p.Close(true))
		requireProbeRoutes(t, route, 1)
		assert.NoError(t, next.Rollback())
		requireProbeRoutes(t, route, 1)
		requireCleanupMarker(t, dir, true)
		last, _ := startProbeRoute(t, dir, family.ip)
		assert.NoError(t, last.Close(true))
		requireProbeRoutes(t, route, 0)
		requireCleanupMarker(t, dir, false)
	})
}

func TestHostProbeRouteDisableAndAddressChange(t *testing.T) {
	testProbeRouteFamilies(t, func(t *testing.T, family probeRouteFamily) {
		setup(t)
		dir := t.TempDir()
		p, route := startProbeRoute(t, dir, family.ip)
		assert.NoError(t, p.Close(false))

		next, changed := startProbeRoute(t, dir, family.otherIP)
		requireProbeRoutes(t, route, 0)
		requireProbeRoutes(t, changed, 1)
		assert.NoError(t, next.Close(false))

		disabled, err := ConfigureHostProbeRoutes(dir, netip.Addr{}, netip.Addr{}, false, false)
		assert.NoError(t, err)
		assert.Equal(t, disabled == nil, true)
		requireProbeRoutes(t, changed, 0)
		requireCleanupMarker(t, dir, false)
	})
}

func TestHostProbeRouteForeignRoutes(t *testing.T) {
	testProbeRouteFamilies(t, func(t *testing.T, family probeRouteFamily) {
		setup(t)
		lo, err := netlink.LinkByName("lo")
		assert.NoError(t, err)
		for _, conflicting := range []bool{false, true} {
			name := "equivalent"
			if conflicting {
				name = "conflicting"
			}
			t.Run(name, func(t *testing.T) {
				route := newHostProbeRoute(netip.MustParseAddr(family.ip), lo.Attrs().Index)
				route.Protocol = unix.RTPROT_STATIC
				if conflicting {
					route.Type = unix.RTN_UNICAST
					route.Scope = netlink.SCOPE_LINK
				}
				assert.NoError(t, netlink.RouteAdd(&route))
				t.Cleanup(func() { assert.NoError(t, netlink.RouteDel(&route)) })
				dir := t.TempDir()
				_, err := configureProbeRoute(dir, family.ip)
				assert.Error(t, err)
				requireProbeRoutes(t, route, 1)
				requireCleanupMarker(t, dir, false)
			})
		}
	})
}

func TestHostProbeRouteProtocolOwnership(t *testing.T) {
	testProbeRouteFamilies(t, func(t *testing.T, family probeRouteFamily) {
		setup(t)
		lo, err := netlink.LinkByName("lo")
		assert.NoError(t, err)
		route := newHostProbeRoute(netip.MustParseAddr(family.ip), lo.Attrs().Index)
		route.Priority = 123
		assert.NoError(t, netlink.RouteAdd(&route))
		p, _ := startProbeRoute(t, t.TempDir(), family.ip)
		actual, err := probeRoutes(&route)
		assert.NoError(t, err)
		assert.Equal(t, actual[0].Priority, 123)
		assert.NoError(t, p.Rollback())
		requireProbeRoutes(t, route, 1)
		next, _ := startProbeRoute(t, filepath.Dir(p.statePath), family.ip)
		assert.NoError(t, next.Close(true))
		requireProbeRoutes(t, route, 0)
	})
}

func TestHostProbeRouteChangedByOperator(t *testing.T) {
	testProbeRouteFamilies(t, func(t *testing.T, family probeRouteFamily) {
		setup(t)
		p, route := startProbeRoute(t, t.TempDir(), family.ip)
		assert.NoError(t, netlink.RouteDel(&route))
		route.Protocol = unix.RTPROT_STATIC
		assert.NoError(t, netlink.RouteAdd(&route))
		t.Cleanup(func() { assert.NoError(t, netlink.RouteDel(&route)) })
		assert.NoError(t, p.Close(true))
		requireProbeRoutes(t, route, 1)
	})
}

func TestHostProbeRouteMetricNotOwnership(t *testing.T) {
	testProbeRouteFamilies(t, func(t *testing.T, family probeRouteFamily) {
		setup(t)
		p, route := startProbeRoute(t, t.TempDir(), family.ip)
		assert.NoError(t, netlink.RouteDel(&route))
		route.Priority++
		assert.NoError(t, netlink.RouteAdd(&route))
		assert.NoError(t, p.Close(true))
		requireProbeRoutes(t, route, 0)
	})
}

func TestHostProbeRouteInterruptedCreation(t *testing.T) {
	testProbeRouteFamilies(t, func(t *testing.T, family probeRouteFamily) {
		setup(t)
		dir := t.TempDir()
		// Simulate termination after writing cleanup intent, before RouteAdd.
		assert.NoError(t, os.WriteFile(filepath.Join(dir, hostProbeRoutesStateFile), nil, 0o600))
		p, route := startProbeRoute(t, dir, family.ip)
		assert.Equal(t, len(p.added) > 0, true)
		requireProbeRoutes(t, route, 1)
		assert.NoError(t, p.Rollback())
		requireProbeRoutes(t, route, 0)
		requireCleanupMarker(t, dir, false)
	})
}

func TestHostProbeRouteRestartAfterDeletion(t *testing.T) {
	testProbeRouteFamilies(t, func(t *testing.T, family probeRouteFamily) {
		setup(t)
		dir := t.TempDir()
		p, route := startProbeRoute(t, dir, family.ip)
		assert.NoError(t, p.Close(false))
		assert.NoError(t, netlink.RouteDel(&route))
		next, _ := startProbeRoute(t, dir, family.ip)
		assert.Equal(t, len(next.added), 1)
		requireProbeRoutes(t, route, 1)
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
				ipv4, ipv6 := netip.MustParseAddr(ip), netip.Addr{}
				if family.family == unix.AF_INET6 {
					ipv4, ipv6 = netip.MustParseAddr("169.254.240.1"), netip.MustParseAddr(ip)
				}
				_, err := ConfigureHostProbeRoutes(dir, ipv4, ipv6, true, family.family == unix.AF_INET6)
				assert.Error(t, err)
				files, err := os.ReadDir(dir)
				assert.NoError(t, err)
				assert.Equal(t, len(files), 0)
			})
		}
	})
	t.Run("disabled", func(t *testing.T) {
		dir := t.TempDir()
		p, err := ConfigureHostProbeRoutes(dir, netip.Addr{}, netip.Addr{}, false, false)
		assert.NoError(t, err)
		assert.Equal(t, p == nil, true)
		files, err := os.ReadDir(dir)
		assert.NoError(t, err)
		assert.Equal(t, len(files), 0)
	})
}

func TestHostProbeRoutesDualStackLifecycle(t *testing.T) {
	setup(t)
	dir := t.TempDir()
	ipv4, ipv6 := netip.MustParseAddr("169.254.240.1"), netip.MustParseAddr("fd16::1")
	routes, err := ConfigureHostProbeRoutes(dir, ipv4, ipv6, true, true)
	assert.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, routes.Close(true)) })
	assert.Equal(t, len(routes.added), 2)
	v4, v6 := newHostProbeRoute(ipv4, routes.linkIndex), newHostProbeRoute(ipv6, routes.linkIndex)
	assert.NoError(t, routes.Close(false))

	v4Only, err := ConfigureHostProbeRoutes(dir, ipv4, ipv6, true, false)
	assert.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, v4Only.Close(true)) })
	assert.Equal(t, len(v4Only.added), 0)
	requireProbeRoutes(t, v4, 1)
	requireProbeRoutes(t, v6, 0)
	assert.NoError(t, v4Only.Close(false))

	both, err := ConfigureHostProbeRoutes(dir, ipv4, ipv6, true, true)
	assert.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, both.Close(true)) })
	assert.Equal(t, len(both.added), 1)
	assert.Equal(t, both.added[0].Family, unix.AF_INET6)
	assert.NoError(t, both.Rollback())
	requireProbeRoutes(t, v4, 1)
	requireProbeRoutes(t, v6, 0)
	requireCleanupMarker(t, dir, true)

	last, err := ConfigureHostProbeRoutes(dir, ipv4, ipv6, true, true)
	assert.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, last.Close(true)) })
	assert.NoError(t, last.Close(false))
	disabled, err := ConfigureHostProbeRoutes(dir, ipv4, ipv6, false, false)
	assert.NoError(t, err)
	assert.Equal(t, disabled == nil, true)
	requireProbeRoutes(t, v4, 0)
	requireProbeRoutes(t, v6, 0)
	requireCleanupMarker(t, dir, false)
}

func TestHostProbeRoutesSharedLock(t *testing.T) {
	setup(t)
	dir := t.TempDir()
	p, v4 := startProbeRoute(t, dir, "169.254.240.1")
	_, err := ConfigureHostProbeRoutes(dir, netip.MustParseAddr("169.254.240.2"), netip.MustParseAddr("fd16::1"), true, true)
	assert.Error(t, err)
	_, err = ConfigureHostProbeRoutes(dir, netip.Addr{}, netip.Addr{}, false, false)
	assert.Error(t, err)
	requireProbeRoutes(t, v4, 1)
	requireCleanupMarker(t, dir, true)
	files, err := os.ReadDir(dir)
	assert.NoError(t, err)
	assert.Equal(t, len(files), 2)
	assert.NoError(t, p.Close(true))
	files, err = os.ReadDir(dir)
	assert.NoError(t, err)
	assert.Equal(t, len(files), 1)
	assert.Equal(t, files[0].Name(), hostProbeRoutesStateFile+".lock")
	next, _ := startProbeRoute(t, dir, "169.254.240.2")
	assert.NoError(t, next.Close(true))
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
			v4 := newHostProbeRoute(ipv4, lo.Attrs().Index)
			if inherited {
				old, _ := startProbeRoute(t, dir, ipv4.String())
				assert.NoError(t, old.Close(false))
			}
			foreign := newHostProbeRoute(ipv6, lo.Attrs().Index)
			foreign.Protocol = unix.RTPROT_STATIC
			assert.NoError(t, netlink.RouteAdd(&foreign))
			_, err = ConfigureHostProbeRoutes(dir, ipv4, ipv6, true, true)
			assert.Error(t, err)
			want := 0
			if inherited {
				want = 1
			}
			requireProbeRoutes(t, v4, want)
			requireProbeRoutes(t, foreign, 1)
			requireCleanupMarker(t, dir, inherited)
			assert.NoError(t, netlink.RouteDel(&foreign))
			retry, err := ConfigureHostProbeRoutes(dir, ipv4, ipv6, true, true)
			assert.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, retry.Close(true)) })
		})
	}
}

func TestHostProbeRoutesCleanupScope(t *testing.T) {
	setup(t)
	lo, err := netlink.LinkByName("lo")
	assert.NoError(t, err)
	dummy := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "probe-other"}}
	assert.NoError(t, netlink.LinkAdd(dummy))
	t.Cleanup(func() { assert.NoError(t, netlink.LinkDel(dummy)) })
	for _, tt := range []struct {
		name   string
		change func(*netlink.Route)
	}{
		{"foreign protocol", func(r *netlink.Route) { r.Protocol = unix.RTPROT_STATIC }},
		{"other table", func(r *netlink.Route) { r.Table = 100 }},
		{"other interface", func(r *netlink.Route) { r.LinkIndex = dummy.Attrs().Index }},
		{"non-probe address", func(r *netlink.Route) { r.Dst.IP = net.ParseIP("10.244.240.1").To4() }},
		{"non-exact prefix", func(r *netlink.Route) { r.Dst.Mask = net.CIDRMask(24, 32) }},
		{"non-local route", func(r *netlink.Route) { r.Type, r.Scope = unix.RTN_UNICAST, netlink.SCOPE_LINK }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			other := newHostProbeRoute(netip.MustParseAddr("169.254.250.0"), lo.Attrs().Index)
			tt.change(&other)
			assert.NoError(t, netlink.RouteAdd(&other))
			t.Cleanup(func() { assert.NoError(t, netlink.RouteDel(&other)) })
			dir := t.TempDir()
			p, route := startProbeRoute(t, dir, "169.254.240.1")
			assert.NoError(t, p.Close(false))
			_, err := ConfigureHostProbeRoutes(dir, netip.Addr{}, netip.Addr{}, false, false)
			assert.NoError(t, err)
			requireProbeRoutes(t, route, 0)
			actual, err := netlink.RouteListFiltered(other.Family, &other, netlink.RT_FILTER_TABLE|netlink.RT_FILTER_DST)
			assert.NoError(t, err)
			assert.Equal(t, len(actual), 1)
			requireCleanupMarker(t, dir, false)
		})
	}
}
