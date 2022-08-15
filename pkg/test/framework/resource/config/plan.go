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

package config

import (
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
)

// Plan for configuration that can be applied or deleted as an atomic unit.
// A Plan is initially created by a Factory, but then can continually be
// appended to before finally calling Apply or Delete.
type Plan interface {
	// Factory for appending to this Config.
	Factory

	// Copy returns a deep copy of this Plan.
	Copy() Plan

	// Apply this config.
	Apply(opts ...apply.Option) error
	ApplyOrFail(t test.Failer, opts ...apply.Option)

	// Delete this config.
	Delete() error
	DeleteOrFail(t test.Failer)
}
