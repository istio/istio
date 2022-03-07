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

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/config"
	"istio.io/istio/pkg/test/framework/label"
)

var settingsFromCommandLine = DefaultSettings()

// SettingsFromCommandLine returns settings obtained from command-line flags. config.Parse must be called before
// calling this function.
func SettingsFromCommandLine(testID string) (*Settings, error) {
	if !config.Parsed() {
		panic("config.Parse must be called before this function")
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

	// NOTE: not using echo.VM, etc. here to avoid circular dependency.
	if s.SkipVM {
		s.SkipWorkloadClasses = append(s.SkipWorkloadClasses, "vm")
	}
	if s.SkipTProxy {
		s.SkipWorkloadClasses = append(s.SkipWorkloadClasses, "tproxy")
	}
	if s.SkipDelta {
		// TODO we may also want to trigger this if we have an old verion
		s.SkipWorkloadClasses = append(s.SkipWorkloadClasses, "delta")
	}

	if s.Image.Hub == "" {
		s.Image.Hub = env.HUB.ValueOrDefault("gcr.io/istio-testing")
	}

	if s.Image.Tag == "" {
		s.Image.Tag = env.TAG.ValueOrDefault("latest")
	}

	if s.Image.PullPolicy == "" {
		s.Image.PullPolicy = env.PULL_POLICY.ValueOrDefault("Always")
	}

	if err = validate(s); err != nil {
		return nil, err
	}

	return s, nil
}

// validate checks that user has not passed invalid flag combinations to test framework.
func validate(s *Settings) error {
	if s.FailOnDeprecation && s.NoCleanup {
		return fmt.Errorf("checking for deprecation occurs at cleanup level, thus flags -istio.test.nocleanup and" +
			" -istio.test.deprecation_failure must not be used at the same time")
	}

	if s.Revision != "" {
		if s.Revisions != nil {
			return fmt.Errorf("cannot use --istio.test.revision and --istio.test.revisions at the same time," +
				" --istio.test.revisions will take precedence and --istio.test.revision will be ignored")
		}
		// use Revision as the sole revision in RevVerMap
		s.Revisions = RevVerMap{
			s.Revision: "",
		}
	} else if s.Revisions != nil {
		// TODO(Monkeyanator) remove once existing jobs are migrated to use compatibility flag.
		s.Compatibility = true
	}

	if s.Revisions == nil && s.Compatibility {
		return fmt.Errorf("cannot use --istio.test.compatibility without setting --istio.test.revisions")
	}

	if s.Image.Hub == "" || s.Image.Tag == "" {
		return fmt.Errorf("values for Hub & Tag are not detected. Please supply them through command-line or via environment")
	}

	return nil
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

	flag.Var(&settingsFromCommandLine.SkipWorkloadClasses, "istio.test.skipWorkloads",
		"Skips deploying and using workloads of the given comma-separated classes (e.g. vm, proxyless, etc.)")

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

	flag.BoolVar(&settingsFromCommandLine.SkipDelta, "istio.test.skipDelta", settingsFromCommandLine.SkipDelta,
		"Skip Delta XDS related parts in all tests.")

	flag.BoolVar(&settingsFromCommandLine.SkipTProxy, "istio.test.skipTProxy", settingsFromCommandLine.SkipTProxy,
		"Skip TProxy related parts in all tests.")

	flag.BoolVar(&settingsFromCommandLine.Compatibility, "istio.test.compatibility", settingsFromCommandLine.Compatibility,
		"Transparently deploy echo instances pointing to each revision set in `Revisions`")

	flag.Var(&settingsFromCommandLine.Revisions, "istio.test.revisions", "Istio CP revisions available to the test framework and their corresponding versions.")

	flag.StringVar(&settingsFromCommandLine.Image.Hub, "istio.test.hub", settingsFromCommandLine.Image.Hub,
		"Container registry hub to use")
	flag.StringVar(&settingsFromCommandLine.Image.Tag, "istio.test.tag", settingsFromCommandLine.Image.Tag,
		"Common Container tag to use when deploying container images")
	flag.StringVar(&settingsFromCommandLine.Image.PullPolicy, "istio.test.pullpolicy", settingsFromCommandLine.Image.PullPolicy,
		"Common image pull policy to use when deploying container images")
	flag.StringVar(&settingsFromCommandLine.Image.PullSecret, "istio.test.imagePullSecret", settingsFromCommandLine.Image.PullSecret,
		"Path to a file containing a DockerConfig secret use for test apps. This will be pushed to all created namespaces."+
			"Secret should already exist when used with istio.test.stableNamespaces.")
}
