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

package core

import (
	"flag"
	"fmt"
	"os"

	"istio.io/istio/pkg/test/framework/label"

	"istio.io/istio/pkg/test/framework/components/environment"
)

var (
	settingsFromCommandLine = DefaultSettings()
)

// SettingsFromCommandLine returns settings obtained from command-line flags. flag.Parse must be called before
// calling this function.
func SettingsFromCommandLine(testID string) (*Settings, error) {
	if !flag.Parsed() {
		panic("flag.Parse must be called before this function")
	}

	s := settingsFromCommandLine.Clone()
	s.TestID = testID

	f, err := label.ParseSelector(s.SelectorString)
	if err != nil {
		return nil, err
	}
	s.Selector = f

	return s, nil
}

// init registers the command-line flags that we can exposed for "go test".
func init() {
	flag.StringVar(&settingsFromCommandLine.BaseDir, "istio.test.work_dir", os.TempDir(),
		"Local working directory for creating logs/temp files. If left empty, os.TempDir() is used.")

	flag.StringVar(&settingsFromCommandLine.Environment, "istio.test.env", settingsFromCommandLine.Environment,
		fmt.Sprintf("Specify the environment to run the tests against. Allowed values are: %v", environment.Names()))

	flag.BoolVar(&settingsFromCommandLine.NoCleanup, "istio.test.nocleanup", settingsFromCommandLine.NoCleanup,
		"Do not cleanup resources after test completion")

	flag.BoolVar(&settingsFromCommandLine.CIMode, "istio.test.ci", settingsFromCommandLine.CIMode,
		"Enable CI Mode. Additional logging and state dumping will be enabled.")

	flag.StringVar(&settingsFromCommandLine.SelectorString, "istio.test.select", settingsFromCommandLine.SelectorString,
		"Comma separatated list of labels for selecting tests to run (e.g. 'foo,+bar-baz').")
}
