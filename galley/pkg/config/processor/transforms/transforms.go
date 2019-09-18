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

// Package transforms contains basic processing building blocks that can be incorporated into bigger/self-contained
// processing pipelines.
package transforms

import (
	"istio.io/istio/galley/pkg/config/processor/transforms/authpolicy"
	"istio.io/istio/galley/pkg/config/processor/transforms/direct"
	"istio.io/istio/galley/pkg/config/processor/transforms/ingress"
	"istio.io/istio/galley/pkg/config/processor/transforms/provider"
	"istio.io/istio/galley/pkg/config/processor/transforms/serviceentry"
	"istio.io/istio/galley/pkg/config/schema"
)

var allInfos provider.Infos

//GetTransformInfos builds and returns a list of all transformer objects
//TODO: Should any of this be generated based on metadata.yaml?
func GetTransformInfos(m *schema.Metadata) provider.Infos {
	allInfos = make([]*provider.Info, 0)

	allInfos = append(allInfos, serviceentry.GetInfo()...)
	allInfos = append(allInfos, ingress.GetInfo()...)
	allInfos = append(allInfos, direct.GetInfo(m.DirectTransform().Mapping())...)
	allInfos = append(allInfos, authpolicy.GetInfo()...)

	return allInfos
}
