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

// EnvoyFilters is the registry of the validated Envoy HTTP filters.
var EnvoyFilters = map[string]EnvoyFilter{}

func initRegister(ef EnvoyFilter) {
	EnvoyFilters[ef.TypeURL()] = ef
}

// ConvertHTTPFilter from JSON to the validated typed config.
func ConvertHTTPFilter(pbst *structpb.Struct) (*hcm.HttpFilter, error) {
	if pbst == nil {
		return nil, errors.New("nil struct")
	}
	json, err := protomarshal.MarshalProtoNames(pbst)
	if err != nil {
		return nil, err
	}
	filter := &hcm.HttpFilter{}
	err = protomarshal.Unmarshal(json, filter)
	if err != nil || filter.GetTypedConfig() == nil || filter.Name == "" {
		return nil, fmt.Errorf("failed to parse HTTP filter xDS config: %v", err)
	}
	ef, ok := EnvoyFilters[filter.GetTypedConfig().TypeUrl]
	if !ok {
		return nil, fmt.Errorf("unknown HTTP filter extension: %s", filter.GetTypedConfig().TypeUrl)
	}
	config := ef.New()
	err = proto.UnmarshalOptions{DiscardUnknown: false}.Unmarshal(filter.GetTypedConfig().Value, config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s xDS config: %v", ef.Name(), err)
	}
	err = ef.Validate(config)
	if err != nil {
		return nil, fmt.Errorf("failed to validate %s xDS config: %v", ef.Name(), err)
	}
	bytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(config)
	if err != nil {
		return nil, err
	}
	return &hcm.HttpFilter{
		Name: filter.Name,
		ConfigType: &hcm.HttpFilter_TypedConfig{
			TypedConfig: &anypb.Any{
				TypeUrl: ef.TypeURL(),
				Value:   bytes,
			},
		},
	}, nil
}
