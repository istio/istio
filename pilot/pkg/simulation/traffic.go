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
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/yl2chen/cidranger"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/xds"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/sets"
	istiolog "istio.io/pkg/log"
)

var log = istiolog.RegisterScope("simulation", "", 0)

type Protocol string

const (
	HTTP  Protocol = "http"
	HTTP2 Protocol = "http2"
	TCP   Protocol = "tcp"
)

type TLSMode string

const (
	Plaintext TLSMode = "plaintext"
	TLS       TLSMode = "tls"
	MTLS      TLSMode = "mtls"
)

func (c Call) IsHTTP() bool {
	return httpProtocols.Contains(string(c.Protocol)) && (c.TLS == Plaintext || c.TLS == "")
}

var httpProtocols = sets.New(string(HTTP), string(HTTP2))

var (
	ErrNoListener          = errors.New("no listener matched")
	ErrNoFilterChain       = errors.New("no filter chains matched")
	ErrNoRoute             = errors.New("no route matched")
	ErrTLSRedirect         = errors.New("tls required, sending 301")
	ErrNoVirtualHost       = errors.New("no virtual host matched")
	ErrMultipleFilterChain = errors.New("multiple filter chains matched")
	// ErrProtocolError happens when sending TLS/TCP request to HCM, for example
	ErrProtocolError = errors.New("protocol error")
	ErrTLSError      = errors.New("invalid TLS")
	ErrMTLSError     = errors.New("invalid mTLS")
)

type Expect struct {
	Name   string
	Call   Call
	Result Result
}

type CallMode string

type CustomFilterChainValidation func(filterChain *listener.FilterChain) error

var (
	// CallModeGateway simulate no iptables
	CallModeGateway CallMode = "gateway"
	// CallModeOutbound simulate iptables redirect to 15001
	CallModeOutbound CallMode = "outbound"
	// CallModeInbound simulate iptables redirect to 15006
	CallModeInbound CallMode = "inbound"
)

type Call struct {
	Address string
	Port    int
	Path    string

	// Protocol describes the protocol type. TLS encapsulation is separate
	Protocol Protocol
	// TLS describes the connection tls parameters
	// TODO: currently this does not verify TLS vs mTLS
	TLS  TLSMode
	Alpn string

	// HostHeader is a convenience field for Headers
	HostHeader string
	Headers    http.Header

	Sni string

	// CallMode describes the type of call to make.
	CallMode CallMode

	CustomListenerValidations []CustomFilterChainValidation

	MtlsSecretConfigName string
}

func (c Call) FillDefaults() Call {
	if c.Headers == nil {
		c.Headers = http.Header{}
	}
	if c.HostHeader != "" {
		c.Headers["Host"] = []string{c.HostHeader}
	}
	// For simplicity, set SNI automatically for TLS traffic.
	if c.Sni == "" && (c.TLS == TLS) {
		c.Sni = c.HostHeader
	}
	if c.Path == "" {
		c.Path = "/"
	}
	if c.TLS == "" {
		c.TLS = Plaintext
	}
	if c.Address == "" {
		// pick a random address, assumption is the test does not care
		c.Address = "1.3.3.7"
	}
	if c.TLS == MTLS && c.Alpn == "" {
		c.Alpn = protocolToMTLSAlpn(c.Protocol)
	}
	if c.TLS == TLS && c.Alpn == "" {
		c.Alpn = protocolToTLSAlpn(c.Protocol)
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
	// StrictMatch controls whether we will strictly match the result. If unset, empty fields will
	// be ignored, allowing testing only fields we care about This allows asserting that the result
	// is *exactly* equal, allowing asserting a field is empty
	StrictMatch bool
	// If set, this will mark a test as skipped. Note the result is still checked first - we skip only
	// if we pass the test. This is to ensure that if the behavior changes, we still capture it; the skip
	// just ensures we notice a test is wrong
	Skip string
	t    test.Failer
}

func (r Result) Matches(t *testing.T, want Result) {
	t.Helper()
	r.StrictMatch = want.StrictMatch // to make diff pass
	r.Skip = want.Skip               // to make diff pass
	diff := cmp.Diff(want, r, cmpopts.IgnoreUnexported(Result{}), cmpopts.EquateErrors())
	if want.StrictMatch && diff != "" {
		t.Errorf("Diff: %v", diff)
		return
	}
	if want.Error != r.Error {
		t.Errorf("want error %v got %v", want.Error, r.Error)
	}
	if want.ListenerMatched != "" && want.ListenerMatched != r.ListenerMatched {
		t.Errorf("want listener matched %q got %q", want.ListenerMatched, r.ListenerMatched)
	} else {
		// Populate each field in case we did not care about it. This avoids confusing errors when we have fields
		// we don't care about in the test that are present in the result.
		want.ListenerMatched = r.ListenerMatched
	}
	if want.FilterChainMatched != "" && want.FilterChainMatched != r.FilterChainMatched {
		t.Errorf("want filter chain matched %q got %q", want.FilterChainMatched, r.FilterChainMatched)
	} else {
		want.FilterChainMatched = r.FilterChainMatched
	}
	if want.RouteMatched != "" && want.RouteMatched != r.RouteMatched {
		t.Errorf("want route matched %q got %q", want.RouteMatched, r.RouteMatched)
	} else {
		want.RouteMatched = r.RouteMatched
	}
	if want.RouteConfigMatched != "" && want.RouteConfigMatched != r.RouteConfigMatched {
		t.Errorf("want route config matched %q got %q", want.RouteConfigMatched, r.RouteConfigMatched)
	} else {
		want.RouteConfigMatched = r.RouteConfigMatched
	}
	if want.VirtualHostMatched != "" && want.VirtualHostMatched != r.VirtualHostMatched {
		t.Errorf("want virtual host matched %q got %q", want.VirtualHostMatched, r.VirtualHostMatched)
	} else {
		want.VirtualHostMatched = r.VirtualHostMatched
	}
	if want.ClusterMatched != "" && want.ClusterMatched != r.ClusterMatched {
		t.Errorf("want cluster matched %q got %q", want.ClusterMatched, r.ClusterMatched)
	} else {
		want.ClusterMatched = r.ClusterMatched
	}
	if t.Failed() {
		t.Logf("Diff: %+v", diff)
		t.Logf("Full Diff: %+v", cmp.Diff(want, r, cmpopts.IgnoreUnexported(Result{}), cmpopts.EquateErrors()))
	} else if want.Skip != "" {
		t.Skipf("Known bug: %v", r.Skip)
	}
}

type Simulation struct {
	t         *testing.T
	Listeners []*listener.Listener
	Clusters  []*cluster.Cluster
	Routes    []*route.RouteConfiguration
}

func NewSimulationFromConfigGen(t *testing.T, s *v1alpha3.ConfigGenTest, proxy *model.Proxy) *Simulation {
	l := s.Listeners(proxy)
	sim := &Simulation{
		t:         t,
		Listeners: l,
		Clusters:  s.Clusters(proxy),
		Routes:    s.RoutesFromListeners(proxy, l),
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

func hasFilterOnPort(l *listener.Listener, filter string, port int) bool {
	got, f := xdstest.ExtractListenerFilters(l)[filter]
	if !f {
		return false
	}
	if got.FilterDisabled == nil {
		return true
	}
	return !xdstest.EvaluateListenerFilterPredicates(got.FilterDisabled, port)
}

func (sim *Simulation) Run(input Call) (result Result) {
	result = Result{t: sim.t}
	input = input.FillDefaults()
	if input.Alpn != "" && input.TLS == Plaintext {
		result.Error = fmt.Errorf("invalid call, ALPN can only be sent in TLS requests")
		return result
	}

	// First we will match a listener
	l := matchListener(sim.Listeners, input)
	if l == nil {
		result.Error = ErrNoListener
		return
	}
	result.ListenerMatched = l.Name

	hasTLSInspector := hasFilterOnPort(l, xdsfilters.TLSInspector.Name, input.Port)
	if !hasTLSInspector {
		// Without tls inspector, Envoy would not read the ALPN in the TLS handshake
		// HTTP inspector still may set it though
		input.Alpn = ""
	}

	// Apply listener filters
	if hasFilterOnPort(l, xdsfilters.HTTPInspector.Name, input.Port) {
		if alpn := protocolToAlpn(input.Protocol); alpn != "" && input.TLS == Plaintext {
			input.Alpn = alpn
		}
	}

	fc, err := sim.matchFilterChain(l.FilterChains, l.DefaultFilterChain, input, hasTLSInspector)
	if err != nil {
		result.Error = err
		return
	}
	result.FilterChainMatched = fc.Name
	// Plaintext to TLS is an error
	if fc.TransportSocket != nil && input.TLS == Plaintext {
		result.Error = ErrTLSError
		return
	}

	mTLSSecretConfigName := "default"
	if input.MtlsSecretConfigName != "" {
		mTLSSecretConfigName = input.MtlsSecretConfigName
	}

	// mTLS listener will only accept mTLS traffic
	if fc.TransportSocket != nil && sim.requiresMTLS(fc, mTLSSecretConfigName) != (input.TLS == MTLS) {
		// If there is no tls inspector, then
		result.Error = ErrMTLSError
		return
	}

	if len(input.CustomListenerValidations) > 0 {
		for _, validation := range input.CustomListenerValidations {
			if err := validation(fc); err != nil {
				result.Error = err
			}
		}
	}

	if hcm := xdstest.ExtractHTTPConnectionManager(sim.t, fc); hcm != nil {
		// We matched HCM and didn't terminate TLS, but we are sending TLS traffic - decoding will fail
		if input.TLS != Plaintext && fc.TransportSocket == nil {
			result.Error = ErrProtocolError
			return
		}
		// TCP to HCM is invalid
		if input.Protocol != HTTP && input.Protocol != HTTP2 {
			result.Error = ErrProtocolError
			return
		}

		// Fetch inline route
		rc := hcm.GetRouteConfig()
		if rc == nil {
			// If not set, fallback to RDS
			routeName := hcm.GetRds().RouteConfigName
			result.RouteConfigMatched = routeName
			rc = xdstest.ExtractRouteConfigurations(sim.Routes)[routeName]
		}
		hostHeader := ""
		if len(input.Headers["Host"]) > 0 {
			hostHeader = input.Headers["Host"][0]
		}
		vh := sim.matchVirtualHost(rc, hostHeader)
		if vh == nil {
			result.Error = ErrNoVirtualHost
			return
		}
		result.VirtualHostMatched = vh.Name
		if vh.RequireTls == route.VirtualHost_ALL && input.TLS == Plaintext {
			result.Error = ErrTLSRedirect
			return
		}

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

func (sim *Simulation) requiresMTLS(fc *listener.FilterChain, mTLSSecretConfigName string) bool {
	if fc.TransportSocket == nil {
		return false
	}
	t := &tls.DownstreamTlsContext{}
	if err := fc.GetTransportSocket().GetTypedConfig().UnmarshalTo(t); err != nil {
		sim.t.Fatal(err)
	}

	if len(t.GetCommonTlsContext().GetTlsCertificateSdsSecretConfigs()) == 0 {
		return false
	}
	// This is a lazy heuristic, we could check for explicit default resource or spiffe if it becomes necessary
	if t.GetCommonTlsContext().GetTlsCertificateSdsSecretConfigs()[0].Name != mTLSSecretConfigName {
		return false
	}
	if !t.RequireClientCertificate.Value {
		return false
	}
	return true
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
				sim.t.Fatalf("invalid regex %v: %v", pt.SafeRegex.GetRegex(), err)
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
	if rc.GetIgnorePortInHostMatching() {
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
	}
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
func (sim *Simulation) matchFilterChain(chains []*listener.FilterChain, defaultChain *listener.FilterChain,
	input Call, hasTLSInspector bool,
) (*listener.FilterChain, error) {
	chains = filter("DestinationPort", chains, func(fc *listener.FilterChainMatch) bool {
		return fc.GetDestinationPort() == nil
	}, func(fc *listener.FilterChainMatch) bool {
		return int(fc.GetDestinationPort().GetValue()) == input.Port
	})
	chains = filter("PrefixRanges", chains, func(fc *listener.FilterChainMatch) bool {
		return fc.GetPrefixRanges() == nil
	}, func(fc *listener.FilterChainMatch) bool {
		ranger := cidranger.NewPCTrieRanger()
		for _, a := range fc.GetPrefixRanges() {
			s := fmt.Sprintf("%s/%d", a.AddressPrefix, a.GetPrefixLen().GetValue())
			_, cidr, err := net.ParseCIDR(s)
			if err != nil {
				sim.t.Fatalf("failed to parse cidr %v: %v", s, err)
			}
			if err := ranger.Insert(cidranger.NewBasicRangerEntry(*cidr)); err != nil {
				sim.t.Fatalf("failed to insert cidr %v: %v", cidr, err)
			}
		}
		f, err := ranger.Contains(net.ParseIP(input.Address))
		if err != nil {
			sim.t.Fatalf("cidr containers %v failed: %v", input.Address, err)
		}
		return f
	})
	chains = filter("ServerNames", chains, func(fc *listener.FilterChainMatch) bool {
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
	chains = filter("TransportProtocol", chains, func(fc *listener.FilterChainMatch) bool {
		return fc.GetTransportProtocol() == ""
	}, func(fc *listener.FilterChainMatch) bool {
		if !hasTLSInspector {
			// Without tls inspector, transport protocol will always be raw buffer
			return fc.GetTransportProtocol() == xdsfilters.RawBufferTransportProtocol
		}
		switch fc.GetTransportProtocol() {
		case xdsfilters.TLSTransportProtocol:
			return input.TLS == TLS || input.TLS == MTLS
		case xdsfilters.RawBufferTransportProtocol:
			return input.TLS == Plaintext
		}
		return false
	})
	chains = filter("ApplicationProtocols", chains, func(fc *listener.FilterChainMatch) bool {
		return fc.GetApplicationProtocols() == nil
	}, func(fc *listener.FilterChainMatch) bool {
		return sets.New(fc.GetApplicationProtocols()...).Contains(input.Alpn)
	})
	// We do not implement the "source" based filters as we do not use them
	if len(chains) > 1 {
		for _, c := range chains {
			log.Warnf("Matched chain %v", c.Name)
		}
		return nil, ErrMultipleFilterChain
	}
	if len(chains) == 0 {
		if defaultChain != nil {
			return defaultChain, nil
		}
		return nil, ErrNoFilterChain
	}
	return chains[0], nil
}

func filter(desc string, chains []*listener.FilterChain,
	empty func(fc *listener.FilterChainMatch) bool,
	match func(fc *listener.FilterChainMatch) bool,
) []*listener.FilterChain {
	res := []*listener.FilterChain{}
	anySet := false
	for _, c := range chains {
		if !empty(c.GetFilterChainMatch()) {
			anySet = true
			break
		}
	}
	if !anySet {
		log.Debugf("%v: none set, skipping", desc)
		return chains
	}
	for i, c := range chains {
		if match(c.GetFilterChainMatch()) {
			log.Debugf("%v: matched chain %v/%v", desc, i, c.GetName())
			res = append(res, c)
		}
	}
	// Return all matching filter chains
	if len(res) > 0 {
		return res
	}
	// Unless there were no matches - in which case we return all filter chains that did not have a
	// match set
	for i, c := range chains {
		if empty(c.GetFilterChainMatch()) {
			log.Debugf("%v: no matches, found empty chain match %v/%v", desc, i, c.GetName())
			res = append(res, c)
		}
	}
	return res
}

func protocolToMTLSAlpn(s Protocol) string {
	switch s {
	case HTTP:
		return "istio-http/1.1"
	case HTTP2:
		return "istio-h2"
	default:
		return "istio"
	}
}

func protocolToTLSAlpn(s Protocol) string {
	switch s {
	case HTTP:
		return "http/1.1"
	case HTTP2:
		return "h2"
	default:
		return ""
	}
}

func protocolToAlpn(s Protocol) string {
	switch s {
	case HTTP:
		return "http/1.1"
	case HTTP2:
		return "h2c"
	default:
		return ""
	}
}

func matchListener(listeners []*listener.Listener, input Call) *listener.Listener {
	if input.CallMode == CallModeInbound {
		return xdstest.ExtractListener(model.VirtualInboundListenerName, listeners)
	}
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

	// Fallback to the outbound listener
	// TODO - support inbound
	for _, l := range listeners {
		if l.Name == model.VirtualOutboundListenerName {
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
