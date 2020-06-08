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
	"strconv"
	"strings"
	"text/tabwriter"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	httpConn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	envoy_extensions_filters_network_tcp_proxy_v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	"github.com/golang/protobuf/ptypes"

	protio "istio.io/istio/istioctl/pkg/util/proto"
	"istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/proxy/envoy/v3"
)

const (
	// HTTPListener identifies a listener as being of HTTP type by the presence of an HTTP connection manager filter
	HTTPListener = "envoy.http_connection_manager"

	// TCPListener identifies a listener as being of TCP type by the presence of TCP proxy filter
	TCPListener = "envoy.tcp_proxy"
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
	if l.Address != "" && !strings.EqualFold(retrieveListenerAddress(listener), l.Address) {
		return false
	}
	if l.Port != 0 && retrieveListenerPort(listener) != l.Port {
		return false
	}
	if l.Type != "" && !strings.EqualFold(retrieveListenerType(listener), l.Type) {
		return false
	}
	return true
}

// retrieveListenerType classifies a Listener as HTTP|TCP|HTTP+TCP|UNKNOWN
func retrieveListenerType(l *listener.Listener) string {
	nHTTP := 0
	nTCP := 0
	for _, filterChain := range l.GetFilterChains() {
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
	return l.Address.GetSocketAddress().Address
}

func retrieveListenerPort(l *listener.Listener) uint32 {
	return l.Address.GetSocketAddress().GetPortValue()
}

// PrintListenerSummary prints a summary of the relevant listeners in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintListenerSummary(filter ListenerFilter) error {
	w, listeners, err := c.setupListenerConfigWriter()
	if err != nil {
		return err
	}
	if filter.Verbose {
		fmt.Fprintln(w, "ADDRESS\tPORT\tMATCH\tDESTINATION")
	} else {
		fmt.Fprintln(w, "ADDRESS\tPORT\tTYPE")
	}
	for _, l := range listeners {
		if filter.Verify(l) {
			address := retrieveListenerAddress(l)
			port := retrieveListenerPort(l)
			if filter.Verbose {

				matches := retrieveListenerMatches(l)
				for _, match := range matches {
					fmt.Fprintf(w, "%v\t%v\t%v\t%v\n", address, port, match.match, match.destination)
				}
			} else {
				listenerType := retrieveListenerType(l)
				fmt.Fprintf(w, "%v\t%v\t%v\n", address, port, listenerType)
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
	mtlsHTTPALPNs      = []string{"istio-http/1.0", "istio-http/1.1", "istio-h2"}
	istioHTTPPlaintext = []string{"istio", "istio-http/1.0", "istio-http/1.1", "istio-h2"}
	httpTLS            = []string{"http/1.0", "http/1.1", "h2c", "istio-http/1.0", "istio-http/1.1", "istio-h2"}
	tcpTLS             = []string{"istio-peer-exchange", "istio"}
)

func retrieveListenerMatches(l *listener.Listener) []filterchain {
	resp := []filterchain{}
	for _, filterChain := range l.GetFilterChains() {
		match := filterChain.FilterChainMatch
		if match == nil {
			match = &listener.FilterChainMatch{}
		}
		ms := ""
		if len(match.ServerNames) > 0 {
			ms += ", SNI: " + strings.Join(match.ServerNames, ",")
		}
		if len(match.TransportProtocol) > 0 {
			ms += ", Protocol: " + match.TransportProtocol
		}
		if len(match.ApplicationProtocols) > 0 {
			if reflect.DeepEqual(match.ApplicationProtocols, httpTLS) {
				ms += ", App Protocol: HTTP TLS"
			} else if reflect.DeepEqual(match.ApplicationProtocols, istioHTTPPlaintext) {
				ms += ", App Protocol: Istio HTTP Plaintext"
			} else if reflect.DeepEqual(match.ApplicationProtocols, tcpTLS) {
				ms += ", App Protocol: TCP TLS"
			} else if reflect.DeepEqual(match.ApplicationProtocols, plaintextHTTPALPNs) {
				ms += ", App Protocol: HTTP"
			} else {
				ms += ", App Protocol: " + strings.Join(match.ApplicationProtocols, ",")
			}
		}
		if match.DestinationPort != nil {
			ms += ", Port: " + strconv.Itoa(int(match.DestinationPort.GetValue()))
		}
		if match.AddressSuffix != "" {
			ms += ", Address: " + match.AddressSuffix
		}
		if len(match.PrefixRanges) > 0 {
			pf := []string{}
			for _, p := range match.PrefixRanges {
				pf = append(pf, fmt.Sprintf("%s/%d", p.AddressPrefix, p.GetPrefixLen().GetValue()))
			}
			ms += ", Address: " + strings.Join(pf, ",")
		}
		if len(ms) > 0 {
			ms = ms[2:]
		} else {
			ms = "ALL"
		}
		fc := filterchain{
			match:       ms,
			destination: getFilterType(filterChain.GetFilters()),
		}
		resp = append(resp, fc)
	}
	return resp
}

func getFilterType(filters []*listener.Filter) string {
	for _, filter := range filters {
		if filter.Name == HTTPListener {

			httpProxy := &httpConn.HttpConnectionManager{}
			ptypes.UnmarshalAny(filter.GetTypedConfig(), httpProxy)
			if httpProxy.GetRouteConfig() != nil {
				return "Inline Route"
			}
			if httpProxy.GetRds().GetRouteConfigName() != "" {
				return httpProxy.GetRds().GetRouteConfigName()
			}
			return "HTTP"
		} else if filter.Name == TCPListener {
			if !strings.Contains(string(filter.GetTypedConfig().GetValue()), util.BlackHoleCluster) {
				tcpProxy := &envoy_extensions_filters_network_tcp_proxy_v3.TcpProxy{}
				ptypes.UnmarshalAny(filter.GetTypedConfig(), tcpProxy)
				return tcpProxy.GetCluster()
			}
		}
	}
	return ""
}

// PrintListenerDump prints the relevant listeners in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintListenerDump(filter ListenerFilter) error {
	_, listeners, err := c.setupListenerConfigWriter()
	if err != nil {
		return err
	}
	filteredListeners := protio.MessageSlice{}
	for _, listener := range listeners {
		if filter.Verify(listener) {
			filteredListeners = append(filteredListeners, listener)
		}
	}
	out, err := json.MarshalIndent(filteredListeners, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal listeners: %v", err)
	}
	fmt.Fprintln(c.Stdout, string(out))
	return nil
}

func (c *ConfigWriter) setupListenerConfigWriter() (*tabwriter.Writer, []*listener.Listener, error) {
	listeners, err := c.retrieveSortedListenerSlice()
	if err != nil {
		return nil, nil, err
	}
	w := new(tabwriter.Writer).Init(c.Stdout, 0, 8, 5, ' ', 0)
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
			err = ptypes.UnmarshalAny(l.ActiveState.Listener, listenerTyped)
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
			err = ptypes.UnmarshalAny(l.Listener, listenerTyped)
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
