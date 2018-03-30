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

package util

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

	"istio.io/istio/pkg/log"
)

//// convertAddressListToCidrList converts a list of IP addresses with cidr prefixes into envoy CIDR proto
//func convertAddressListToCidrList(addresses []string) []*core.CidrRange {
//	if addresses == nil {
//		return nil
//	}
//
//	cidrList := make([]*core.CidrRange, 0)
//	for _, addr := range addresses {
//		cidrList = append(cidrList, ConvertAddressToCidr(addr))
//	}
//	return cidrList
//}

// ConvertAddressToCidr converts from string to CIDR proto
func ConvertAddressToCidr(addr string) *core.CidrRange {
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
	return cidr
}

// NormalizeListeners sorts and de-duplicates listeners by address
func NormalizeListeners(listeners []*xdsapi.Listener) []*xdsapi.Listener {
	out := make([]*xdsapi.Listener, 0, len(listeners))
	set := make(map[string]bool)
	for _, listener := range listeners {
		if !set[listener.Address.String()] {
			set[listener.Address.String()] = true
			out = append(out, listener)
		} else { // noolint megacheck
			// we already have a listener on this address.
			// WE can merge the two listeners if and only if they are of the same type
			// i.e. both HTTP or both TCP.
			// for the moment, we handle HTTP only. Need to do TCP. or use filter chain match
			//existingListener := set[listener.Address.String()]
			//if listener.ListenerFilters[0].
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address.String() < out[j].Address.String() })
	return out
}

// BuildAddress returns a SocketAddress with the given ip and port.
func BuildAddress(ip string, port uint32) core.Address {
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

// GetByAddress returns a listener by its address
// TODO(mostrowski): consider passing map around to save iteration.
func GetByAddress(listeners []*xdsapi.Listener, addr string) *xdsapi.Listener {
	for _, listener := range listeners {
		if listener.Address.String() == addr {
			return listener
		}
	}
	return nil
}

//// protoDurationToTimeDuration converts d to time.Duration format.
//func protoDurationToTimeDuration(d *types.Duration) time.Duration { //nolint
//	return time.Duration(d.Nanos) + time.Second*time.Duration(d.Seconds)
//}
//
//// google_protobufToProto converts d to google protobuf Duration format.
//func durationToProto(d time.Duration) *types.Duration { // nolint
//	nanos := d.Nanoseconds()
//	secs := nanos / 1e9
//	nanos -= secs * 1e9
//	return &types.Duration{
//		Seconds: secs,
//		Nanos:   int32(nanos),
//	}
//}

//func buildProtoStruct(name, value string) *types.Struct {
//	return &types.Struct{
//		Fields: map[string]*types.Value{
//			name: {
//				Kind: &types.Value_StringValue{
//					StringValue: value,
//				},
//			},
//		},
//	}
//}

// MessageToStruct converts from proto message to proto Struct
func MessageToStruct(msg proto.Message) *types.Struct {
	s, err := util.MessageToStruct(msg)
	if err != nil {
		log.Error(err.Error())
		return &types.Struct{}
	}
	return s
}

// ConvertGogoDurationToDuration converts from gogo proto duration to time.duration
func ConvertGogoDurationToDuration(d *types.Duration) time.Duration {
	if d == nil {
		return 0
	}
	dur, err := types.DurationFromProto(d)
	if err != nil {
		log.Warnf("error converting duration %#v, using 0: %v", d, err)
	}
	return dur
}
