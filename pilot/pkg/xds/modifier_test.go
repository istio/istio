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
	"reflect"
	"testing"

	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	router "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"

	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/xds/filters"
)

func TestModifyFilter(t *testing.T) {
	patterns := []struct {
		name            string
		ctx             *filters.FilterModifierContext
		filtersBefore   []*hcm.HttpFilter
		filtersExpected []*hcm.HttpFilter
	}{
		{
			name: "empty context",
			ctx:  &filters.FilterModifierContext{},
			filtersBefore: []*hcm.HttpFilter{
				filters.Router,
			},
			filtersExpected: []*hcm.HttpFilter{
				filters.Router,
			},
		},
		{
			name: "with filter modifier ctx",
			ctx: &filters.FilterModifierContext{
				RouterFilterModifierCtx: filters.RouterFilterModifierContext{
					EnableStartChildSpan: true,
				},
			},
			filtersBefore: []*hcm.HttpFilter{
				filters.Router,
			},
			filtersExpected: []*hcm.HttpFilter{
				{
					Name: wellknown.Router,
					ConfigType: &hcm.HttpFilter_TypedConfig{
						TypedConfig: util.MessageToAny(&router.Router{
							StartChildSpan: true,
						}),
					},
				},
			},
		},
	}

	for _, pattern := range patterns {
		filters.ModifyFilter(pattern.ctx, pattern.filtersBefore)
		for i, filter := range pattern.filtersBefore {
			if !reflect.DeepEqual(filter, pattern.filtersExpected[i]) {
				t.Errorf("invalid filter modification has been detected got = %v, want = %v", filter, pattern.filtersExpected[i])
			}
		}
	}
}
