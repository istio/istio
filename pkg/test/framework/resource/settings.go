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
	"fmt"
	"path"
	"strings"

	"github.com/google/uuid"

	"istio.io/istio/pkg/test/framework/label"
)

const (
	// maxTestIDLength is the maximum length allowed for testID.
	maxTestIDLength = 30
)

// Settings is the set of arguments to the test driver.
type Settings struct {
	// Name of the test
	TestID string

	RunID uuid.UUID

	// Do not cleanup the resources after the test run.
	NoCleanup bool

	// Indicates that the tests are running in CI Mode
	CIMode bool

	// Should the tests fail if usage of deprecated stuff (e.g. Envoy flags) is detected
	FailOnDeprecation bool

	// Local working directory root for creating temporary directories / files in. If left empty,
	// os.TempDir() will be used.
	BaseDir string

	// The number of times to retry failed tests.
	// This should not be depended on as a primary means for reducing test flakes.
	Retries int

	// If enabled, namespaces will be reused rather than created with dynamic names each time.
	// This is useful when combined with NoCleanup, to allow quickly iterating on tests.
	StableNamespaces bool

	// The label selector that the user has specified.
	SelectorString string

	// The label selector, in parsed form.
	Selector label.Selector

	// EnvironmentFactory allows caller to override the environment creation. If nil, a default is used based
	// on the known environment names.
	EnvironmentFactory EnvironmentFactory
}

// RunDir is the name of the dir to output, for this particular run.
func (s *Settings) RunDir() string {
	u := strings.Replace(s.RunID.String(), "-", "", -1)
	t := strings.Replace(s.TestID, "_", "-", -1)
	// We want at least 6 characters of uuid padding
	padding := maxTestIDLength - len(t)
	if padding < 0 {
		padding = 0
	}
	n := fmt.Sprintf("%s-%s", t, u[0:padding])

	return path.Join(s.BaseDir, n)
}

// Clone settings
func (s *Settings) Clone() *Settings {
	cl := *s
	return &cl
}

// DefaultSettings returns a default settings instance.
func DefaultSettings() *Settings {
	return &Settings{
		RunID: uuid.New(),
	}
}

// String implements fmt.Stringer
func (s *Settings) String() string {
	result := ""

	result += fmt.Sprintf("TestID:            %s\n", s.TestID)
	result += fmt.Sprintf("RunID:             %s\n", s.RunID.String())
	result += fmt.Sprintf("NoCleanup:         %v\n", s.NoCleanup)
	result += fmt.Sprintf("BaseDir:           %s\n", s.BaseDir)
	result += fmt.Sprintf("Selector:          %v\n", s.Selector)
	result += fmt.Sprintf("FailOnDeprecation: %v\n", s.FailOnDeprecation)
	return result
}
