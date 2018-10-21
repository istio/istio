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

package settings

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/google/uuid"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework/scopes"
)

// EnvironmentID is a unique identifier for a testing environment.
type EnvironmentID string

const (
	// MaxTestIDLength is the maximum length allowed for testID.
	MaxTestIDLength = 30

	// Local environment identifier
	Local = EnvironmentID("local")

	// Kubernetes environment identifier
	Kubernetes = EnvironmentID("kubernetes")
)

var (
	globalSettings = defaultSettings()
)

// Settings is the set of arguments to the test driver.
type Settings struct {
	// Environment to run the tests in. By default, a local environment will be used.
	Environment EnvironmentID

	// TestID is the id of the test suite. This should supplied by the author once, and must be immutable.
	TestID string

	// RunID is the id of the current run.
	RunID string

	// Do not cleanup the resources after the test run.
	NoCleanup bool

	// Local working directory root for creating temporary directories / files in. If left empty,
	// os.TempDir() will be used.
	WorkDir string

	LogOptions *log.Options
}

func (s *Settings) String() string {
	result := ""

	result += fmt.Sprintf("Environment: %s\n", s.Environment)
	result += fmt.Sprintf("TestID:      %s\n", s.TestID)
	result += fmt.Sprintf("RunID:       %s\n", s.RunID)
	result += fmt.Sprintf("NoCleanup:   %v\n", s.NoCleanup)
	result += fmt.Sprintf("WorkDir:     %s\n", s.WorkDir)

	return result
}

// defaultSettings returns a default settings instance.
func defaultSettings() *Settings {
	o := log.DefaultOptions()

	// Disable lab logging for the default run.
	o.SetOutputLevel(scopes.CI.Name(), log.NoneLevel)

	return &Settings{
		Environment: Local,
		LogOptions:  o,
	}
}

// New returns settings built from flags and environment variables.
func New(testID string) (*Settings, error) {
	// Copy the global settings.
	s := &(*globalSettings)

	s.TestID = testID
	s.RunID = generateRunID(testID)

	s.WorkDir = path.Join(s.WorkDir, s.RunID)

	if err := os.Mkdir(s.WorkDir, os.ModePerm); err != nil {
		return nil, err
	}

	if err := s.validate(); err != nil {
		return nil, err
	}

	return s, nil
}

// Validate the arguments.
func (s *Settings) validate() error {
	switch s.Environment {
	case Local, Kubernetes:
	default:
		return fmt.Errorf("unrecognized environment: %q", string(s.Environment))
	}

	if s.TestID == "" || len(s.TestID) > MaxTestIDLength {
		return fmt.Errorf("testID must be non-empty and cannot be longer than %d characters", MaxTestIDLength)
	}

	return nil
}

func generateRunID(testID string) string {
	u := uuid.New().String()
	u = strings.Replace(u, "-", "", -1)
	testID = strings.Replace(testID, "_", "-", -1)
	// We want at least 6 characters of uuid padding
	padding := MaxTestIDLength - len(testID)
	if padding < 0 {
		padding = 0
	}
	return fmt.Sprintf("%s-%s", testID, u[0:padding])
}
