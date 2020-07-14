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

package processing

import (
	"istio.io/api/mesh/v1alpha1"

	"istio.io/istio/pkg/config/event"
)

// ProcessorOptions are options that are passed to event.Processors during startup.
type ProcessorOptions struct {
	MeshConfig   *v1alpha1.MeshConfig
	DomainSuffix string
}

// ProcessorProvider returns a new Processor instance for the given ProcessorOptions.
type ProcessorProvider func(o ProcessorOptions) event.Processor
