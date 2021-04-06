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

package resource

import (
	"flag"
	"fmt"
	"os"

	"istio.io/istio/pkg/test/framework/label"
)

var settingsFromCommandLine = DefaultSettings()

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

	s.SkipMatcher, err = NewMatcher(s.SkipString)
	if err != nil {
		return nil, err
	}

	if s.FailOnDeprecation && s.NoCleanup {
		return nil,
			fmt.Errorf("checking for deprecation occurs at cleanup level, thus flags -istio.test.nocleanup and" +
				" -istio.test.deprecation_failure must not be used at the same time")
	}

	if s.Revision != "" && s.IstioVersions != nil {
		return nil,
			fmt.Errorf("cannot use --istio.test.revision and --istio.test.versions at the same time," +
				" --istio.test.versions will take precedence and --istio.test.revision will be ignored")
	}

	return s, nil
}

// init registers the command-line flags that we can exposed for "go test".
func init() {
	flag.StringVar(&settingsFromCommandLine.BaseDir, "istio.test.work_dir", os.TempDir(),
		"Local working directory for creating logs/temp files. If left empty, os.TempDir() is used.")

	var env string
	flag.StringVar(&env, "istio.test.env", "", "Deprecated. This flag does nothing")

	flag.BoolVar(&settingsFromCommandLine.NoCleanup, "istio.test.nocleanup", settingsFromCommandLine.NoCleanup,
		"Do not cleanup resources after test completion")

	flag.BoolVar(&settingsFromCommandLine.CIMode, "istio.test.ci", settingsFromCommandLine.CIMode,
		"Enable CI Mode. Additional logging and state dumping will be enabled.")

	flag.StringVar(&settingsFromCommandLine.SelectorString, "istio.test.select", settingsFromCommandLine.SelectorString,
		"Comma separated list of labels for selecting tests to run (e.g. 'foo,+bar-baz').")

	flag.Var(&settingsFromCommandLine.SkipString, "istio.test.skip",
		"Skip tests matching the regular expression. This follows the semantics of -test.run.")

	flag.IntVar(&settingsFromCommandLine.Retries, "istio.test.retries", settingsFromCommandLine.Retries,
		"Number of times to retry tests")

	flag.BoolVar(&settingsFromCommandLine.StableNamespaces, "istio.test.stableNamespaces", settingsFromCommandLine.StableNamespaces,
		"If set, will use consistent namespace rather than randomly generated. Useful with nocleanup to develop tests.")

	flag.BoolVar(&settingsFromCommandLine.FailOnDeprecation, "istio.test.deprecation_failure", settingsFromCommandLine.FailOnDeprecation,
		"Make tests fail if any usage of deprecated stuff (e.g. Envoy flags) is detected.")

	flag.StringVar(&settingsFromCommandLine.Revision, "istio.test.revision", settingsFromCommandLine.Revision,
		"If set to XXX, overwrite the default namespace label (istio-injection=enabled) with istio.io/rev=XXX.")

	flag.BoolVar(&settingsFromCommandLine.SkipVM, "istio.test.skipVM", settingsFromCommandLine.SkipVM,
		"Skip VM related parts in all tests.")

	flag.Var(&settingsFromCommandLine.IstioVersions, "istio.test.versions", "Istio CP versions available to the test framework and their corresponding revisions.")
}

type arrayFlags []string

func (i *arrayFlags) String() string {
	return fmt.Sprint([]string(*i))
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}
