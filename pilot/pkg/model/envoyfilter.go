// Copyright 2019 Istio Authors
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
package model

import (
	"fmt"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	xdsutil "github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	networking "istio.io/api/networking/v1alpha3"
)

// EnvoyFilterWrapper is a wrapper for the EnvoyFilter api object with pre-processed data
type EnvoyFilterWrapper struct {
	workloadSelector Labels
	ConfigPatches    []*EnvoyFilterConfigPatchWrapper
}

// EnvoyFilterConfigPatchWrapper is a wrapper over the EnvoyFilter ConfigPatch api object
// fields are ordered such that this struct is aligned
type EnvoyFilterConfigPatchWrapper struct {
	Value     proto.Message
	Match     *networking.EnvoyFilter_EnvoyConfigObjectMatch
	ApplyTo   networking.EnvoyFilter_ApplyTo
	Operation networking.EnvoyFilter_Patch_Operation
}

// convertToEnvoyFilterWrapper converts from EnvoyFilter config to EnvoyFilterWrapper object
func convertToEnvoyFilterWrapper(local *Config) *EnvoyFilterWrapper {
	localEnvoyFilter := local.Spec.(*networking.EnvoyFilter)

	out := &EnvoyFilterWrapper{}
	if localEnvoyFilter.WorkloadSelector != nil {
		out.workloadSelector = Labels(localEnvoyFilter.WorkloadSelector.Labels)
	}
	out.ConfigPatches = make([]*EnvoyFilterConfigPatchWrapper, 0, len(localEnvoyFilter.ConfigPatches))
	for _, cp := range localEnvoyFilter.ConfigPatches {
		cpw := &EnvoyFilterConfigPatchWrapper{
			ApplyTo:   cp.ApplyTo,
			Match:     cp.Match,
			Operation: cp.Patch.Operation,
		}
		// there wont be an error here because validation catches mismatched types
		cpw.Value, _ = buildXDSObjectFromStruct(cp.ApplyTo, cp.Patch.Value)
		out.ConfigPatches = append(out.ConfigPatches, cpw)
	}
	return out
}

func buildXDSObjectFromStruct(applyTo networking.EnvoyFilter_ApplyTo, value *types.Struct) (proto.Message, error) {
	if value == nil {
		// for remove ops
		return nil, nil
	}
	var obj proto.Message
	switch applyTo {
	case networking.EnvoyFilter_CLUSTER:
		obj = &xdsapi.Cluster{}
	case networking.EnvoyFilter_LISTENER:
		obj = &xdsapi.Listener{}
	case networking.EnvoyFilter_ROUTE_CONFIGURATION:
		obj = &xdsapi.RouteConfiguration{}
	case networking.EnvoyFilter_FILTER_CHAIN:
		obj = &listener.FilterChain{}
	case networking.EnvoyFilter_HTTP_FILTER:
		obj = &http_conn.HttpFilter{}
	case networking.EnvoyFilter_NETWORK_FILTER:
		obj = &listener.Filter{}
	case networking.EnvoyFilter_VIRTUAL_HOST:
		obj = &route.VirtualHost{}
	default:
		return nil, fmt.Errorf("envoy filter: unknown object type for applyTo %s", applyTo.String())
	}

	if err := xdsutil.StructToMessage(value, obj); err != nil {
		return nil, fmt.Errorf("envoy filter: %v", err)
	}
	return obj, nil
}
