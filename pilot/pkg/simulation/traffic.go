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

package simulation

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/yl2chen/cidranger"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pilot/pkg/xds"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/test"
)

type Protocol string

const (
	HTTP  Protocol = "http"
	HTTP2 Protocol = "http2"
	HTTPS Protocol = "https"
	GRPC  Protocol = "grpc"
	TCP   Protocol = "tcp"
	TLS   Protocol = "tls"
)

var (
	ErrNoListener          = errors.New("no listener matched")
	ErrNoFilterChain       = errors.New("no filter chains matched")
	ErrNoRoute             = errors.New("no route matched")
	ErrNoVirtualHost       = errors.New("no virtual host matched")
	ErrMultipleFilterChain = errors.New("multiple filter chains matched")
)

type Expect struct {
	Name   string
	Call   Call
	Result Result
}

type Call struct {
	Address  string
	Port     int
	Path     string
	Protocol Protocol
	Alpn     string

	// HostHeader is a convenience field for Headers
	HostHeader string
	Headers    http.Header

	Sni string
}

func (c Call) FillDefaults() Call {
	if c.Headers == nil {
		c.Headers = http.Header{}
	}
	if c.HostHeader != "" {
		c.Headers["Host"] = []string{c.HostHeader}
	}
	if c.Sni == "" && c.Protocol == HTTPS {
		c.Sni = c.HostHeader
	}
	if c.Path == "" {
		c.Path = "/"
	}
	return c
}

type Result struct {
	Error              error
	ListenerMatched    string
	FilterChainMatched string
	RouteMatched       string
	RouteConfigMatched string
	VirtualHostMatched string
	ClusterMatched     string
	t                  test.Failer
}

func (r Result) Matches(t *testing.T, want Result) {
	diff := cmp.Diff(want, r, cmpopts.IgnoreUnexported(Result{}), cmpopts.EquateErrors())
	if want.Error != nil && want.Error != r.Error {
		t.Errorf("want error %v got %v", want.Error, r.Error)
	}
	if want.ListenerMatched != "" && want.ListenerMatched != r.ListenerMatched {
		t.Errorf("want listener matched %q got %q", want.ListenerMatched, r.ListenerMatched)
	}
	if want.FilterChainMatched != "" && want.FilterChainMatched != r.FilterChainMatched {
		t.Errorf("want filter chain matched %q got %q", want.FilterChainMatched, r.FilterChainMatched)
	}
	if want.RouteMatched != "" && want.RouteMatched != r.RouteMatched {
		t.Errorf("want route matched %q got %q", want.RouteMatched, r.RouteMatched)
	}
	if want.RouteConfigMatched != "" && want.RouteConfigMatched != r.RouteConfigMatched {
		t.Errorf("want route config matched %q got %q", want.RouteConfigMatched, r.RouteConfigMatched)
	}
	if want.VirtualHostMatched != "" && want.VirtualHostMatched != r.VirtualHostMatched {
		t.Errorf("want virtual host matched %q got %q", want.VirtualHostMatched, r.VirtualHostMatched)
	}
	if want.ClusterMatched != "" && want.ClusterMatched != r.ClusterMatched {
		t.Errorf("want cluster matched %q got %q", want.ClusterMatched, r.ClusterMatched)
	}
	if t.Failed() {
		t.Logf("Diff: %+v", diff)
	}
}

type Simulation struct {
	t         *testing.T
	Listeners []*listener.Listener
	Clusters  []*cluster.Cluster
	Routes    []*route.RouteConfiguration
}

func NewSimulationFromConfigGen(t *testing.T, s *v1alpha3.ConfigGenTest, proxy *model.Proxy) *Simulation {
	sim := &Simulation{
		t:         t,
		Listeners: s.Listeners(proxy),
		Clusters:  s.Clusters(proxy),
		Routes:    s.Routes(proxy),
	}
	return sim
}

func NewSimulation(t *testing.T, s *xds.FakeDiscoveryServer, proxy *model.Proxy) *Simulation {
	return NewSimulationFromConfigGen(t, s.ConfigGenTest, proxy)
}

// withT swaps out the testing struct. This allows executing sub tests.
func (sim *Simulation) withT(t *testing.T) *Simulation {
	cpy := *sim
	cpy.t = t
	return &cpy
}

func (sim *Simulation) RunExpectations(es []Expect) {
	for _, e := range es {
		sim.t.Run(e.Name, func(t *testing.T) {
			sim.withT(t).Run(e.Call).Matches(t, e.Result)
		})
	}
}

func (sim *Simulation) Run(input Call) (result Result) {
	result = Result{t: sim.t}
	input = input.FillDefaults()

	// First we will match a listener
	l := matchListener(sim.Listeners, input)
	if l == nil {
		result.Error = ErrNoListener
		return
	}
	result.ListenerMatched = l.Name

	// Apply listener filters. This will likely need the TLS inspector in the future as well
	if _, f := xdstest.ExtractListenerFilters(l)[xdsfilters.HTTPInspector.Name]; f {
		input.Alpn = protocolToAlpn(input.Protocol)
	}
	fc, err := sim.matchFilterChain(l.FilterChains, input)
	if err != nil {
		result.Error = err
		return
	}
	result.FilterChainMatched = fc.Name

	if hcm := xdstest.ExtractHTTPConnectionManager(sim.t, fc); hcm != nil {
		routeName := hcm.GetRds().RouteConfigName
		result.RouteConfigMatched = routeName
		rc := xdstest.ExtractRouteConfigurations(sim.Routes)[routeName]
		if len(input.Headers["Host"]) != 1 {
			result.Error = errors.New("http requests require a host header")
			return
		}
		vh := sim.matchVirtualHost(rc, input.Headers["Host"][0])
		if vh == nil {
			result.Error = ErrNoVirtualHost
			return
		}
		result.VirtualHostMatched = vh.Name
		r := sim.matchRoute(vh, input)

		if r == nil {
			result.Error = ErrNoRoute
			return
		}
		result.RouteMatched = r.Name
		switch t := r.GetAction().(type) {
		case *route.Route_Route:
			result.ClusterMatched = t.Route.GetCluster()
		}
	} else if tcp := xdstest.ExtractTCPProxy(sim.t, fc); tcp != nil {
		result.ClusterMatched = tcp.GetCluster()
	}
	return
}

func (sim *Simulation) matchRoute(vh *route.VirtualHost, input Call) *route.Route {
	for _, r := range vh.Routes {
		// check path
		switch pt := r.Match.GetPathSpecifier().(type) {
		case *route.RouteMatch_Prefix:
			if !strings.HasPrefix(input.Path, pt.Prefix) {
				continue
			}
		case *route.RouteMatch_Path:
			if input.Path != pt.Path {
				continue
			}
		case *route.RouteMatch_SafeRegex:
			r, err := regexp.Compile(pt.SafeRegex.GetRegex())
			if err != nil {
				sim.t.Fatalf("invalid regex %v: %v", r, err)
			}
			if !r.MatchString(input.Path) {
				continue
			}
		default:
			sim.t.Fatalf("unknown route path type")
		}

		// TODO this only handles path - we need to add headers, query params, etc to be complete.

		return r
	}
	return nil
}

func (sim *Simulation) matchVirtualHost(rc *route.RouteConfiguration, host string) *route.VirtualHost {
	// Exact match
	for _, vh := range rc.VirtualHosts {
		for _, d := range vh.Domains {
			if d == host {
				return vh
			}
		}
	}
	// prefix match
	var bestMatch *route.VirtualHost
	longest := 0
	for _, vh := range rc.VirtualHosts {
		for _, d := range vh.Domains {
			if d[0] != '*' {
				continue
			}
			if len(host) >= len(d) && strings.HasSuffix(host, d[1:]) && len(d) > longest {
				bestMatch = vh
				longest = len(d)
			}
		}
	}
	if bestMatch != nil {
		return bestMatch
	}
	// Suffix match
	longest = 0
	for _, vh := range rc.VirtualHosts {
		for _, d := range vh.Domains {
			if d[len(d)-1] != '*' {
				continue
			}
			if len(host) >= len(d) && strings.HasPrefix(host, d[:len(d)-1]) && len(d) > longest {
				bestMatch = vh
				longest = len(d)
			}
		}
	}
	if bestMatch != nil {
		return bestMatch
	}
	// wildcard match
	for _, vh := range rc.VirtualHosts {
		for _, d := range vh.Domains {
			if d == "*" {
				return vh
			}
		}
	}
	return nil
}

// Follow the 8 step Sieve as in
// https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/listener/v3/listener_components.proto.html#config-listener-v3-filterchainmatch
// The implementation may initially be confusing because of a property of the
// Envoy algorithm - at each level we will filter out all FilterChains that do
// not match. This means an empty match (`{}`) may not match if another chain
// matches one criteria but not another.
func (sim *Simulation) matchFilterChain(chains []*listener.FilterChain, input Call) (*listener.FilterChain, error) {
	chains = filter(chains, func(fc *listener.FilterChainMatch) bool {
		return fc.GetDestinationPort() == nil
	}, func(fc *listener.FilterChainMatch) bool {
		return int(fc.GetDestinationPort().GetValue()) == input.Port
	})
	chains = filter(chains, func(fc *listener.FilterChainMatch) bool {
		return fc.GetPrefixRanges() == nil
	}, func(fc *listener.FilterChainMatch) bool {
		ranger := cidranger.NewPCTrieRanger()
		for _, a := range fc.GetPrefixRanges() {
			_, cidr, err := net.ParseCIDR(fmt.Sprintf("%s/%d", a.AddressPrefix, a.GetPrefixLen().GetValue()))
			if err != nil {
				sim.t.Fatal(err)
			}
			if err := ranger.Insert(cidranger.NewBasicRangerEntry(*cidr)); err != nil {
				sim.t.Fatal(err)
			}
		}
		f, err := ranger.Contains(net.ParseIP(input.Address))
		if err != nil {
			sim.t.Fatal(err)
		}
		return f
	})
	chains = filter(chains, func(fc *listener.FilterChainMatch) bool {
		return fc.GetServerNames() == nil
	}, func(fc *listener.FilterChainMatch) bool {
		sni := host.Name(input.Sni)
		for _, s := range fc.GetServerNames() {
			if sni.SubsetOf(host.Name(s)) {
				return true
			}
		}
		return false
	})
	chains = filter(chains, func(fc *listener.FilterChainMatch) bool {
		return fc.GetTransportProtocol() == ""
	}, func(fc *listener.FilterChainMatch) bool {
		switch fc.GetTransportProtocol() {
		case xdsfilters.TLSTransportProtocol:
			return input.Protocol == HTTPS || input.Protocol == TLS
		case xdsfilters.RawBufferTransportProtocol:
			return input.Protocol != HTTPS && input.Protocol != TLS
		}
		return false
	})
	chains = filter(chains, func(fc *listener.FilterChainMatch) bool {
		return fc.GetApplicationProtocols() == nil
	}, func(fc *listener.FilterChainMatch) bool {
		return sets.NewSet(fc.GetApplicationProtocols()...).Contains(input.Alpn)
	})
	// We do not implement the "source" based filters as we do not use them
	if len(chains) > 1 {
		return nil, ErrMultipleFilterChain
	}
	if len(chains) == 0 {
		return nil, ErrNoFilterChain
	}
	return chains[0], nil
}

func filter(chains []*listener.FilterChain,
	empty func(fc *listener.FilterChainMatch) bool,
	match func(fc *listener.FilterChainMatch) bool) []*listener.FilterChain {
	res := []*listener.FilterChain{}
	anySet := false
	for _, c := range chains {
		if !empty(c.GetFilterChainMatch()) {
			anySet = true
		}
	}
	if !anySet {
		return chains
	}
	for _, c := range chains {
		if match(c.GetFilterChainMatch()) {
			res = append(res, c)
		}
	}
	return res
}

func protocolToAlpn(s Protocol) string {
	switch s {
	case HTTP:
		return "http/1.1"
	case HTTP2:
		return "http/2"
	case GRPC:
		return "http/2"
	default:
		return ""
	}
}

func matchListener(listeners []*listener.Listener, input Call) *listener.Listener {
	// First find exact match for the IP/Port, then fallback to wildcard IP/Port
	// There is no wildcard port
	for _, l := range listeners {
		if matchAddress(l.GetAddress(), input.Address, input.Port) {
			return l
		}
	}
	for _, l := range listeners {
		if matchAddress(l.GetAddress(), "0.0.0.0", input.Port) {
			return l
		}
	}
	return nil
}

func matchAddress(a *core.Address, address string, port int) bool {
	if a.GetSocketAddress().GetAddress() != address {
		return false
	}
	if int(a.GetSocketAddress().GetPortValue()) != port {
		return false
	}
	return true
}
