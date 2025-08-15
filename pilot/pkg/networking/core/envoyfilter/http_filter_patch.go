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

package envoyfilter

import (
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/slices"
)

func patchHTTPFilters(patchContext networking.EnvoyFilter_PatchContext,
	patchType PatchType, patches []*model.EnvoyFilterConfigPatchWrapper,
	filters []*hcm.HttpFilter,
	matchFunc func(*model.EnvoyFilterConfigPatchWrapper) bool,
	getFilterNameMatch func(*model.EnvoyFilterConfigPatchWrapper) string,
) []*hcm.HttpFilter {
	for _, p := range patches {
		applied := false
		if !commonConditionMatch(patchContext, p) ||
			!matchFunc(p) {
			IncrementEnvoyFilterMetric(p.Key(), patchType, false)
			continue
		}
		if p.Operation == networking.EnvoyFilter_Patch_ADD {
			applied = true
			filters = append(filters, proto.Clone(p.Value).(*hcm.HttpFilter))
		} else if p.Operation == networking.EnvoyFilter_Patch_INSERT_FIRST {
			filters = append([]*hcm.HttpFilter{proto.Clone(p.Value).(*hcm.HttpFilter)}, filters...)
		} else if p.Operation == networking.EnvoyFilter_Patch_INSERT_AFTER {
			filterNameMatch := getFilterNameMatch(p)
			// Insert after without a filter match is same as ADD in the end
			if filterNameMatch == "" {
				filters = append(filters, proto.Clone(p.Value).(*hcm.HttpFilter))
				continue
			}
			filters, applied = insertAfterFunc(
				filters,
				func(e *hcm.HttpFilter) (bool, *hcm.HttpFilter) {
					if e.Name == filterNameMatch {
						return true, proto.Clone(p.Value).(*hcm.HttpFilter)
					}
					return false, nil
				},
			)
		} else if p.Operation == networking.EnvoyFilter_Patch_INSERT_BEFORE {
			filterNameMatch := getFilterNameMatch(p)
			// insert before without a filter match is same as insert in the beginning
			if filterNameMatch == "" {
				filters = append([]*hcm.HttpFilter{proto.Clone(p.Value).(*hcm.HttpFilter)}, filters...)
				continue
			}
			filters, applied = insertBeforeFunc(
				filters,
				func(e *hcm.HttpFilter) (bool, *hcm.HttpFilter) {
					if e.Name == filterNameMatch {
						return true, proto.Clone(p.Value).(*hcm.HttpFilter)
					}
					return false, nil
				},
			)
		} else if p.Operation == networking.EnvoyFilter_Patch_REPLACE {
			filterNameMatch := getFilterNameMatch(p)
			if filterNameMatch == "" {
				continue
			}
			filters, applied = replaceFunc(
				filters,
				func(e *hcm.HttpFilter) (bool, *hcm.HttpFilter) {
					if e.Name == filterNameMatch {
						return true, proto.Clone(p.Value).(*hcm.HttpFilter)
					}
					return false, nil
				},
			)
		} else if p.Operation == networking.EnvoyFilter_Patch_REMOVE {
			filterNameMatch := getFilterNameMatch(p)
			if filterNameMatch == "" {
				continue
			}
			filters = slices.FilterInPlace(filters, func(h *hcm.HttpFilter) bool {
				return h.Name != filterNameMatch
			})
		}
		IncrementEnvoyFilterMetric(p.Key(), patchType, applied)
	}
	for _, httpFilter := range filters {
		mergeHTTPFilter(patchContext, patchType, patches, httpFilter, matchFunc, getFilterNameMatch)
	}
	return filters
}

// mergeHTTPFilter patches passed in filter if it is MERGE operation.
func mergeHTTPFilter(patchContext networking.EnvoyFilter_PatchContext,
	patchType PatchType,
	patches []*model.EnvoyFilterConfigPatchWrapper,
	httpFilter *hcm.HttpFilter,
	matchFunc func(*model.EnvoyFilterConfigPatchWrapper) bool,
	getFilterNameMatch func(*model.EnvoyFilterConfigPatchWrapper) string,
) {
	for _, p := range patches {
		applied := false
		filterNameMatch := getFilterNameMatch(p)
		if !commonConditionMatch(patchContext, p) ||
			!matchFunc(p) ||
			filterNameMatch == "" ||
			httpFilter.Name != filterNameMatch {
			IncrementEnvoyFilterMetric(p.Key(), patchType, applied)
			continue
		}
		if p.Operation == networking.EnvoyFilter_Patch_MERGE {
			// proto merge doesn't work well when merging two filters with ANY typed configs
			// especially when the incoming cp.Value is a struct that could contain the json config
			// of an ANY typed filter. So convert our filter's typed config to Struct (retaining the any
			// typed output of json)
			if httpFilter.GetTypedConfig() == nil {
				// TODO(rshriram): fixme
				// skip this op as we would possibly have to do a merge of Any with struct
				// which doesn't seem to work well.
				continue
			}
			userHTTPFilter := p.Value.(*hcm.HttpFilter)
			var err error
			// we need to be able to overwrite filter names or simply empty out a filter's configs
			// as they could be supplied through per route filter configs
			httpFilterName := httpFilter.Name
			if userHTTPFilter.Name != "" {
				httpFilterName = userHTTPFilter.Name
			}
			var retVal *anypb.Any
			if userHTTPFilter.GetTypedConfig() != nil {
				if retVal, err = util.MergeAnyWithAny(httpFilter.GetTypedConfig(), userHTTPFilter.GetTypedConfig()); err != nil {
					retVal = httpFilter.GetTypedConfig()
				}
			}
			applied = true
			httpFilter.Name = httpFilterName
			if retVal != nil {
				httpFilter.ConfigType = &hcm.HttpFilter_TypedConfig{TypedConfig: retVal}
			}
		}
		IncrementEnvoyFilterMetric(p.Key(), patchType, applied)
	}
}
