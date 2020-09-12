package simulation

import (
	"fmt"
	"net/http"
	"strings"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/google/go-cmp/cmp"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pilot/pkg/xds"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pilot/test/xdstest"
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

type Call struct {
	Address  string
	Port     int
	Path     string
	Protocol Protocol
	Alpn     string
	Headers  http.Header
	Sni      string
}

type Result struct {
	Info               string
	ListenerMatched    string
	FilterChainMatched string
	RouteMatched       string
	RouteConfigMatched string
	VirtualHostMatched string
	ClusterMatched     string
}

func (r Result) Matches(t test.Failer, want Result) {
	t.Helper()
	diff := cmp.Diff(want, r)
	if want.Info != "" && want.Info != r.Info {
		t.Fatalf("want info %q got %q. Diff: %s", want.Info, r.Info, diff)
	}
	if want.ListenerMatched != "" && want.ListenerMatched != r.ListenerMatched {
		t.Fatalf("want listener matched %q got %q. Diff: %s", want.ListenerMatched, r.ListenerMatched, diff)
	}
	if want.FilterChainMatched != "" && want.FilterChainMatched != r.FilterChainMatched {
		t.Fatalf("want filter chain matched %q got %q. Diff: %s", want.FilterChainMatched, r.FilterChainMatched, diff)
	}
	if want.RouteMatched != "" && want.RouteMatched != r.RouteMatched {
		t.Fatalf("want route matched %q got %q. Diff: %s", want.RouteMatched, r.RouteMatched, diff)
	}
	if want.RouteConfigMatched != "" && want.RouteConfigMatched != r.RouteConfigMatched {
		t.Fatalf("want route config matched %q got %q. Diff: %s", want.RouteConfigMatched, r.RouteConfigMatched, diff)
	}
	if want.VirtualHostMatched != "" && want.VirtualHostMatched != r.VirtualHostMatched {
		t.Fatalf("want virtual host matched %q got %q. Diff: %s", want.VirtualHostMatched, r.VirtualHostMatched, diff)
	}
	if want.ClusterMatched != "" && want.ClusterMatched != r.ClusterMatched {
		t.Fatalf("want cluster matched %q got %q. Diff: %s", want.ClusterMatched, r.ClusterMatched, diff)
	}
}

func RunSimulation(t test.Failer, s *xds.FakeDiscoveryServer, proxy *model.Proxy, input Call) (result Result) {
	listeners := s.Listeners(proxy)
	l := matchListener(listeners, input)
	if _, hasHttpInspector := xdstest.ExtractListenerFilters(l)[xdsfilters.HTTPInspector.Name]; hasHttpInspector {
		input.Alpn = protocolToAlpn(input.Protocol)
	}
	if input.Path == "" {
		input.Path = "/"
	}
	if l == nil {
		result.Info = "no listener matches"
		return
	}
	result.ListenerMatched = l.Name
	fc, err := matchFilterChain(l.FilterChains, input)
	if err != nil {
		result.Info = err.Error()
		return
	}
	result.FilterChainMatched = fc.Name
	hcm := xdstest.ExtractHTTPConnectionManager(t, fc)
	if hcm != nil {
		routeName := hcm.GetRds().RouteConfigName
		result.RouteConfigMatched = routeName
		rc := xdstest.ExtractRouteConfigurations(s.Routes(proxy))[routeName]
		if len(input.Headers["Host"]) != 1 {
			result.Info = "must have exactly 1 host for http calls"
			return
		}
		vh := matchDomain(rc, input.Headers["Host"][0])
		if vh == nil {
			result.Info = "no virtual host match"
			return
		}
		result.VirtualHostMatched = vh.Name
		r := matchVirtualHost(vh, input)

		if r == nil {
			result.Info = "no route match"
			return
		}
		result.RouteMatched = r.Name
		switch t := r.GetAction().(type) {
		case *route.Route_Route:
			result.ClusterMatched = t.Route.GetCluster()
		}
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
			// TODO
			continue
		default:
			continue
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

func matchFilterChain(chains []*listener.FilterChain, input Call) (*listener.FilterChain, error) {
	chains = filter(chains, func(fc *listener.FilterChainMatch) bool {
		return fc.GetDestinationPort() == nil
	}, func(fc *listener.FilterChainMatch) bool {
		return int(fc.GetDestinationPort().GetValue()) == input.Port
	})
	chains = filter(chains, func(fc *listener.FilterChainMatch) bool {
		return fc.GetPrefixRanges() == nil
	}, func(fc *listener.FilterChainMatch) bool {
		// TODO implement
		return true
	})
	chains = filter(chains, func(fc *listener.FilterChainMatch) bool {
		return fc.GetServerNames() == nil
	}, func(fc *listener.FilterChainMatch) bool {
		// TODO implement
		return true
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
	if len(chains) > 1 {
		return nil, fmt.Errorf("multiple matching filter chains")
	}
	if len(chains) == 0 {
		return nil, fmt.Errorf("no matching filter chains")
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
