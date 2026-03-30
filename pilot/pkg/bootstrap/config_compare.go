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

package bootstrap

import (
	"strings"

	"google.golang.org/protobuf/proto"

	"istio.io/istio/pkg/config"
)

// needsPush checks whether the passed in config has same spec and hence push needs
// to be triggered. This is to avoid unnecessary pushes only when labels have changed
// for example.
func needsPush(prev config.Config, curr config.Config) bool {
	if prev.GroupVersionKind != curr.GroupVersionKind {
		// This should never happen.
		return true
	}
	// If the config is not Istio, let us just push.
	if !strings.HasSuffix(prev.GroupVersionKind.Group, "istio.io") {
		return true
	}
	// If current/previous metadata has "*istio.io" label/annotation, just push
	for label := range curr.Meta.Labels {
		if strings.Contains(label, "istio.io") {
			return true
		}
	}
	for annotation := range curr.Meta.Annotations {
		if strings.Contains(annotation, "istio.io") {
			return true
		}
	}
	for label := range prev.Meta.Labels {
		if strings.Contains(label, "istio.io") {
			return true
		}
	}
	for annotation := range prev.Meta.Annotations {
		if strings.Contains(annotation, "istio.io") {
			return true
		}
	}
	prevspecProto, okProtoP := prev.Spec.(proto.Message)
	currspecProto, okProtoC := curr.Spec.(proto.Message)
	if okProtoP && okProtoC {
		return !proto.Equal(prevspecProto, currspecProto)
	}
	return true
}
