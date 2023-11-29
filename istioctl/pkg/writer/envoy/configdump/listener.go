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

package configdump

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"text/tabwriter"

	matcher "github.com/cncf/xds/go/xds/type/matcher/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	"sigs.k8s.io/yaml"

	"istio.io/istio/istioctl/pkg/util/proto"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/protoconv"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/wellknown"
)

const (
	// HTTPListener identifies a listener as being of HTTP type by the presence of an HTTP connection manager filter
	HTTPListener = wellknown.HTTPConnectionManager

	// TCPListener identifies a listener as being of TCP type by the presence of TCP proxy filter
	TCPListener = wellknown.TCPProxy

	IPMatcher = "type.googleapis.com/xds.type.matcher.v3.IPMatcher"
)

// ListenerFilter is used to pass filter information into listener based config writer print functions
type ListenerFilter struct {
	Address string
	Port    uint32
	Type    string
	Verbose bool
}

// Verify returns true if the passed listener matches the filter fields
func (l *ListenerFilter) Verify(listener *listener.Listener) bool {
	if l.Address == "" && l.Port == 0 && l.Type == "" {
		return true
	}
	if l.Address != "" {
		addresses := retrieveListenerAdditionalAddresses(listener)
		addresses = append(addresses, retrieveListenerAddress(listener))
		found := false
		for _, address := range addresses {
			if strings.EqualFold(address, l.Address) {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	if l.Port != 0 && retrieveListenerPort(listener) != l.Port {
		return false
	}
	if l.Type != "" && !strings.EqualFold(retrieveListenerType(listener), l.Type) {
		return false
	}
	return true
}

func getFilterChains(l *listener.Listener) []*listener.FilterChain {
	res := l.FilterChains
	if l.DefaultFilterChain != nil {
		res = append(res, l.DefaultFilterChain)
	}
	return res
}

// retrieveListenerType classifies a Listener as HTTP|TCP|HTTP+TCP|UNKNOWN
func retrieveListenerType(l *listener.Listener) string {
	nHTTP := 0
	nTCP := 0
	for _, filterChain := range getFilterChains(l) {
		for _, filter := range filterChain.GetFilters() {
			if filter.Name == HTTPListener {
				nHTTP++
			} else if filter.Name == TCPListener {
				if !strings.Contains(string(filter.GetTypedConfig().GetValue()), util.BlackHoleCluster) {
					nTCP++
				}
			}
		}
	}

	if nHTTP > 0 {
		if nTCP == 0 {
			return "HTTP"
		}
		return "HTTP+TCP"
	} else if nTCP > 0 {
		return "TCP"
	}

	return "UNKNOWN"
}

func retrieveListenerAddress(l *listener.Listener) string {
	sockAddr := l.Address.GetSocketAddress()
	if sockAddr != nil {
		return sockAddr.Address
	}

	pipe := l.Address.GetPipe()
	if pipe != nil {
		return pipe.Path
	}

	return ""
}

func retrieveListenerAdditionalAddresses(l *listener.Listener) []string {
	var addrs []string
	socketAddresses := l.GetAdditionalAddresses()
	for _, socketAddr := range socketAddresses {
		addr := socketAddr.Address
		addrs = append(addrs, addr.GetSocketAddress().Address)
	}

	return addrs
}

func retrieveListenerPort(l *listener.Listener) uint32 {
	return l.Address.GetSocketAddress().GetPortValue()
}

func (c *ConfigWriter) PrintRemoteListenerSummary() error {
	w, listeners, err := c.setupListenerConfigWriter()
	if err != nil {
		return err
	}
	// Sort by port, addr, type
	sort.Slice(listeners, func(i, j int) bool {
		if listeners[i].GetInternalListener() != nil && listeners[j].GetInternalListener() != nil {
			return listeners[i].GetName() < listeners[j].GetName()
		}
		iPort := retrieveListenerPort(listeners[i])
		jPort := retrieveListenerPort(listeners[j])
		if iPort != jPort {
			return iPort < jPort
		}
		iAddr := retrieveListenerAddress(listeners[i])
		jAddr := retrieveListenerAddress(listeners[j])
		if iAddr != jAddr {
			return iAddr < jAddr
		}
		iType := retrieveListenerType(listeners[i])
		jType := retrieveListenerType(listeners[j])
		return iType < jType
	})

	fmt.Fprintln(w, "LISTENER\tCHAIN\tMATCH\tDESTINATION")
	for _, l := range listeners {
		chains := getFilterChains(l)
		lname := "envoy://" + l.GetName()
		// Avoid duplicating the listener and filter name
		if l.GetInternalListener() != nil && len(chains) == 1 && chains[0].GetName() == lname {
			lname = "internal"
		}
		for _, fc := range chains {

			name := fc.GetName()
			matches := newMatcher(fc, l)
			destination := getFilterType(fc.GetFilters())
			for _, match := range matches {
				fmt.Fprintf(w, "%v\t%v\t%v\t%v\n", lname, name, match, destination)
			}
		}
	}
	return w.Flush()
}

func newMatcher(fc *listener.FilterChain, l *listener.Listener) []string {
	if l.FilterChainMatcher == nil {
		return []string{getMatches(fc.GetFilterChainMatch())}
	}
	switch v := l.GetFilterChainMatcher().GetOnNoMatch().GetOnMatch().(type) {
	case *matcher.Matcher_OnMatch_Action:
		if v.Action.GetName() == fc.GetName() {
			return []string{"UNMATCHED"}
		}
	case *matcher.Matcher_OnMatch_Matcher:
		ms, f := recurse(fc.GetName(), v.Matcher)
		if !f {
			return []string{"NONE"}
		}
		return ms
	}
	ms, f := recurse(fc.GetName(), l.GetFilterChainMatcher())
	if !f {
		return []string{"NONE"}
	}
	return ms
}

func recurse(name string, match *matcher.Matcher) ([]string, bool) {
	switch v := match.GetOnNoMatch().GetOnMatch().(type) {
	case *matcher.Matcher_OnMatch_Action:
		if v.Action.GetName() == name {
			// TODO this only makes sense in context of a chain... do we need a way to give it context
			return []string{"ANY"}, true
		}
	case *matcher.Matcher_OnMatch_Matcher:
		ms, f := recurse(name, v.Matcher)
		if !f {
			return []string{"NONE"}, true
		}
		return ms, true
	}
	// TODO support list
	n := match.GetMatcherTree().GetInput().GetName()

	var m map[string]*matcher.Matcher_OnMatch
	equality := "="
	switch v := match.GetMatcherTree().GetTreeType().(type) {
	case *matcher.Matcher_MatcherTree_ExactMatchMap:
		m = v.ExactMatchMap.Map
	case *matcher.Matcher_MatcherTree_PrefixMatchMap:
		m = v.PrefixMatchMap.Map
		equality = "^"
	case *matcher.Matcher_MatcherTree_CustomMatch:
		tc := v.CustomMatch.GetTypedConfig()
		switch tc.TypeUrl {
		case IPMatcher:
			ip := protoconv.SilentlyUnmarshalAny[matcher.IPMatcher](tc)
			m = map[string]*matcher.Matcher_OnMatch{}
			for _, rm := range ip.GetRangeMatchers() {
				for _, r := range rm.Ranges {
					cidr := r.AddressPrefix
					pl := r.PrefixLen.GetValue()
					if pl != 32 && pl != 128 {
						cidr += fmt.Sprintf("/%d", pl)
					}
					m[cidr] = rm.OnMatch
				}
			}
		default:
			panic("unhandled")
		}
	}
	outputs := []string{}
	for k, v := range m {
		switch v := v.GetOnMatch().(type) {
		case *matcher.Matcher_OnMatch_Action:
			if v.Action.GetName() == name {
				outputs = append(outputs, fmt.Sprintf("%v%v%v", n, equality, k))
			}
			continue
		case *matcher.Matcher_OnMatch_Matcher:
			children, match := recurse(name, v.Matcher)
			if !match {
				continue
			}
			for _, child := range children {
				outputs = append(outputs, fmt.Sprintf("%v%v%v -> %v", n, equality, k, child))
			}
		}
	}
	return outputs, len(outputs) > 0
}

// PrintListenerSummary prints a summary of the relevant listeners in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintListenerSummary(filter ListenerFilter) error {
	w, listeners, err := c.setupListenerConfigWriter()
	if err != nil {
		return err
	}

	verifiedListeners := make([]*listener.Listener, 0, len(listeners))
	for _, l := range listeners {
		if filter.Verify(l) {
			verifiedListeners = append(verifiedListeners, l)
		}
	}

	// Sort by port, addr, type
	sort.Slice(verifiedListeners, func(i, j int) bool {
		iPort := retrieveListenerPort(verifiedListeners[i])
		jPort := retrieveListenerPort(verifiedListeners[j])
		if iPort != jPort {
			return iPort < jPort
		}
		iAddr := retrieveListenerAddress(verifiedListeners[i])
		jAddr := retrieveListenerAddress(verifiedListeners[j])
		if iAddr != jAddr {
			return iAddr < jAddr
		}
		iType := retrieveListenerType(verifiedListeners[i])
		jType := retrieveListenerType(verifiedListeners[j])
		return iType < jType
	})

	printStr := "ADDRESSES\tPORT"
	if includeConfigType {
		printStr = "NAME\t" + printStr
	}
	if filter.Verbose {
		printStr += "\tMATCH\tDESTINATION"
	} else {
		printStr += "\tTYPE"
	}
	fmt.Fprintln(w, printStr)
	for _, l := range verifiedListeners {
		addresses := []string{retrieveListenerAddress(l)}
		addresses = append(addresses, retrieveListenerAdditionalAddresses(l)...)
		port := retrieveListenerPort(l)
		if filter.Verbose {

			matches := retrieveListenerMatches(l)
			sort.Slice(matches, func(i, j int) bool {
				return matches[i].destination > matches[j].destination
			})
			for _, match := range matches {
				if includeConfigType {
					name := fmt.Sprintf("listener/%s", l.Name)
					fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%v\n", name, strings.Join(addresses, ","), port, match.match, match.destination)
				} else {
					fmt.Fprintf(w, "%v\t%v\t%v\t%v\n", strings.Join(addresses, ","), port, match.match, match.destination)
				}
			}
		} else {
			listenerType := retrieveListenerType(l)
			if includeConfigType {
				name := fmt.Sprintf("listener/%s", l.Name)
				fmt.Fprintf(w, "%v\t%v\t%v\t%v\n", name, strings.Join(addresses, ","), port, listenerType)
			} else {
				fmt.Fprintf(w, "%v\t%v\t%v\n", strings.Join(addresses, ","), port, listenerType)
			}
		}
	}
	return w.Flush()
}

type filterchain struct {
	match       string
	destination string
}

var (
	plaintextHTTPALPNs = []string{"http/1.0", "http/1.1", "h2c"}
	istioHTTPPlaintext = []string{"istio", "istio-http/1.0", "istio-http/1.1", "istio-h2"}
	httpTLS            = []string{"http/1.0", "http/1.1", "h2c", "istio-http/1.0", "istio-http/1.1", "istio-h2"}
	tcpTLS             = []string{"istio-peer-exchange", "istio"}

	protDescrs = map[string][]string{
		"App: HTTP TLS":         httpTLS,
		"App: Istio HTTP Plain": istioHTTPPlaintext,
		"App: TCP TLS":          tcpTLS,
		"App: HTTP":             plaintextHTTPALPNs,
	}
)

func retrieveListenerMatches(l *listener.Listener) []filterchain {
	fChains := getFilterChains(l)
	resp := make([]filterchain, 0, len(fChains))
	for _, filterChain := range fChains {
		fc := filterchain{
			destination: getFilterType(filterChain.GetFilters()),
			match:       getMatches(filterChain.FilterChainMatch),
		}
		resp = append(resp, fc)
	}
	return resp
}

func getMatches(f *listener.FilterChainMatch) string {
	match := f
	if match == nil {
		match = &listener.FilterChainMatch{}
	}
	// filterChaince also has SuffixLen, SourceType, SourcePrefixRanges which are not rendered.

	descrs := []string{}
	if len(match.ServerNames) > 0 {
		descrs = append(descrs, fmt.Sprintf("SNI: %s", strings.Join(match.ServerNames, ",")))
	}
	if len(match.TransportProtocol) > 0 {
		descrs = append(descrs, fmt.Sprintf("Trans: %s", match.TransportProtocol))
	}

	if len(match.ApplicationProtocols) > 0 {
		found := false
		for protDescr, protocols := range protDescrs {
			if reflect.DeepEqual(match.ApplicationProtocols, protocols) {
				found = true
				descrs = append(descrs, protDescr)
				break
			}
		}
		if !found {
			descrs = append(descrs, fmt.Sprintf("App: %s", strings.Join(match.ApplicationProtocols, ",")))
		}
	}

	port := ""
	if match.DestinationPort != nil {
		port = fmt.Sprintf(":%d", match.DestinationPort.GetValue())
	}
	if len(match.PrefixRanges) > 0 {
		pf := []string{}
		for _, p := range match.PrefixRanges {
			pf = append(pf, fmt.Sprintf("%s/%d", p.AddressPrefix, p.GetPrefixLen().GetValue()))
		}
		descrs = append(descrs, fmt.Sprintf("Addr: %s%s", strings.Join(pf, ","), port))
	} else if port != "" {
		descrs = append(descrs, fmt.Sprintf("Addr: *%s", port))
	}
	if len(descrs) == 0 {
		descrs = []string{"ALL"}
	}
	return strings.Join(descrs, "; ")
}

func getFilterType(filters []*listener.Filter) string {
	for _, filter := range filters {
		if filter.Name == HTTPListener {
			httpProxy := &hcm.HttpConnectionManager{}
			// Allow Unmarshal to work even if Envoy and istioctl are different
			filter.GetTypedConfig().TypeUrl = "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager"
			err := filter.GetTypedConfig().UnmarshalTo(httpProxy)
			if err != nil {
				return err.Error()
			}
			if httpProxy.GetRouteConfig() != nil {
				return describeRouteConfig(httpProxy.GetRouteConfig())
			}
			if httpProxy.GetRds().GetRouteConfigName() != "" {
				return fmt.Sprintf("Route: %s", httpProxy.GetRds().GetRouteConfigName())
			}
			return "HTTP"
		} else if filter.Name == TCPListener {
			if !strings.Contains(string(filter.GetTypedConfig().GetValue()), util.BlackHoleCluster) {
				tcpProxy := &tcp.TcpProxy{}
				// Allow Unmarshal to work even if Envoy and istioctl are different
				filter.GetTypedConfig().TypeUrl = "type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy"
				err := filter.GetTypedConfig().UnmarshalTo(tcpProxy)
				if err != nil {
					return err.Error()
				}
				if strings.Contains(tcpProxy.GetCluster(), "Cluster") {
					return tcpProxy.GetCluster()
				}
				return fmt.Sprintf("Cluster: %s", tcpProxy.GetCluster())
			}
		}
	}
	return "Non-HTTP/Non-TCP"
}

func describeRouteConfig(route *route.RouteConfiguration) string {
	if cluster := getMatchAllCluster(route); cluster != "" {
		return cluster
	}
	vhosts := []string{}
	for _, vh := range route.GetVirtualHosts() {
		if describeDomains(vh) == "" {
			vhosts = append(vhosts, describeRoutes(vh))
		} else {
			vhosts = append(vhosts, fmt.Sprintf("%s %s", describeDomains(vh), describeRoutes(vh)))
		}
	}
	return fmt.Sprintf("Inline Route: %s", strings.Join(vhosts, "; "))
}

// If this is a route that matches everything and forwards to a cluster, just report the cluster.
func getMatchAllCluster(er *route.RouteConfiguration) string {
	if len(er.GetVirtualHosts()) != 1 {
		return ""
	}
	vh := er.GetVirtualHosts()[0]
	if !reflect.DeepEqual(vh.Domains, []string{"*"}) {
		return ""
	}
	if len(vh.GetRoutes()) != 1 {
		return ""
	}
	r := vh.GetRoutes()[0]
	if r.GetMatch().GetPrefix() != "/" {
		return ""
	}
	a, ok := r.GetAction().(*route.Route_Route)
	if !ok {
		return ""
	}
	cl, ok := a.Route.ClusterSpecifier.(*route.RouteAction_Cluster)
	if !ok {
		return ""
	}
	if strings.Contains(cl.Cluster, "Cluster") {
		return cl.Cluster
	}
	return fmt.Sprintf("Cluster: %s", cl.Cluster)
}

func describeDomains(vh *route.VirtualHost) string {
	if len(vh.GetDomains()) == 1 && vh.GetDomains()[0] == "*" {
		return ""
	}
	return strings.Join(vh.GetDomains(), "/")
}

func describeRoutes(vh *route.VirtualHost) string {
	routes := make([]string, 0, len(vh.GetRoutes()))
	for _, route := range vh.GetRoutes() {
		routes = append(routes, describeMatch(route.GetMatch()))
	}
	return strings.Join(routes, ", ")
}

func describeMatch(match *route.RouteMatch) string {
	conds := []string{}
	if match.GetPrefix() != "" {
		conds = append(conds, fmt.Sprintf("%s*", match.GetPrefix()))
	}
	if match.GetPathSeparatedPrefix() != "" {
		conds = append(conds, fmt.Sprintf("PathPrefix:%s", match.GetPathSeparatedPrefix()))
	}
	if match.GetPath() != "" {
		conds = append(conds, match.GetPath())
	}
	if match.GetSafeRegex() != nil {
		conds = append(conds, fmt.Sprintf("regex %s", match.GetSafeRegex().Regex))
	}
	// Ignore headers
	return strings.Join(conds, " ")
}

// PrintListenerDump prints the relevant listeners in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintListenerDump(filter ListenerFilter, outputFormat string) error {
	_, listeners, err := c.setupListenerConfigWriter()
	if err != nil {
		return err
	}
	filteredListeners := proto.MessageSlice{}
	for _, listener := range listeners {
		if filter.Verify(listener) {
			filteredListeners = append(filteredListeners, listener)
		}
	}
	out, err := json.MarshalIndent(filteredListeners, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal listeners: %v", err)
	}
	if outputFormat == "yaml" {
		if out, err = yaml.JSONToYAML(out); err != nil {
			return err
		}
	}
	fmt.Fprintln(c.Stdout, string(out))
	return nil
}

func (c *ConfigWriter) setupListenerConfigWriter() (*tabwriter.Writer, []*listener.Listener, error) {
	listeners, err := c.retrieveSortedListenerSlice()
	if err != nil {
		return nil, nil, err
	}
	w := new(tabwriter.Writer).Init(c.Stdout, 0, 8, 1, ' ', 0)
	return w, listeners, nil
}

func (c *ConfigWriter) retrieveSortedListenerSlice() ([]*listener.Listener, error) {
	if c.configDump == nil {
		return nil, fmt.Errorf("config writer has not been primed")
	}
	listenerDump, err := c.configDump.GetListenerConfigDump()
	if err != nil {
		return nil, fmt.Errorf("listener dump: %v", err)
	}
	listeners := make([]*listener.Listener, 0)
	for _, l := range listenerDump.DynamicListeners {
		if l.ActiveState != nil && l.ActiveState.Listener != nil {
			listenerTyped := &listener.Listener{}
			// Support v2 or v3 in config dump. See ads.go:RequestedTypes for more info.
			l.ActiveState.Listener.TypeUrl = v3.ListenerType
			err = l.ActiveState.Listener.UnmarshalTo(listenerTyped)
			if err != nil {
				return nil, fmt.Errorf("unmarshal listener: %v", err)
			}
			listeners = append(listeners, listenerTyped)
		}
	}

	for _, l := range listenerDump.StaticListeners {
		if l.Listener != nil {
			listenerTyped := &listener.Listener{}
			// Support v2 or v3 in config dump. See ads.go:RequestedTypes for more info.
			l.Listener.TypeUrl = v3.ListenerType
			err = l.Listener.UnmarshalTo(listenerTyped)
			if err != nil {
				return nil, fmt.Errorf("unmarshal listener: %v", err)
			}
			listeners = append(listeners, listenerTyped)
		}
	}
	if len(listeners) == 0 {
		return nil, fmt.Errorf("no listeners found")
	}
	return listeners, nil
}
