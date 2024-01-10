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
	"strings"

	udpa "github.com/cncf/xds/go/udpa/type/v1"
	bootstrap "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/structpb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
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
		obj = &listener.ListenerFilter{}
	default:
		return nil, fmt.Errorf("Envoy filter: unknown object type for applyTo %s", applyTo.String()) // nolint: stylecheck
	}

	if err := StructToMessage(value, obj, strict); err != nil {
		return nil, fmt.Errorf("Envoy filter: %v", err) // nolint: stylecheck
	}
	return obj, nil
}

type anyTransformer struct {
	unknownTypes sets.String
}

var typedStructTypeURL = protoconv.TypeURL(&udpa.TypedStruct{})

func (a *anyTransformer) AnyToTypedStruct(pbst *structpb.Struct) *structpb.Struct {
	typeURL, f := pbst.Fields["@type"]
	if f && typeURL.GetStringValue() != "" && !isTypeKnown(typeURL.GetStringValue()) {
		// need to transform!
		transformed, _ := structpb.NewStruct(make(map[string]any, 3))
		transformed.Fields["@type"] = structpb.NewStringValue(typedStructTypeURL)
		transformed.Fields["type_url"] = typeURL
		pbst := proto.Clone(pbst).(*structpb.Struct)
		delete(pbst.Fields, "@type")
		transformed.Fields["value"] = structpb.NewStructValue(pbst)
		a.unknownTypes.Insert(typeURL.GetStringValue())
		return transformed
	}
	var parent *structpb.Struct

	for k, v := range pbst.Fields {
		switch t := v.Kind.(type) {
		case *structpb.Value_StructValue:
			res := a.AnyToTypedStruct(t.StructValue)
			if res != nil {
				if parent == nil {
					parent = proto.Clone(pbst).(*structpb.Struct)
				}
				parent.Fields[k] = structpb.NewStructValue(res)
			}
		case *structpb.Value_ListValue:
			for idx, i := range t.ListValue.Values {
				if sz := i.GetStructValue(); sz != nil {
					res := a.AnyToTypedStruct(sz)
					if res != nil {
						if parent == nil {
							parent = proto.Clone(pbst).(*structpb.Struct)
						}
						parent.Fields[k].GetListValue().Values[idx] = structpb.NewStructValue(res)
					}
				}
			}
		}
	}
	return parent
}

func StructToMessage(raw *structpb.Struct, out proto.Message, strict bool) error {
	if raw == nil {
		return errors.New("nil struct")
	}

	at := anyTransformer{unknownTypes: sets.New[string]()}
	pbst := at.AnyToTypedStruct(raw)
	if pbst == nil {
		// No clone needed
		pbst = raw
	}
	// Strict is set, but we found unknown *types*. Warn there will be no validation
	if strict && !at.unknownTypes.IsEmpty() {
		return fmt.Errorf("unknown @type values detected; message will be sent as unvalidated: %v", sets.SortedList(at.unknownTypes))
	}
	buf, err := protomarshal.MarshalProtoNames(pbst)
	if err != nil {
		return err
	}

	// If strict is not set, ignore unknown fields as they may be sending versions of
	// the proto we are not internally using.
	// Note this is for unknown fields; above we handle unknown types.
	if strict {
		return protomarshal.Unmarshal(buf, out)
	}
	return protomarshal.UnmarshalAllowUnknown(buf, out)
}

func isTypeKnown(typeURL string) bool {
	mname := typeURL
	if slash := strings.LastIndex(typeURL, "/"); slash >= 0 {
		mname = mname[slash+1:]
	}
	_, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(mname))
	return err == nil
}
