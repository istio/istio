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

// Package transforms contains basic processing building blocks that can be incorporated into bigger/self-contained
// processing pipelines.
package transforms

import (
	"istio.io/istio/galley/pkg/config/processing/transformer"
	"istio.io/istio/galley/pkg/config/processor/transforms/direct"
	"istio.io/istio/pkg/config/schema"
)

//Providers builds and returns a list of all transformer objects
func Providers(m *schema.Metadata) transformer.Providers {
	providers := make([]transformer.Provider, 0)

	providers = append(providers, direct.GetProviders(m)...)

	return providers
}
