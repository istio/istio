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
	"os"
	"path"
	"reflect"
	"strings"
	"sync"

	"go.uber.org/atomic"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config"
	"istio.io/istio/pkg/test/framework/resource/config/cleanup"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/yml"
	"istio.io/istio/pkg/util/sets"
)

// SuiteContext contains suite-level items used during runtime.
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
	contextNames sets.Set

	suiteLabels label.Set

	outcomeMu    sync.RWMutex
	testOutcomes []TestOutcome

	dumpCount *atomic.Uint64

	traces sync.Map
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
		contextNames: sets.New(),
		dumpCount:    atomic.NewUint64(0),
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
func (c *suiteContext) allocateContextID(prefix string) string {
	c.contextMu.Lock()
	defer c.contextMu.Unlock()

	candidate := prefix
	discriminator := 0
	for {
		if !c.contextNames.Contains(candidate) {
			c.contextNames.Insert(candidate)
			return candidate
		}

		candidate = fmt.Sprintf("%s-%d", prefix, discriminator)
		discriminator++
	}
}

func (c *suiteContext) allocateResourceID(contextID string, r resource.Resource) string {
	c.contextMu.Lock()
	defer c.contextMu.Unlock()

	t := reflect.TypeOf(r)
	candidate := fmt.Sprintf("%s/[%s]", contextID, t.String())
	discriminator := 0
	for {
		if !c.contextNames.Contains(candidate) {
			c.contextNames.Insert(candidate)
			return candidate
		}

		candidate = fmt.Sprintf("%s/[%s-%d]", contextID, t.String(), discriminator)
		discriminator++
	}
}

func (c *suiteContext) CleanupConditionally(fn func()) {
	c.CleanupStrategy(cleanup.Conditionally, fn)
}

func (c *suiteContext) Cleanup(fn func()) {
	c.CleanupStrategy(cleanup.Always, fn)
}

func (c *suiteContext) CleanupStrategy(strategy cleanup.Strategy, fn func()) {
	switch strategy {
	case cleanup.Always:
		c.globalScope.addCloser(&closer{fn: func() error {
			fn()
			return nil
		}})
	case cleanup.Conditionally:
		c.globalScope.addCloser(&closer{fn: func() error {
			fn()
			return nil
		}, noskip: true})
	default:
		// No cleanup.
		return
	}
}

// TrackResource adds a new resource to track to the context at this level.
func (c *suiteContext) TrackResource(r resource.Resource) resource.ID {
	id := c.allocateResourceID(c.globalScope.id, r)
	rid := &resourceID{id: id}
	c.globalScope.add(r, rid)
	return rid
}

func (c *suiteContext) GetResource(ref any) error {
	return c.globalScope.get(ref)
}

func (c *suiteContext) Environment() resource.Environment {
	return c.environment
}

func (c *suiteContext) Clusters() cluster.Clusters {
	return c.Environment().Clusters()
}

func (c *suiteContext) AllClusters() cluster.Clusters {
	return c.Environment().AllClusters()
}

// Settings returns the current runtime.Settings.
func (c *suiteContext) Settings() *resource.Settings {
	return c.settings
}

// CreateDirectory creates a new subdirectory within this context.
func (c *suiteContext) CreateDirectory(name string) (string, error) {
	dir, err := os.MkdirTemp(c.workDir, name)
	if err != nil {
		scopes.Framework.Errorf("Error creating temp dir: runID='%s', prefix='%s', workDir='%v', err='%v'",
			c.settings.RunID, name, c.workDir, err)
	} else {
		scopes.Framework.Debugf("Created a temp dir: runID='%s', name='%s'", c.settings.RunID, dir)
	}
	return dir, err
}

// CreateTmpDirectory creates a new temporary directory with the given prefix.
func (c *suiteContext) CreateTmpDirectory(prefix string) (string, error) {
	if len(prefix) != 0 && !strings.HasSuffix(prefix, "-") {
		prefix += "-"
	}

	dir, err := os.MkdirTemp(c.workDir, prefix)
	if err != nil {
		scopes.Framework.Errorf("Error creating temp dir: runID='%s', prefix='%s', workDir='%v', err='%v'",
			c.settings.RunID, prefix, c.workDir, err)
	} else {
		scopes.Framework.Debugf("Created a temp dir: runID='%s', Name='%s'", c.settings.RunID, dir)
	}

	return dir, err
}

func (c *suiteContext) ConfigKube(clusters ...cluster.Cluster) config.Factory {
	return newConfigFactory(c, clusters)
}

func (c *suiteContext) ConfigIstio() config.Factory {
	return newConfigFactory(c, c.Clusters().Configs())
}

// RequestTestDump is called by the test context for a failed test to request a full
// dump of the system state. Returns true if the dump may proceed, or false if the
// maximum number of dumps has been exceeded for this suite.
func (c *suiteContext) RequestTestDump() bool {
	return c.dumpCount.Inc() < c.settings.MaxDumps
}

func (c *suiteContext) ID() string {
	return c.globalScope.id
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

func (c *suiteContext) registerOutcome(test *testImpl) {
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
	c.contextMu.Lock()
	defer c.contextMu.Unlock()
	c.testOutcomes = append(c.testOutcomes, newOutcome)
}

func (c *suiteContext) RecordTraceEvent(key string, value any) {
	c.traces.Store(key, value)
}

func (c *suiteContext) marshalTraceEvent() []byte {
	kvs := map[string]any{}
	c.traces.Range(func(key, value any) bool {
		kvs[key.(string)] = value
		return true
	})
	outer := map[string]any{
		fmt.Sprintf("suite/%s", c.settings.TestID): kvs,
	}
	d, _ := yaml.Marshal(outer)
	return d
}
