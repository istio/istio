//  Copyright Istio Authors
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

package framework

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"strings"
	"sync"

	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/util/yml"

	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

// suiteContext contains suite-level items used during runtime.
type SuiteContext interface {
	resource.Context
}

var _ SuiteContext = &suiteContext{}

// suiteContext contains suite-level items used during runtime.
type suiteContext struct {
	settings    *resource.Settings
	environment resource.Environment

	skipped bool

	workDir string
	yml.FileWriter

	// context-level resources
	globalScope *scope

	contextMu    sync.Mutex
	contextNames map[string]struct{}

	suiteLabels label.Set

	outcomeMu    sync.RWMutex
	testOutcomes []TestOutcome
}

func newSuiteContext(s *resource.Settings, envFn resource.EnvironmentFactory, labels label.Set) (*suiteContext, error) {
	scopeID := fmt.Sprintf("[suite(%s)]", s.TestID)

	workDir := path.Join(s.RunDir(), "_suite_context")
	if err := os.MkdirAll(workDir, os.ModePerm); err != nil {
		return nil, err
	}
	c := &suiteContext{
		settings:     s,
		globalScope:  newScope(scopeID, nil),
		workDir:      workDir,
		FileWriter:   yml.NewFileWriter(workDir),
		suiteLabels:  labels,
		contextNames: make(map[string]struct{}),
	}

	env, err := envFn(c)
	if err != nil {
		return nil, err
	}
	c.environment = env
	c.globalScope.add(env, &resourceID{id: scopeID})

	return c, nil
}

// allocateContextID allocates a unique context id for TestContexts. Useful for creating unique names to help with
// debugging
func (s *suiteContext) allocateContextID(prefix string) string {
	s.contextMu.Lock()
	defer s.contextMu.Unlock()

	candidate := prefix
	discriminator := 0
	for {
		if _, found := s.contextNames[candidate]; !found {
			s.contextNames[candidate] = struct{}{}
			return candidate
		}

		candidate = fmt.Sprintf("%s-%d", prefix, discriminator)
		discriminator++
	}
}

func (s *suiteContext) allocateResourceID(contextID string, r resource.Resource) string {
	s.contextMu.Lock()
	defer s.contextMu.Unlock()

	t := reflect.TypeOf(r)
	candidate := fmt.Sprintf("%s/[%s]", contextID, t.String())
	discriminator := 0
	for {
		if _, found := s.contextNames[candidate]; !found {
			s.contextNames[candidate] = struct{}{}
			return candidate
		}

		candidate = fmt.Sprintf("%s/[%s-%d]", contextID, t.String(), discriminator)
		discriminator++
	}
}

// TrackResource adds a new resource to track to the context at this level.
func (s *suiteContext) TrackResource(r resource.Resource) resource.ID {
	id := s.allocateResourceID(s.globalScope.id, r)
	rid := &resourceID{id: id}
	s.globalScope.add(r, rid)
	return rid
}

func (s *suiteContext) GetResource(ref interface{}) error {
	return s.globalScope.get(ref)
}

func (s *suiteContext) Environment() resource.Environment {
	return s.environment
}

func (s *suiteContext) Clusters() resource.Clusters {
	return s.Environment().Clusters()
}

// Settings returns the current runtime.Settings.
func (s *suiteContext) Settings() *resource.Settings {
	return s.settings
}

// CreateDirectory creates a new subdirectory within this context.
func (s *suiteContext) CreateDirectory(name string) (string, error) {
	dir, err := ioutil.TempDir(s.workDir, name)
	if err != nil {
		scopes.Framework.Errorf("Error creating temp dir: runID='%s', prefix='%s', workDir='%v', err='%v'",
			s.settings.RunID, name, s.workDir, err)
	} else {
		scopes.Framework.Debugf("Created a temp dir: runID='%s', name='%s'", s.settings.RunID, dir)
	}
	return dir, err
}

// CreateTmpDirectory creates a new temporary directory with the given prefix.
func (s *suiteContext) CreateTmpDirectory(prefix string) (string, error) {
	if len(prefix) != 0 && !strings.HasSuffix(prefix, "-") {
		prefix += "-"
	}

	dir, err := ioutil.TempDir(s.workDir, prefix)
	if err != nil {
		scopes.Framework.Errorf("Error creating temp dir: runID='%s', prefix='%s', workDir='%v', err='%v'",
			s.settings.RunID, prefix, s.workDir, err)
	} else {
		scopes.Framework.Debugf("Created a temp dir: runID='%s', Name='%s'", s.settings.RunID, dir)
	}

	return dir, err
}

func (s *suiteContext) Config(clusters ...resource.Cluster) resource.ConfigManager {
	return newConfigManager(s, clusters)
}

type Outcome string

const (
	Passed         Outcome = "Passed"
	Failed         Outcome = "Failed"
	Skipped        Outcome = "Skipped"
	NotImplemented Outcome = "NotImplemented"
)

type TestOutcome struct {
	Name          string
	Type          string
	Outcome       Outcome
	FeatureLabels map[features.Feature][]string
}

func (s *suiteContext) registerOutcome(test *testImpl) {
	s.outcomeMu.Lock()
	defer s.outcomeMu.Unlock()
	o := Passed
	if test.notImplemented {
		o = NotImplemented
	} else if test.goTest.Failed() {
		o = Failed
	} else if test.goTest.Skipped() {
		o = Skipped
	}
	newOutcome := TestOutcome{
		Name:          test.goTest.Name(),
		Type:          "integration",
		Outcome:       o,
		FeatureLabels: test.featureLabels,
	}
	s.contextMu.Lock()
	defer s.contextMu.Unlock()
	s.testOutcomes = append(s.testOutcomes, newOutcome)
}
