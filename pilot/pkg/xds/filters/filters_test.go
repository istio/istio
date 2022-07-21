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

package filters

import (
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	router "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"

	"istio.io/istio/pilot/pkg/util/protoconv"
)

func TestBuildRouterFilter(t *testing.T) {
	tests := []struct {
		name     string
		ctx      *RouterFilterContext
		expected *hcm.HttpFilter
	}{
		{
			name: "test for build router filter",
			ctx:  &RouterFilterContext{StartChildSpan: true},
			expected: &hcm.HttpFilter{
				Name: wellknown.Router,
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: protoconv.MessageToAny(&router.Router{
						StartChildSpan: true,
					}),
				},
			},
		},
		{
			name: "test for build router filter with start child span false",
			ctx:  &RouterFilterContext{StartChildSpan: false},
			expected: &hcm.HttpFilter{
				Name: wellknown.Router,
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: protoconv.MessageToAny(&router.Router{
						StartChildSpan: false,
					}),
				},
			},
		},
		{
			name: "test for build router filter with empty context",
			ctx:  nil,
			expected: &hcm.HttpFilter{
				Name: wellknown.Router,
				ConfigType: &hcm.HttpFilter_TypedConfig{
					TypedConfig: protoconv.MessageToAny(&router.Router{}),
				},
			},
		},
	}

	for _, tt := range tests {
		result := BuildRouterFilter(tt.ctx)
		if !reflect.DeepEqual(result, tt.expected) {
			t.Errorf("Test %s failed, expected: %v ,got: %v", tt.name, spew.Sdump(result), spew.Sdump(tt.expected))
		}
	}
}
