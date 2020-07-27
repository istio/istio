package envoyfilter

import (
	"fmt"
	"github.com/golang/protobuf/proto"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

func newRollupPatch(cp *model.EnvoyFilterConfigPatchWrapper) *model.EnvoyFilterConfigPatchWrapper {
	return &model.EnvoyFilterConfigPatchWrapper{
		Value: proto.Clone(cp.Value),
		Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
			// assume context and proxy matches have already been checked
			Context:              networking.EnvoyFilter_ANY,
			Proxy:                nil,
			ObjectTypes:          cp.Match.ObjectTypes,
			XXX_NoUnkeyedLiteral: struct{}{},
			XXX_unrecognized:     nil,
			XXX_sizecache:        0,
		},
		ApplyTo:   cp.ApplyTo,
		Operation: networking.EnvoyFilter_Patch_MERGE,
	}
}

func doProtoMerge(dst proto.Message, cp *model.EnvoyFilterConfigPatchWrapper) {
	proto.Merge(dst, cp.Value)
}

var mergeFuncByType = map[networking.EnvoyFilter_ApplyTo]func(dst proto.Message, cp *model.EnvoyFilterConfigPatchWrapper){
	networking.EnvoyFilter_CLUSTER:        doProtoMerge,
	networking.EnvoyFilter_LISTENER:       doProtoMerge,
	networking.EnvoyFilter_FILTER_CHAIN:   doProtoMerge,
	networking.EnvoyFilter_NETWORK_FILTER: doNetworkFilterMerge,
	networking.EnvoyFilter_HTTP_FILTER:    doHttpFilterMerge,
}

func rollupPatches(
	patchContext networking.EnvoyFilter_PatchContext,
	allPatches map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper,
) map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper {
	rollup := make(map[networking.EnvoyFilter_ApplyTo][]*model.EnvoyFilterConfigPatchWrapper, len(allPatches))
	for applyTo, patches := range allPatches {
		if _, ok := mergeFuncByType[applyTo]; !ok {
			continue
		}
		if len(patches) < 2 {
			continue
		}

		outPatches := make([]*model.EnvoyFilterConfigPatchWrapper, 0, len(patches))
		byMatch := map[string]*model.EnvoyFilterConfigPatchWrapper{}

		for _, cp := range patches {
			if !commonConditionMatch(patchContext, cp) {
				continue
			}
			if cp.Operation != networking.EnvoyFilter_Patch_MERGE {
				outPatches = append(outPatches, cp)
				continue
			}

			var groupKey string
			if globalMatch(cp) {
				groupKey = "global"
			} else {
				var err error
				if groupKey, err = matcherHash(cp); err != nil {
					// can't perform group by, but we don't want to lose the patch
					outPatches = append(outPatches, cp)
					continue
				}
			}

			rollupPatch, ok := byMatch[groupKey]
			if ok {
				mergeFuncByType[applyTo](rollupPatch.Value, cp)
			} else {
				byMatch[groupKey] = newRollupPatch(cp)
			}
		}
		for _, rollupPatch := range byMatch {
			outPatches = append(outPatches, rollupPatch)
		}
		rollup[applyTo] = outPatches
	}
	return rollup
}

func globalMatch(cp *model.EnvoyFilterConfigPatchWrapper) bool {
	switch cp.ApplyTo {
	case networking.EnvoyFilter_CLUSTER:
		return clusterMatch(nil, cp)
	case networking.EnvoyFilter_LISTENER:
		return listenerMatch(nil, cp)
	case networking.EnvoyFilter_FILTER_CHAIN:
		return listenerMatch(nil, cp) && filterChainMatch(nil, cp)
	case networking.EnvoyFilter_NETWORK_FILTER:
		return listenerMatch(nil, cp) && filterChainMatch(nil, cp) &&
			networkFilterMatch(nil, cp)
	case networking.EnvoyFilter_HTTP_FILTER:
		return listenerMatch(nil, cp) && filterChainMatch(nil, cp) &&
			networkFilterMatch(nil, cp) && httpFilterMatch(nil, cp)
	}
	return false
}

func matcherHash(cp *model.EnvoyFilterConfigPatchWrapper) (string, error) {
	var objectMatcher proto.Message
	switch cp.Match.ObjectTypes.(type) {
	case *networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener:
		objectMatcher = cp.Match.ObjectTypes.(*networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener).Listener
	case *networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster:
		objectMatcher = cp.Match.ObjectTypes.(*networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster).Cluster
	default:
		return "", fmt.Errorf("unsupported object matcher in patch wrapper; got %T", cp.Match.ObjectTypes)
	}

	if objectMatcher == nil {
		return "", fmt.Errorf("empty matcher for patch")
	}

	hash, err := proto.Marshal(objectMatcher)
	if err != nil {
		return "", err
	}

	return string(hash), nil
}
