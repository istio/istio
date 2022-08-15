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
	"bytes"
	"errors"
	"fmt"

	bootstrapv3 "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	httpConn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/golang/protobuf/jsonpb"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/util/protomarshal"
)

// nolint: interfacer
func BuildXDSObjectFromStruct(applyTo networking.EnvoyFilter_ApplyTo, value *structpb.Struct, strict bool) (proto.Message, error) {
	if value == nil {
		// for remove ops
		return nil, nil
	}
	var obj proto.Message
	switch applyTo {
	case networking.EnvoyFilter_CLUSTER:
		obj = &cluster.Cluster{}
	case networking.EnvoyFilter_LISTENER:
		obj = &listener.Listener{}
	case networking.EnvoyFilter_ROUTE_CONFIGURATION:
		obj = &route.RouteConfiguration{}
	case networking.EnvoyFilter_FILTER_CHAIN:
		obj = &listener.FilterChain{}
	case networking.EnvoyFilter_HTTP_FILTER:
		obj = &httpConn.HttpFilter{}
	case networking.EnvoyFilter_NETWORK_FILTER:
		obj = &listener.Filter{}
	case networking.EnvoyFilter_VIRTUAL_HOST:
		obj = &route.VirtualHost{}
	case networking.EnvoyFilter_HTTP_ROUTE:
		obj = &route.Route{}
	case networking.EnvoyFilter_EXTENSION_CONFIG:
		obj = &core.TypedExtensionConfig{}
	case networking.EnvoyFilter_BOOTSTRAP:
		obj = &bootstrapv3.Bootstrap{}
	default:
		return nil, fmt.Errorf("Envoy filter: unknown object type for applyTo %s", applyTo.String()) // nolint: stylecheck
	}

	if err := StructToMessage(value, obj, strict); err != nil {
		return nil, fmt.Errorf("Envoy filter: %v", err) // nolint: stylecheck
	}
	return obj, nil
}

func StructToMessage(pbst *structpb.Struct, out proto.Message, strict bool) error {
	if pbst == nil {
		return errors.New("nil struct")
	}

	buf := &bytes.Buffer{}
	if err := (&jsonpb.Marshaler{OrigName: true}).Marshal(buf, pbst); err != nil {
		return err
	}

	// If strict is not set, ignore unknown fields as they may be sending versions of
	// the proto we are not internally using
	if strict {
		return protomarshal.Unmarshal(buf.Bytes(), out)
	}
	return protomarshal.UnmarshalAllowUnknown(buf.Bytes(), out)
}
