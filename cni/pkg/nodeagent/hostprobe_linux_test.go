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

func startProbeRoute(t *testing.T, dir, ip string) *hostProbeRoute {
	t.Helper()
	p, err := configureHostProbeRoute(dir, netip.MustParseAddr(ip), true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { assert.NoError(t, p.close(true)) })
	return p
}

func requireProbeRoutes(t *testing.T, route *netlink.Route, count int) {
	t.Helper()
	routes, err := probeRoutes(route)
	assert.NoError(t, err)
	assert.Equal(t, len(routes), count)
}

func TestHostProbeRouteLifecycle(t *testing.T) {
	setup(t)
	dir := t.TempDir()
	p := startProbeRoute(t, dir, "169.254.240.1")
	assert.Equal(t, p.added, true)
	requireProbeRoutes(t, p.route, 1)
	routes, err := netlink.RouteGet(p.route.Dst.IP)
	assert.NoError(t, err)
	assert.Equal(t, routes[0].Type, unix.RTN_LOCAL)

	// A second agent must not take ownership while the first can still clean up.
	_, err = configureHostProbeRoute(dir, netip.MustParseAddr("169.254.240.1"), true)
	assert.Error(t, err)
	requireProbeRoutes(t, p.route, 1)

	assert.NoError(t, p.close(false))
	requireProbeRoutes(t, p.route, 1)
	next := startProbeRoute(t, dir, "169.254.240.1")
	assert.Equal(t, next.added, false)
	assert.Equal(t, next.route.Priority, p.route.Priority)

	// A failed replacement startup must preserve an inherited route.
	assert.NoError(t, next.close(next.added))
	requireProbeRoutes(t, p.route, 1)
	last := startProbeRoute(t, dir, "169.254.240.1")
	assert.NoError(t, last.close(true))
	requireProbeRoutes(t, p.route, 0)
	_, err = os.Stat(last.statePath)
	assert.Equal(t, os.IsNotExist(err), true)
}

func TestHostProbeRouteDisableAndAddressChange(t *testing.T) {
	setup(t)
	dir := t.TempDir()
	p := startProbeRoute(t, dir, "169.254.240.2")
	assert.NoError(t, p.close(false))

	next := startProbeRoute(t, dir, "169.254.240.3")
	requireProbeRoutes(t, p.route, 0)
	requireProbeRoutes(t, next.route, 1)
	assert.NoError(t, next.close(false))

	disabled, err := configureHostProbeRoute(dir, netip.Addr{}, false)
	assert.NoError(t, err)
	assert.Equal(t, disabled == nil, true)
	requireProbeRoutes(t, next.route, 0)
}

func TestHostProbeRouteForeignRoutes(t *testing.T) {
	setup(t)
	lo, err := netlink.LinkByName("lo")
	assert.NoError(t, err)
	for _, tt := range []struct {
		name     string
		ip       string
		conflict bool
	}{
		{name: "equivalent", ip: "169.254.240.4"},
		{name: "conflicting", ip: "169.254.240.5", conflict: true},
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
			p, err := configureHostProbeRoute(t.TempDir(), netip.MustParseAddr(tt.ip), true)
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
}

func TestHostProbeRouteChangedByOperator(t *testing.T) {
	setup(t)
	dir := t.TempDir()
	p := startProbeRoute(t, dir, "169.254.240.6")
	assert.NoError(t, netlink.RouteDel(p.route))
	foreign := *p.route
	foreign.Priority++
	assert.NoError(t, netlink.RouteAdd(&foreign))
	t.Cleanup(func() { assert.NoError(t, netlink.RouteDel(&foreign)) })
	assert.NoError(t, p.close(true))
	requireProbeRoutes(t, &foreign, 1)
}

func TestHostProbeRouteInterruptedCreation(t *testing.T) {
	setup(t)
	dir := t.TempDir()
	lo, err := netlink.LinkByName("lo")
	assert.NoError(t, err)
	state := hostProbeRouteState{
		Address: netip.MustParseAddr("169.254.240.7"), LinkIndex: lo.Attrs().Index, Priority: 123,
	}
	data, err := json.Marshal(state)
	assert.NoError(t, err)
	// Simulate termination after writing intent, before RouteAdd.
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "host-probe-route.json"), data, 0o600))
	p := startProbeRoute(t, dir, state.Address.String())
	assert.Equal(t, p.added, true)
	requireProbeRoutes(t, p.route, 1)
	assert.NoError(t, p.close(p.added))
	requireProbeRoutes(t, p.route, 0)
}

func TestHostProbeRouteRestartAfterDeletion(t *testing.T) {
	setup(t)
	dir := t.TempDir()
	p := startProbeRoute(t, dir, "169.254.240.9")
	assert.NoError(t, p.close(false))
	// Routes can disappear on node reboot while the ownership record survives.
	assert.NoError(t, netlink.RouteDel(p.route))
	next := startProbeRoute(t, dir, "169.254.240.9")
	assert.Equal(t, next.added, true)
	requireProbeRoutes(t, next.route, 1)
}

func TestHostProbeRoutePendingStateDoesNotAdoptForeignRoute(t *testing.T) {
	setup(t)
	dir := t.TempDir()
	p := startProbeRoute(t, dir, "169.254.240.10")
	assert.NoError(t, p.close(false))
	assert.NoError(t, netlink.RouteDel(p.route))
	foreign := *p.route
	foreign.Priority++
	assert.NoError(t, netlink.RouteAdd(&foreign))
	t.Cleanup(func() { assert.NoError(t, netlink.RouteDel(&foreign)) })
	next := startProbeRoute(t, dir, "169.254.240.10")
	assert.Equal(t, next.route == nil, true)
	assert.NoError(t, next.close(true))
	requireProbeRoutes(t, &foreign, 1)
}

func TestHostProbeRouteInvalidInputs(t *testing.T) {
	for _, ip := range []string{"0.0.0.0", "127.0.0.6", "10.0.0.1", "224.0.0.1", "fd16::1", "::ffff:169.254.7.127"} {
		t.Run(ip, func(t *testing.T) {
			dir := t.TempDir()
			_, err := configureHostProbeRoute(dir, netip.MustParseAddr(ip), true)
			assert.Error(t, err)
			files, err := os.ReadDir(dir)
			assert.NoError(t, err)
			assert.Equal(t, len(files), 0)
		})
	}
	t.Run("disabled", func(t *testing.T) {
		dir := t.TempDir()
		p, err := configureHostProbeRoute(dir, netip.Addr{}, false)
		assert.NoError(t, err)
		assert.Equal(t, p == nil, true)
		files, err := os.ReadDir(dir)
		assert.NoError(t, err)
		assert.Equal(t, len(files), 0)
	})
	t.Run("corrupt ownership", func(t *testing.T) {
		dir := t.TempDir()
		assert.NoError(t, os.WriteFile(filepath.Join(dir, "host-probe-route.json"), []byte("{"), 0o600))
		_, err := configureHostProbeRoute(dir, netip.MustParseAddr("169.254.240.8"), true)
		assert.Error(t, err)
	})
}
