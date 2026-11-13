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

package model

import (
	"strings"

	"google.golang.org/protobuf/proto"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
)

// NeedsPush checks whether a config update event requires an XDS push.
// Returns false only when the spec is unchanged AND no istio.io label or annotation
// value changed, i.e. a pure non-Istio metadata change (Helm annotations, Argo CD labels,
// kubectl.kubernetes.io/last-applied-configuration) that has no effect on XDS state.
func NeedsPush(prev config.Config, curr config.Config) bool {
	if prev.GroupVersionKind != curr.GroupVersionKind {
		// This should never happen.
		return true
	}
	// If the config is not Istio, let us just push.
	if !strings.HasSuffix(prev.GroupVersionKind.Group, "istio.io") {
		return true
	}
	if alwaysPush(prev.Meta) || alwaysPush(curr.Meta) {
		return true
	}
	// istio.io labels/annotations can change behavior, push only if one of their values changed
	if istioMetadataChanged(prev.Meta.Labels, curr.Meta.Labels) ||
		istioMetadataChanged(prev.Meta.Annotations, curr.Meta.Annotations) {
		return true
	}
	prevspecProto, okProtoP := prev.Spec.(proto.Message)
	currspecProto, okProtoC := curr.Spec.(proto.Message)
	if okProtoP && okProtoC {
		return !proto.Equal(prevspecProto, currspecProto)
	}
	return true
}

// istioMetadataChanged returns true if any key containing istio.io was added, removed or changed.
func istioMetadataChanged(prev, curr map[string]string) bool {
	n := 0
	for k, v := range curr {
		if strings.Contains(k, "istio.io") {
			n++
			if pv, f := prev[k]; !f || pv != v {
				return true
			}
		}
	}
	for k := range prev {
		if strings.Contains(k, "istio.io") {
			n--
		}
	}
	return n != 0
}

func alwaysPush(m config.Meta) bool {
	_, l := m.Labels[constants.AlwaysPushLabel]
	_, a := m.Annotations[constants.AlwaysPushLabel]
	return l || a
}
