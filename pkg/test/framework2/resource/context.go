//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package resource

import (
	"istio.io/istio/pkg/test/framework2/common"
	"istio.io/istio/pkg/test/framework2/components/environment"
)

// Context is an interface that is used by resources.
type Context interface {
	// TrackResource tracks a resource in this context. If the context is closed, then the resource will be
	// cleaned up.
	TrackResource(r interface{})

	// The Environment in whcih the tests run
	Environment() environment.Instance

	// Settings returns common settings
	Settings() *common.Settings

	// CreateTmpDirectory creates a new temporary direcoty within this context.
	CreateTmpDirectory(prefix string) (string, error)
}
