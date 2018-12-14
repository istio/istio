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

package context

import (
	"testing"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework/api/component"
)

// Instance of a testing context.
type Instance interface {
	component.Repository
	component.Factory
	component.Resolver
	component.Defaults

	// TestID is the id of the test suite. This should supplied by the author once, and must be immutable.
	TestID() string

	// RunID is the id of the current run.
	RunID() string

	// Do not cleanup the resources after the test run.
	NoCleanup() bool

	// Local working directory root for creating temporary directories / files in. If left empty,
	// os.TempDir() will be used.
	WorkDir() string

	// CreateTmpDirectory creates a temporary directory for running local programs, or storing logs.
	// By default, the root of the tmp dir will be established by os.TempDir(). If workdir flag is specified,
	// it will be used instead. The directory will be of the form <root>/<runID>/<name>/.
	CreateTmpDirectory(name string) (string, error)

	LogOptions() *log.Options

	DumpState(context string)

	// TODO(nmittler): Remove this.
	// Evaluate the given template with environment specific template variables.
	Evaluate(t testing.TB, tmpl string) string
}
