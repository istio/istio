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
	"fmt"
	"path"
	"strings"

	"istio.io/istio/pkg/test/framework/label"

	"istio.io/istio/pkg/test/framework/components/environment"

	"github.com/google/uuid"
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

	// Environment to run the tests in. By default, a local environment will be used.
	Environment string

	// Do not cleanup the resources after the test run.
	NoCleanup bool

	// Indicates that the tests are running in CI Mode
	CIMode bool

	// Local working directory root for creating temporary directories / files in. If left empty,
	// os.TempDir() will be used.
	BaseDir string

	// The label selector that the user has specified.
	SelectorString string

	// The label selector, in parsed form.
	Selector label.Selector
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
		Environment: environment.DefaultName().String(),
		RunID:       uuid.New(),
	}
}

// String implements fmt.Stringer
func (s *Settings) String() string {
	result := ""

	result += fmt.Sprintf("Environment:  %v\n", s.Environment)
	result += fmt.Sprintf("TestID:       %s\n", s.TestID)
	result += fmt.Sprintf("RunID:        %s\n", s.RunID.String())
	result += fmt.Sprintf("NoCleanup:    %v\n", s.NoCleanup)
	result += fmt.Sprintf("BaseDir:      %s\n", s.BaseDir)
	result += fmt.Sprintf("Selector:     %v\n", s.Selector)
	return result
}
