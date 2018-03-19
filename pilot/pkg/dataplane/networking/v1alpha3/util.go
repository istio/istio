// Copyright 2017 Istio Authors
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

package v1alpha3

import (
	"sort"
	// TODO(mostrowski): remove JSON encoding once mixer filter proto spec is available.
	oldjson "encoding/json"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	google_protobuf "github.com/gogo/protobuf/types"
)

// normalizeListeners sorts and de-duplicates listeners by address
func normalizeListeners(listeners []*xdsapi.Listener) []*xdsapi.Listener {
	out := make([]*xdsapi.Listener, 0, len(listeners))
	set := make(map[string]bool)
	for _, listener := range listeners {
		if !set[listener.Address.String()] {
			set[listener.Address.String()] = true
			out = append(out, listener)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address.String() < out[j].Address.String() })
	return out
}

// mustMarshalToString marshals i to a JSON string. It panics if i cannot be marshaled.
// TODO(mostrowski): this should be removed once v2 Mixer proto is finalized.
func mustMarshalToString(i interface{}) string {
	s, err := oldjson.Marshal(i)
	if err != nil {
		panic(err)
	}
	return string(s)
}

// buildAddress returns a SocketAddress with the given ip and port.
func buildAddress(ip string, port uint32) core.Address {
	return core.Address{
		Address: &core.Address_SocketAddress{
			SocketAddress: &core.SocketAddress{
				Address: ip,
				PortSpecifier: &core.SocketAddress_PortValue{
					PortValue: port,
				},
			},
		},
	}
}

// getByAddress returns a listener by its address
// TODO(mostrowski): consider passing map around to save iteration.
func getByAddress(listeners []*xdsapi.Listener, addr string) *xdsapi.Listener {
	for _, listener := range listeners {
		if listener.Address.String() == addr {
			return listener
		}
	}
	return nil
}

func buildHTTPFilterConfig(name, protoStr string) *http_conn.HttpFilter {
	return &http_conn.HttpFilter{
		Name:   name,
		Config: buildProtoStruct(name, protoStr),
	}
}

func buildProtoStruct(name, value string) *google_protobuf.Struct {
	return &google_protobuf.Struct{
		Fields: map[string]*google_protobuf.Value{
			name: {
				Kind: &google_protobuf.Value_StringValue{
					StringValue: value,
				},
			},
		},
	}
}
