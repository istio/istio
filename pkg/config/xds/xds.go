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

package xds

import (
	"errors"
	"fmt"

	bootstrap "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/util/protomarshal"
)

// nolint: interfacer
func BuildXDSObjectFromStruct(cp *networking.EnvoyFilter_EnvoyConfigObjectPatch, strict bool) (proto.Message, error, error) {
	validationErr := ValidateSafety(cp.ApplyTo, cp.Patch.Operation, cp.Match)
	if cp.Patch.Value == nil {
		// for remove ops
		return nil, nil, validationErr
	}
	var obj proto.Message
	switch cp.ApplyTo {
	case networking.EnvoyFilter_CLUSTER:
		obj = &cluster.Cluster{}
	case networking.EnvoyFilter_LISTENER:
		obj = &listener.Listener{}
	case networking.EnvoyFilter_ROUTE_CONFIGURATION:
		obj = &route.RouteConfiguration{}
	case networking.EnvoyFilter_FILTER_CHAIN:
		obj = &listener.FilterChain{}
	case networking.EnvoyFilter_HTTP_FILTER:
		// Try the restricted conversion first.
		var convertErr error
		obj, convertErr = ConvertHTTPFilter(cp.Patch.Value)
		validationErr = errors.Join(validationErr, convertErr)
		if validationErr == nil {
			return obj, nil, nil
		}
		obj = &hcm.HttpFilter{}
	case networking.EnvoyFilter_NETWORK_FILTER:
		obj = &listener.Filter{}
	case networking.EnvoyFilter_VIRTUAL_HOST:
		obj = &route.VirtualHost{}
	case networking.EnvoyFilter_HTTP_ROUTE:
		obj = &route.Route{}
	case networking.EnvoyFilter_EXTENSION_CONFIG:
		obj = &core.TypedExtensionConfig{}
	case networking.EnvoyFilter_BOOTSTRAP:
		obj = &bootstrap.Bootstrap{}
	case networking.EnvoyFilter_LISTENER_FILTER:
		var convertErr error
		obj, convertErr = ConvertListenerFilter(cp.Patch.Value)
		validationErr = errors.Join(validationErr, convertErr)
		if validationErr == nil {
			return obj, nil, nil
		}
		obj = &listener.ListenerFilter{}
	default:
		return nil, fmt.Errorf("Envoy filter: unknown object type for applyTo %s", cp.ApplyTo.String()), validationErr // nolint: stylecheck
	}

	if err := StructToMessage(cp.Patch.Value, obj, strict); err != nil {
		return nil, fmt.Errorf("Envoy filter: %v", err), validationErr // nolint: stylecheck
	}
	return obj, nil, validationErr
}

// ValidateSafety checks for restrictions on the EnvoyFilter.
func ValidateSafety(applyTo networking.EnvoyFilter_ApplyTo, operation networking.EnvoyFilter_Patch_Operation,
	match *networking.EnvoyFilter_EnvoyConfigObjectMatch,
) error {
	if operation != networking.EnvoyFilter_Patch_INSERT_AFTER &&
		operation != networking.EnvoyFilter_Patch_INSERT_BEFORE &&
		operation != networking.EnvoyFilter_Patch_INSERT_FIRST &&
		operation != networking.EnvoyFilter_Patch_ADD {
		return fmt.Errorf("unsupported operation: %s", operation.String())
	}
	if applyTo != networking.EnvoyFilter_HTTP_FILTER &&
		applyTo != networking.EnvoyFilter_LISTENER_FILTER {
		return fmt.Errorf("unsupported patch object: %s", applyTo.String())
	}
	if match != nil && match.ObjectTypes != nil {
		lm := match.GetListener()
		if lm == nil || lm.Name != "" || lm.ListenerFilter != "" || lm.PortName != "" {
			return fmt.Errorf("match by listener attributes except port number is not supported")
		}
		// The only allowed reference point is "router" for HTTP filters
		if fc := lm.FilterChain; fc != nil {
			if applyTo != networking.EnvoyFilter_HTTP_FILTER {
				return fmt.Errorf("filter chain match can be only be used for HTTP filter")
			}
			if fc.Name != "" || fc.Sni != "" || fc.TransportProtocol != "" || fc.ApplicationProtocols != "" || fc.DestinationPort > 0 {
				return fmt.Errorf("match by filter chain attributes not supported")
			}
			if f := fc.Filter; f != nil {
				if f.Name != "envoy.filters.network.http_connection_manager" {
					return fmt.Errorf("unknown filter: %s", f.Name)
				}
				if f.SubFilter != nil && f.SubFilter.Name != "envoy.filters.http.router" {
					return fmt.Errorf("only router can be used as a reference: %s", f.SubFilter.Name)
				}
			}
		}
	}
	return nil
}

func StructToMessage(pbst *structpb.Struct, out proto.Message, strict bool) error {
	if pbst == nil {
		return errors.New("nil struct")
	}

	buf, err := protomarshal.MarshalProtoNames(pbst)
	if err != nil {
		return err
	}

	// If strict is not set, ignore unknown fields as they may be sending versions of
	// the proto we are not internally using
	if strict {
		return protomarshal.Unmarshal(buf, out)
	}
	return protomarshal.UnmarshalAllowUnknown(buf, out)
}
