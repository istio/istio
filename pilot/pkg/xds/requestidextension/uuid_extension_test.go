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
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	uuid_extension "github.com/envoyproxy/go-control-plane/envoy/extensions/request_id/uuid/v3"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/istio/pilot/pkg/networking/util"
)

func TestBuildUUIDRequestIDExtension(t *testing.T) {

	tests := []struct {
		name     string
		ctx      *UUIDRequestIDExtensionContext
		expected *hcm.RequestIDExtension
	}{
		{
			name: "test for build uuid request id with user request id tracing sampling false",
			ctx:  &UUIDRequestIDExtensionContext{UseRequestIDForTraceSampling: false},
			expected: &hcm.RequestIDExtension{
				TypedConfig: util.MessageToAny(&uuid_extension.UuidRequestIdConfig{
					UseRequestIdForTraceSampling: &wrapperspb.BoolValue{
						Value: false,
					},
				}),
			},
		},
		{
			name: "test for build uuid request id with user request id tracing sampling true",
			ctx:  &UUIDRequestIDExtensionContext{UseRequestIDForTraceSampling: true},
			expected: &hcm.RequestIDExtension{
				TypedConfig: util.MessageToAny(&uuid_extension.UuidRequestIdConfig{
					UseRequestIdForTraceSampling: &wrapperspb.BoolValue{
						Value: true,
					},
				}),
			},
		},
		{
			name:     "test for build uuid request id with empty context",
			ctx:      nil,
			expected: UUIDRequestIDExtension,
		},
	}

	for _, tt := range tests {
		result := BuildUUIDRequestIDExtension(tt.ctx)
		if !reflect.DeepEqual(result, tt.expected) {
			t.Errorf("Test %s failed, expected: %v ,got: %v", tt.name, spew.Sdump(result), spew.Sdump(tt.expected))
		}
	}

}
