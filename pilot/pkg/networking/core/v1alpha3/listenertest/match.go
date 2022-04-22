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

package listenertest

import (
	"fmt"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

type ListenersTest struct {
	// Match listener by name
	Name string
	// Match listener by port
	Port uint32

	// Listener assertions
	Listener ListenerTest
}

// ListenerTest provides a struct for defining expectations for a listener
type ListenerTest struct {
	// Assert the listener contains these filter chains (in order, if TotalMatch)
	FilterChains []FilterChainTest

	// Assert the listener contains these ListenerFilters (in order, if TotalMatch)
	Filters []string

	// TotalMatch will require that the all elements exactly match (eg, if I have 3 elements in the
	// check, the listener must as well). Otherwise, we only validate the assertions we provided are
	// present.
	TotalMatch bool
}

type FilterChainTest struct {
	// Match a filter chain by name
	Name string
	// Match filter chain by 'type'. This can be important since Name is currently not unique
	Type FilterChainType
	// Port the filter chain matches
	Port uint32

	NetworkFilters []string
	HTTPFilters    []string

	ValidateHCM func(t test.Failer, hcm *hcm.HttpConnectionManager)

	TotalMatch bool
}

type FilterChainType string

const (
	PlainTCP    FilterChainType = "plaintext TCP"
	PlainHTTP   FilterChainType = "plaintext HTTP"
	StandardTLS FilterChainType = "TLS"
	MTLSTCP     FilterChainType = "mTLS TCP"
	MTLSHTTP    FilterChainType = "mTLS HTTP"
	Unknown     FilterChainType = "unknown"
)

func classifyFilterChain(have *listener.FilterChain) FilterChainType {
	fcm := have.GetFilterChainMatch()
	alpn := sets.New(fcm.GetApplicationProtocols()...)
	switch fcm.GetTransportProtocol() {
	case xdsfilters.TLSTransportProtocol:
		if alpn.Contains("istio-http/1.1") {
			return MTLSHTTP
		}
		if alpn.Contains("istio") {
			return MTLSTCP
		}
		return StandardTLS
	case xdsfilters.RawBufferTransportProtocol:
		if alpn.Contains("http/1.1") {
			return PlainHTTP
		}
		return PlainTCP
	default:
		return Unknown
	}
}

func VerifyListeners(t test.Failer, listeners []*listener.Listener, lt ListenersTest) {
	t.Helper()
	for _, l := range listeners {
		if lt.Name != "" && lt.Name != l.Name {
			continue
		}
		if lt.Port != 0 && lt.Port != l.Address.GetSocketAddress().GetPortValue() {
			continue
		}
		// It was a match, run assertions
		VerifyListener(t, l, lt.Listener)
	}
}

func VerifyListener(t test.Failer, l *listener.Listener, lt ListenerTest) {
	t.Helper()
	haveFilters := []string{}
	for _, lf := range l.ListenerFilters {
		haveFilters = append(haveFilters, lf.Name)
	}

	// Check ListenerFilters

	if lt.Filters != nil {
		if lt.TotalMatch {
			assert.Equal(t, lt.Filters, haveFilters, l.Name+": listener filters should be equal")
		}
	} else {
		if missing := sets.New(lt.Filters...).Difference(sets.New(haveFilters...)).SortedList(); len(missing) > 0 {
			t.Fatalf("%v: missing listener filters: %v", l.Name, missing)
		}
	}

	// Check FilterChains
	if lt.FilterChains != nil {
		if lt.TotalMatch {
			// First check they are the same size
			if len(lt.FilterChains) != len(l.FilterChains) {
				want := []string{}
				for _, n := range lt.FilterChains {
					want = append(want, n.Name)
				}
				t.Fatalf("didn't match filter chains, have names %v, expected %v", xdstest.ExtractFilterChainNames(l), want)
			}
			// Now check they are equivalent
			for i := range lt.FilterChains {
				have := l.FilterChains[i]
				haveType := classifyFilterChain(have)
				want := lt.FilterChains[i]
				context := func(s string) string {
					return fmt.Sprintf("%v/%v: %v", have.Name, haveType, s)
				}
				if want.Name != "" {
					assert.Equal(t, want.Name, have.Name, context("name should be equal"))
				}
				if want.Type != "" {
					assert.Equal(t, want.Type, haveType, context("type should be equal"))
				}
				if want.Port != 0 {
					assert.Equal(t, want.Port, have.GetFilterChainMatch().GetDestinationPort().GetValue(), context("port should be equal"))
				}
				assertFilterChain(t, have, want)
			}
		} else {
			for _, want := range lt.FilterChains {
				found := 0
				for _, have := range l.FilterChains {
					if want.Name != "" && want.Name != have.Name {
						continue
					}
					haveType := classifyFilterChain(have)
					if want.Type != "" && want.Type != haveType {
						continue
					}
					if want.Port != 0 && want.Port != have.GetFilterChainMatch().GetDestinationPort().GetValue() {
						continue
					}
					found++
					assertFilterChain(t, have, want)
				}
				if found == 0 {
					t.Fatalf("No matching chain found for %+v", want)
				}
				if found > 1 {
					t.Logf("warning: multiple matching chains found for %+v", want)
				}
			}
		}
	}
}

func assertFilterChain(t test.Failer, have *listener.FilterChain, want FilterChainTest) {
	t.Helper()
	haveType := classifyFilterChain(have)
	context := func(s string) string {
		return fmt.Sprintf("%v/%v: %v", have.Name, haveType, s)
	}
	haveNetwork, haveHTTP := xdstest.ExtractFilterNames(t, have)
	if want.TotalMatch {
		if want.NetworkFilters != nil {
			assert.Equal(t, want.NetworkFilters, haveNetwork, context("network filters should be equal"))
		}
		if want.HTTPFilters != nil {
			assert.Equal(t, want.HTTPFilters, haveHTTP, context("http should be equal"))
		}
	} else {
		if missing := sets.New(want.NetworkFilters...).Difference(sets.New(haveNetwork...)).SortedList(); len(missing) > 0 {
			t.Fatalf("%v/%v: missing network filters: %v", have.Name, haveType, missing)
		}
		if missing := sets.New(want.HTTPFilters...).Difference(sets.New(haveHTTP...)).SortedList(); len(missing) > 0 {
			t.Fatalf("%v/%v: missing network filters: %v", have.Name, haveType, missing)
		}
	}
	if want.ValidateHCM != nil {
		want.ValidateHCM(t, xdstest.ExtractHTTPConnectionManager(t, have))
	}
}
