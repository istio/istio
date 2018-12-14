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

package runtime

import (
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/scopes"
)

const (
	// MaxTestIDLength is the maximum length allowed for testID.
	MaxTestIDLength = 30
)

var (
	globalSettings = defaultSettings()
)

// settings is the set of arguments to the test driver.
type settings struct {
	// Environment to run the tests in. By default, a local environment will be used.
	Environment component.Variant

	// Do not cleanup the resources after the test run.
	NoCleanup bool

	// Local working directory root for creating temporary directories / files in. If left empty,
	// os.TempDir() will be used.
	WorkDir string

	LogOptions *log.Options
}

// defaultSettings returns a default settings instance.
func defaultSettings() *settings {
	o := log.DefaultOptions()

	// Disable lab logging for the default run.
	o.SetOutputLevel(scopes.CI.Name(), log.NoneLevel)

	return &settings{
		Environment: descriptors.NativeEnvironment.Variant,
		LogOptions:  o,
	}
}
