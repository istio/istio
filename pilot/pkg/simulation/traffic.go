package simulation

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"

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
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/test"
	"istio.io/pkg/log"
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

type Expect struct {
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

func (r Result) Matches(want Result) error {
	diff := cmp.Diff(want, r, cmpopts.IgnoreUnexported(Result{}), cmpopts.EquateErrors())
	if want.Error != nil && want.Error != r.Error {
		return fmt.Errorf("want info %q got %q. Diff: %s", want.Error, r.Error, diff)
	}
	if want.ListenerMatched != "" && want.ListenerMatched != r.ListenerMatched {
		return fmt.Errorf("want listener matched %q got %q. Diff: %s", want.ListenerMatched, r.ListenerMatched, diff)
	}
	if want.FilterChainMatched != "" && want.FilterChainMatched != r.FilterChainMatched {
		return fmt.Errorf("want filter chain matched %q got %q. Diff: %s", want.FilterChainMatched, r.FilterChainMatched, diff)
	}
	if want.RouteMatched != "" && want.RouteMatched != r.RouteMatched {
		return fmt.Errorf("want route matched %q got %q. Diff: %s", want.RouteMatched, r.RouteMatched, diff)
	}
	if want.RouteConfigMatched != "" && want.RouteConfigMatched != r.RouteConfigMatched {
		return fmt.Errorf("want route config matched %q got %q. Diff: %s", want.RouteConfigMatched, r.RouteConfigMatched, diff)
	}
	if want.VirtualHostMatched != "" && want.VirtualHostMatched != r.VirtualHostMatched {
		return fmt.Errorf("want virtual host matched %q got %q. Diff: %s", want.VirtualHostMatched, r.VirtualHostMatched, diff)
	}
	if want.ClusterMatched != "" && want.ClusterMatched != r.ClusterMatched {
		return fmt.Errorf("want cluster matched %q got %q. Diff: %s", want.ClusterMatched, r.ClusterMatched, diff)
	}
	return nil
}

type Simulation struct {
	t         test.Failer
	Listeners []*listener.Listener
	Clusters  []*cluster.Cluster
	Routes    []*route.RouteConfiguration
}

func NewSimulation(t test.Failer, s *v1alpha3.ConfigGenTest, proxy *model.Proxy) *Simulation {
	return &Simulation{
		t:         t,
		Listeners: s.Listeners(proxy),
		Clusters:  s.Clusters(proxy),
		Routes:    s.Routes(proxy),
	}
}

var (
	ErrNoListener          = errors.New("no listener matched")
	ErrNoFilterChain       = errors.New("no filter chains matched")
	ErrNoRoute             = errors.New("no route matched")
	ErrNoVirtualHost       = errors.New("no virtual host matched")
	ErrMultipleFilterChain = errors.New("multiple filter chains matched")
)

func (c Call) FillDefaults() Call {
	if c.Headers == nil {
		c.Headers = http.Header{}
	}
	if c.HostHeader != "" {
		c.Headers["Host"] = []string{c.HostHeader}
	}
	if c.Path == "" {
		c.Path = "/"
	}
	return c
}

func (sim *Simulation) RunExpectations(es []Expect) {
	for i, e := range es {
		res := sim.Run(e.Call)
		if err := res.Matches(e.Result); err != nil {
			sim.t.Fatalf("%d: %v", i, err)
		}
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
	if _, hasHttpInspector := xdstest.ExtractListenerFilters(l)[xdsfilters.HTTPInspector.Name]; hasHttpInspector {
		input.Alpn = protocolToAlpn(input.Protocol)
	}
	fc, err := matchFilterChain(l.FilterChains, input)
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
		vh := matchDomain(rc, input.Headers["Host"][0])
		if vh == nil {
			result.Error = ErrNoVirtualHost
			return
		}
		result.VirtualHostMatched = vh.Name
		r := matchVirtualHost(vh, input)

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

func matchVirtualHost(vh *route.VirtualHost, input Call) *route.Route {
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
				log.Errorf("invalid regex: %v", err)
				continue
			}
			if !r.MatchString(input.Path) {
				continue
			}
		default:
			panic("unknown route path type")
		}

		return r
	}
	return nil
}

func matchDomain(rc *route.RouteConfiguration, host string) *route.VirtualHost {
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
			if len(host) > len(d)-1 && strings.HasSuffix(host, d[1:]) && len(d) > longest {
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
			if len(host) > len(d)-1 && strings.HasPrefix(host, d[:len(d)-1]) && len(d) > longest {
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
func matchFilterChain(chains []*listener.FilterChain, input Call) (*listener.FilterChain, error) {
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
				panic(err.Error())
			}
			if err := ranger.Insert(cidranger.NewBasicRangerEntry(*cidr)); err != nil {
				panic(err.Error())
			}
		}
		f, err := ranger.Contains(net.ParseIP(input.Address))
		if err != nil {
			panic(err.Error())
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

func filter(chains []*listener.FilterChain, empty func(fc *listener.FilterChainMatch) bool, match func(fc *listener.FilterChainMatch) bool) []*listener.FilterChain {
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
