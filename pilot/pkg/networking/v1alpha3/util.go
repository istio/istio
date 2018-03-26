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
	"strconv"
	"strings"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"

	"istio.io/istio/pkg/log"
)

// convertAddressListToCidrList converts a list of IP addresses with cidr prefixes into envoy CIDR proto
func convertAddressListToCidrList(addresses []string) []*core.CidrRange {
	if addresses == nil {
		return nil
	}

	cidrList := make([]*core.CidrRange, 0)
	for _, addr := range addresses {
		cidr := &core.CidrRange{
			AddressPrefix: addr,
			PrefixLen: &types.UInt32Value{
				Value: 32,
			},
		}

		if strings.Contains(addr, "/") {
			parts := strings.Split(addr, "/")
			cidr.AddressPrefix = parts[0]
			prefix, _ := strconv.Atoi(parts[1])
			cidr.PrefixLen.Value = uint32(prefix)
		}
		cidrList = append(cidrList, cidr)
	}
	return cidrList
}

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

func messageToStruct(msg proto.Message) *types.Struct {
	s, err := util.MessageToStruct(msg)
	if err != nil {
		log.Error(err.Error())
		return &types.Struct{}
	}
	return s
}

func convertGogoDurationToDuration(d *types.Duration) time.Duration {
	if d == nil {
		return 0
	}
	dur, err := types.DurationFromProto(d)
	if err != nil {
		log.Warnf("error converting duration %#v, using 0: %v", d, err)
	}
	return dur
}

// convertDuration converts to golang duration and logs errors
func convertProtoDurationToDuration(d *duration.Duration) time.Duration {
	if d == nil {
		return 0
	}
	dur, err := ptypes.Duration(d)
	if err != nil {
		log.Warnf("error converting duration %#v, using 0: %v", d, err)
	}
	return dur
}
