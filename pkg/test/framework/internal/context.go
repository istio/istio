//  Copyright 2018 Istio Authors
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

package internal

import (
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/settings"
)

// TestContext provides the ambient context to internal code.
type TestContext struct {
	settings settings.Settings
	impl     environment.Implementation
	Tracker  Tracker
}

var _ environment.ComponentContext = &TestContext{}

// NewTestContext initializes and returns a new instance of TestContext.
func NewTestContext(s settings.Settings, impl environment.Implementation) *TestContext {
	return &TestContext{
		settings: s,
		impl:     impl,
		Tracker:  make(map[dependency.Instance]interface{}),
	}
}

// Settings returns current settings.
func (t *TestContext) Settings() settings.Settings {
	return t.settings
}

// Environment returns current environment implementation.
func (t *TestContext) Environment() environment.Implementation {
	return t.impl
}
