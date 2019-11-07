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

package resource

import (
	"istio.io/istio/pkg/test/framework/core"
)

// Context is the core context interface that is used by resources.
type Context interface {
	// TrackResource tracks a resource in this context. If the context is closed, then the resource will be
	// cleaned up.
	TrackResource(r Resource) ID

	// The Environment in which the tests run
	Environment() Environment

	// Settings returns common settings
	Settings() *core.Settings

	// CreateDirectory creates a new subdirectory within this context.
	CreateDirectory(name string) (string, error)

	// CreateTmpDirectory creates a new temporary directory within this context.
	CreateTmpDirectory(prefix string) (string, error)
}
