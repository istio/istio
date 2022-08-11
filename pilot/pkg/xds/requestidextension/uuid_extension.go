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

package requestidextension

import (
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	uuid_extension "github.com/envoyproxy/go-control-plane/envoy/extensions/request_id/uuid/v3"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/istio/pilot/pkg/util/protoconv"
)

var UUIDRequestIDExtension = &hcm.RequestIDExtension{
	TypedConfig: protoconv.MessageToAny(&uuid_extension.UuidRequestIdConfig{
		UseRequestIdForTraceSampling: &wrapperspb.BoolValue{
			Value: true,
		},
	}),
}

func BuildUUIDRequestIDExtension(ctx *UUIDRequestIDExtensionContext) *hcm.RequestIDExtension {
	if ctx == nil {
		return UUIDRequestIDExtension
	}
	return &hcm.RequestIDExtension{
		TypedConfig: protoconv.MessageToAny(&uuid_extension.UuidRequestIdConfig{
			UseRequestIdForTraceSampling: &wrapperspb.BoolValue{
				Value: ctx.UseRequestIDForTraceSampling,
			},
		}),
	}
}
