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

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	resource "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"

	"istio.io/istio/pkg/util/protomarshal"
)

const (
	WasmHTTPFilterType = resource.APITypePrefix + wellknown.HTTPWasm
	RBACHTTPFilterType = resource.APITypePrefix + "envoy.extensions.filters.http.rbac.v3.RBAC"
	TypedStructType    = resource.APITypePrefix + "udpa.type.v1.TypedStruct"

	StatsFilterName       = "istio.stats"
	StackdriverFilterName = "istio.stackdriver"
)

// EnvoyFilter is the description of the validated Envoy HTTP filters.
type EnvoyFilter interface {
	// Name is the human readable filter name.
	Name() string
	// TypeURL is the protobuf type URL for the filter config.
	TypeURL() string
	// New creates a new instance of the filter config.
	New() proto.Message
	// Validate performs the structural validation. The input is always of New() type.
	Validate(config proto.Message) error
}

// EnvoyHTTPFilters is the registry of the validated Envoy HTTP filters.
var EnvoyHTTPFilters = map[string]EnvoyFilter{}

// EnvoyListenerFilters is the registry of the validated Envoy listener filters.
var EnvoyListenerFilters = map[string]EnvoyFilter{}

func initRegisterHTTP(ef EnvoyFilter) {
	EnvoyHTTPFilters[ef.TypeURL()] = ef
}
func initRegisterListener(ef EnvoyFilter) {
	EnvoyListenerFilters[ef.TypeURL()] = ef
}

// ConvertHTTPFilter from JSON to the validated typed HTTP filter config.
func ConvertHTTPFilter(pbst *structpb.Struct) (*hcm.HttpFilter, error) {
	out := &hcm.HttpFilter{}
	err := convertFilter(pbst, out)
	return out, err
}

// ConvertListenerFilter from JSON to the validated typed listener filter config.
func ConvertListenerFilter(pbst *structpb.Struct) (*listener.ListenerFilter, error) {
	out := &listener.ListenerFilter{}
	err := convertFilter(pbst, out)
	if out.FilterDisabled != nil {
		return out, errors.New("listener filter matcher not supported")
	}
	return out, err
}

type filterConfig interface {
	proto.Message
	*hcm.HttpFilter | *listener.ListenerFilter
	GetName() string
	GetTypedConfig() *anypb.Any
}

func convertFilter[T filterConfig](pbst *structpb.Struct, filter T) error {
	if pbst == nil {
		return errors.New("nil struct")
	}
	json, err := protomarshal.MarshalProtoNames(pbst)
	if err != nil {
		return err
	}
	err = protomarshal.Unmarshal(json, filter)
	if err != nil || filter.GetTypedConfig() == nil || filter.GetName() == "" {
		return fmt.Errorf("failed to parse filter xDS config: %v", err)
	}
	ef, ok := EnvoyHTTPFilters[filter.GetTypedConfig().TypeUrl]
	if !ok {
		return fmt.Errorf("unknown filter extension: %s", filter.GetTypedConfig().TypeUrl)
	}
	config := ef.New()
	err = proto.UnmarshalOptions{DiscardUnknown: false}.Unmarshal(filter.GetTypedConfig().Value, config)
	if err != nil {
		return fmt.Errorf("failed to parse %s xDS config: %v", ef.Name(), err)
	}
	err = ef.Validate(config)
	if err != nil {
		return fmt.Errorf("failed to validate %s xDS config: %v", ef.Name(), err)
	}
	return nil
}
