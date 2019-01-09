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
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/google/uuid"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/framework/runtime/dependency"
	"istio.io/istio/pkg/test/framework/runtime/registries"
	"istio.io/istio/pkg/test/framework/runtime/registry"
	"istio.io/istio/pkg/test/scopes"
)

var _ context.Instance = &contextImpl{}
var _ io.Closer = &contextImpl{}
var _ api.Resettable = &contextImpl{}

type contextImpl struct {
	component.Repository
	component.Factory
	component.Defaults

	testID     string
	runID      string
	noCleanup  bool
	workDir    string
	logOptions *log.Options
	registry   *registry.Instance
	depManager *dependency.Manager
}

func newContext(testID string) (*contextImpl, error) {
	if testID == "" || len(testID) > MaxTestIDLength {
		return nil, fmt.Errorf("testID must be non-empty and cannot be longer than %d characters", MaxTestIDLength)
	}

	// Copy the global settings.
	s := &(*globalSettings)

	runID := generateRunID(testID)
	workDir := path.Join(s.WorkDir, runID)

	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		if err := os.MkdirAll(workDir, os.ModePerm); err != nil {
			return nil, err
		}
	}

	r := registries.ForEnvironment(s.Environment)
	if r == nil {
		return nil, fmt.Errorf("unsupported environment: %v", s.Environment)
	}

	ctx := &contextImpl{
		testID:     testID,
		runID:      runID,
		workDir:    workDir,
		Defaults:   r,
		registry:   r,
		logOptions: s.LogOptions,
	}

	// Create the dependency manager.
	depMgr := dependency.NewManager(ctx, r)
	ctx.depManager = depMgr
	ctx.Repository = depMgr
	ctx.Factory = depMgr

	return ctx, nil
}

func (c *contextImpl) TestID() string {
	return c.testID
}

func (c *contextImpl) RunID() string {
	return c.runID
}

func (c *contextImpl) NoCleanup() bool {
	return c.noCleanup
}

func (c *contextImpl) WorkDir() string {
	return c.workDir
}

func (c *contextImpl) LogOptions() *log.Options {
	return c.logOptions
}

func (c *contextImpl) CreateTmpDirectory(prefix string) (string, error) {
	dir, err := ioutil.TempDir(c.workDir, prefix)
	if err != nil {
		scopes.Framework.Errorf("Error creating temp dir: runID='%s', prefix='%s', workDir='%v', err='%v'",
			c.runID, prefix, c.workDir, err)
	} else {
		scopes.Framework.Debugf("Created a temp dir: runID='%s', name='%s'", c.runID, dir)
	}

	return dir, err
}

func (c *contextImpl) Require(scope lifecycle.Scope, reqs ...component.Requirement) component.RequirementError {
	err := c.depManager.Require(scope, reqs...)
	if err != nil && err.IsStartError() {
		c.DumpState(c.RunID())
	}
	return err
}

func (c *contextImpl) RequireOrFail(t testing.TB, scope lifecycle.Scope, reqs ...component.Requirement) {
	t.Helper()
	if err := c.Require(scope, reqs...); err != nil {
		t.Fatal(err)
	}
}

func (c *contextImpl) RequireOrSkip(t testing.TB, scope lifecycle.Scope, reqs ...component.Requirement) {
	t.Helper()
	if err := c.Require(scope, reqs...); err != nil {
		if err.IsStartError() {
			t.Fatal(err)
		} else {
			t.Skipf("Missing requirement: %v", err)
		}
	}
}

func (c *contextImpl) DumpState(contextStr string) {
	e := api.GetEnvironment(c)
	if e != nil {
		e.DumpState(contextStr)
	}
}

// TODO(nmittler): Remove this.
func (c *contextImpl) Evaluate(t testing.TB, tmpl string) string {
	e := api.GetEnvironment(c)
	if e == nil {
		t.Fatal("environment unavailable")
	}

	out, err := e.Evaluate(tmpl)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (c *contextImpl) Reset() (err error) {
	if c.depManager != nil {
		err = c.depManager.Reset()
	}
	return err
}

func (c *contextImpl) Close() (err error) {
	// Close all of the components.
	if c.depManager != nil {
		err = c.depManager.Close()
		c.Repository = nil
		c.Factory = nil
	}
	return
}

func (c *contextImpl) String() string {
	result := ""

	result += fmt.Sprintf("Environment: %v\n", globalSettings.Environment)
	result += fmt.Sprintf("TestID:      %s\n", c.testID)
	result += fmt.Sprintf("RunID:       %s\n", c.runID)
	result += fmt.Sprintf("NoCleanup:   %v\n", c.noCleanup)
	result += fmt.Sprintf("WorkDir:     %s\n", c.workDir)

	return result
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
