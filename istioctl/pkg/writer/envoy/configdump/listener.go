// Copyright 2018 Istio Authors
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
	"strings"
	"text/tabwriter"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"

	protio "istio.io/istio/istioctl/pkg/util/proto"
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
}

// Verify returns true if the passed listener matches the filter fields
func (l *ListenerFilter) Verify(listener *xdsapi.Listener) bool {
	if l.Address == "" && l.Port == 0 && l.Type == "" {
		return true
	}
	if l.Address != "" && strings.ToLower(retrieveListenerAddress(listener)) != strings.ToLower(l.Address) {
		return false
	}
	if l.Port != 0 && retrieveListenerPort(listener) != l.Port {
		return false
	}
	if l.Type != "" && strings.ToLower(retrieveListenerType(listener)) != strings.ToLower(l.Type) {
		return false
	}
	return true
}

func retrieveListenerType(l *xdsapi.Listener) string {
	for _, filterChain := range l.GetFilterChains() {
		for _, filter := range filterChain.GetFilters() {
			if filter.Name == HTTPListener {
				return "HTTP"
			} else if filter.Name == TCPListener {
				return "TCP"
			}
		}
	}
	return "UNKNOWN"
}

func retrieveListenerAddress(l *xdsapi.Listener) string {
	return l.Address.GetSocketAddress().Address
}

func retrieveListenerPort(l *xdsapi.Listener) uint32 {
	return l.Address.GetSocketAddress().GetPortValue()
}

// PrintListenerSummary prints a summary of the relevant listeners in the config dump to the ConfigWriter stdout
func (c *ConfigWriter) PrintListenerSummary(filter ListenerFilter) error {
	w, listeners, err := c.setupListenerConfigWriter()
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "ADDRESS\tPORT\tTYPE")
	for _, listener := range listeners {
		if filter.Verify(listener) {
			address := retrieveListenerAddress(listener)
			port := retrieveListenerPort(listener)
			listenerType := retrieveListenerType(listener)
			fmt.Fprintf(w, "%v\t%v\t%v\n", address, port, listenerType)
		}
	}
	return w.Flush()
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
		return err
	}
	fmt.Fprintln(c.Stdout, string(out))
	return nil
}

func (c *ConfigWriter) setupListenerConfigWriter() (*tabwriter.Writer, []*xdsapi.Listener, error) {
	listeners, err := c.retrieveSortedListenerSlice()
	if err != nil {
		return nil, nil, err
	}
	w := new(tabwriter.Writer).Init(c.Stdout, 0, 8, 5, ' ', 0)
	return w, listeners, nil
}

func (c *ConfigWriter) retrieveSortedListenerSlice() ([]*xdsapi.Listener, error) {
	if c.configDump == nil {
		return nil, fmt.Errorf("config writer has not been primed")
	}
	listenerDump, err := c.configDump.GetListenerConfigDump()
	if err != nil {
		return nil, err
	}
	listeners := []*xdsapi.Listener{}
	for _, listener := range listenerDump.DynamicActiveListeners {
		listeners = append(listeners, listener.Listener)
	}
	// Static listeners not currently in use, leaving this code here in case they ever are
	// for _, listener := range listenerDump.StaticListeners {
	// 	listeners = append(listeners, &listener)
	// }
	if len(listeners) == 0 {
		return nil, fmt.Errorf("no listeners found")
	}
	return listeners, nil
}
