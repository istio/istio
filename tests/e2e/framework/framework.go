// Copyright 2017 Istio Authors
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

package framework

import (
	"flag"
	"io/ioutil"
	"os"
	"sync"

	"github.com/golang/glog"
)

var (
	skipCleanup = flag.Bool("skip_cleanup", false, "Debug, skip clean up")
	logProvider = flag.String("log_provider", "", "Cluster log storage provider")
)

type testCleanup struct {
	Cleanables         []Cleanable
	CleanablesLock     sync.Mutex
	CleanupActions     []func() error
	CleanupActionsLock sync.Mutex
	skipCleanup        bool
}

// CommonConfig regroup all common test configuration.
type CommonConfig struct {
	// Test Cleanup registration
	Cleanup *testCleanup
	// Test Information
	Info *testInfo
	// Kubernetes and istio installation information
	Kube *KubeInfo
}

// Cleanable interfaces that need to be registered to CommonConfig
type Cleanable interface {
	Setup() error
	Teardown() error
}

// Runnable is used for Testing purposes.
type runnable interface {
	Run() int
}

// InitGlog sets the logging directory.
// Should be called right after flag.Parse().
func InitGlog() error {
	tmpDir, err := ioutil.TempDir(os.TempDir(), tmpPrefix)
	if err != nil {
		return err
	}
	f := flag.Lookup("log_dir")
	if err = f.Value.Set(tmpDir); err != nil {
		return err
	}
	glog.Info("Logging initialized")
	return nil
}

// NewCommonConfig creates a full config will all supported configs.
func NewCommonConfig(testID string) (*CommonConfig, error) {
	t, err := newTestInfo(testID)
	if err != nil {
		return nil, err
	}
	k, err := newKubeInfo(t.LogsPath, t.RunID)
	if err != nil {
		return nil, err
	}
	cl := new(testCleanup)
	cl.skipCleanup = *skipCleanup

	c := &CommonConfig{
		Info:    t,
		Kube:    k,
		Cleanup: cl,
	}
	c.Cleanup.RegisterCleanable(c.Info)
	c.Cleanup.RegisterCleanable(c.Kube)
	c.Cleanup.RegisterCleanable(c.Kube.Istioctl)
	c.Cleanup.RegisterCleanable(c.Kube.AppManager)
	return c, nil
}

func (t *testCleanup) RegisterCleanable(c Cleanable) {
	t.CleanablesLock.Lock()
	defer t.CleanablesLock.Unlock()
	t.Cleanables = append(t.Cleanables, c)
}

func (t *testCleanup) popCleanable() Cleanable {
	t.CleanablesLock.Lock()
	defer t.CleanablesLock.Unlock()
	if len(t.Cleanables) == 0 {
		return nil
	}
	c := t.Cleanables[0]
	t.Cleanables = t.Cleanables[1:]
	return c
}

func (t *testCleanup) addCleanupAction(fn func() error) {
	t.CleanupActionsLock.Lock()
	defer t.CleanupActionsLock.Unlock()
	t.CleanupActions = append(t.CleanupActions, fn)
}

func (t *testCleanup) popCleanupAction() func() error {
	t.CleanupActionsLock.Lock()
	defer t.CleanupActionsLock.Unlock()
	if len(t.CleanupActions) == 0 {
		return nil
	}
	fn := t.CleanupActions[len(t.CleanupActions)-1]
	t.CleanupActions = t.CleanupActions[:len(t.CleanupActions)-1]
	return fn
}

func (t *testCleanup) init() error {
	// Run setup on all cleanable
	glog.Info("Starting Initialization")
	c := t.popCleanable()
	for c != nil {
		err := c.Setup()
		t.addCleanupAction(c.Teardown)
		if err != nil {
			return err
		}
		c = t.popCleanable()
	}
	glog.Info("Initialization complete")
	return nil
}

func (t *testCleanup) cleanup() {
	if t.skipCleanup {
		glog.Info("Debug model, skip cleanup")
		return
	}
	// Run tear down on all cleanable
	glog.Info("Starting Cleanup")
	fn := t.popCleanupAction()
	for fn != nil {
		if err := fn(); err != nil {
			glog.Errorf("Failed to cleanup. Error %s", err)
		}
		fn = t.popCleanupAction()
	}
	glog.Info("Cleanup complete")
}

// Save test logs to tmp dir
// Fetch and save cluster tracing logs if logProvider specified
// Logs are uploaded during test tear down
func (c *CommonConfig) saveLogs(r int) error {
	if c.Info == nil {
		glog.Warning("Skipping log saving as Info is not initialized")
		return nil
	}
	if c.Info.LogBucketPath == "" {
		return nil
	}
	glog.Info("Saving logs")
	if err := c.Info.Update(r); err != nil {
		glog.Errorf("Could not create status file. Error %s", err)
		return err
	}
	if r != 0 && *logProvider == "stackdriver" { // fetch logs only if tests failed
		if err := c.Info.FetchAndSaveClusterLogs(c.Kube.Namespace); err != nil {
			return err
		}
	}
	return nil
}

// RunTest sets up all registered cleanables in FIFO order
// Execute the runnable
// Call teardown on all the cleanables in LIFO order.
func (c *CommonConfig) RunTest(m runnable) int {
	var ret int
	if err := c.Cleanup.init(); err != nil {
		glog.Errorf("Failed to complete Init. Error %s", err)
		ret = 1
	} else {
		glog.Info("Running test")
		ret = m.Run()
	}
	if err := c.saveLogs(ret); err != nil {
		glog.Warningf("Log saving incomplete: %v", err)
	}
	c.Cleanup.cleanup()
	return ret
}
