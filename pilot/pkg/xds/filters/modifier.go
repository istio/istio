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
	router "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"istio.io/istio/pilot/pkg/networking/util"
)

type RouterFilterModifierContext struct {
	EnableStartChildSpan bool
}

type FilterModifierContext struct {
	RouterFilterModifierCtx RouterFilterModifierContext
}

func ModifyFilter(ctx *FilterModifierContext, filters []*hcm.HttpFilter) {
	for _, filter := range filters {
		if filter.Name == wellknown.Router {
			routerFilter := router.Router{}
			if err := filter.GetTypedConfig().UnmarshalTo(&routerFilter); err != nil {
				return
			}
			modifyRouterFilter(ctx.RouterFilterModifierCtx, &routerFilter)
			filter.ConfigType = &hcm.HttpFilter_TypedConfig{
				TypedConfig: util.MessageToAny(&routerFilter),
			}
		}
	}
}

func modifyRouterFilter(ctx RouterFilterModifierContext, filter *router.Router) {
	filter.StartChildSpan = ctx.EnableStartChildSpan
}
