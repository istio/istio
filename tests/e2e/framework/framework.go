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

	"istio.io/istio/pkg/log"
)

var (
	skipCleanup = flag.Bool("skip_cleanup", false, "Debug, skip clean up")
	// TestVM is true if in this test run user wants to test VM on istio
	TestVM = flag.Bool("test_vm", false, "whether to test VM on istio")
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

// InitLogging sets the logging directory.
// Should be called right after flag.Parse().
func InitLogging() error {
	// Create a temporary directory for any logging files.
	tmpDir, err := ioutil.TempDir(os.TempDir(), tmpPrefix)
	if err != nil {
		return err
	}

	// Configure Istio logging to use a file under the temp dir.
	o := log.DefaultOptions()
	tmpLogFile, err := ioutil.TempFile(tmpDir, tmpPrefix)
	if err != nil {
		return err
	}
	o.OutputPaths = []string{tmpLogFile.Name(), "stdout"}
	if err := log.Configure(o); err != nil {
		return err
	}

	log.Info("Logging initialized")
	return nil
}

// NewCommonConfigWithVersion creates a new CommonConfig with the specified
// version of Istio. If baseVersion is empty, it will use the local head
// version.
func NewCommonConfigWithVersion(testID, version string) (*CommonConfig, error) {
	t, err := newTestInfo(testID)
	if err != nil {
		return nil, err
	}
	k, err := newKubeInfo(t.TempDir, t.RunID, version)
	if err != nil {
		return nil, err
	}
	cl := new(testCleanup)
	cl.skipCleanup = os.Getenv("SKIP_CLEANUP") != "" || *skipCleanup

	c := &CommonConfig{
		Info:    t,
		Kube:    k,
		Cleanup: cl,
	}
	c.Cleanup.RegisterCleanable(c.Info)
	c.Cleanup.RegisterCleanable(c.Kube)
	c.Cleanup.RegisterCleanable(c.Kube.Istioctl)
	c.Cleanup.RegisterCleanable(c.Kube.AppManager)
	if c.Kube.RemoteKubeConfig != "" {
		c.Cleanup.RegisterCleanable(c.Kube.RemoteAppManager)
	}

	return c, nil
}

// NewCommonConfig creates a full config with the local head version.
func NewCommonConfig(testID string) (*CommonConfig, error) {
	return NewCommonConfigWithVersion(testID, "")
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
	log.Info("Starting Initialization")
	c := t.popCleanable()
	for c != nil {
		err := c.Setup()
		t.addCleanupAction(c.Teardown)
		if err != nil {
			return err
		}
		c = t.popCleanable()
	}
	log.Info("Initialization complete")
	return nil
}

func (t *testCleanup) cleanup() {
	if t.skipCleanup {
		log.Info("Dev mode (--skip_cleanup), skipping cleanup (removal of namespace/install)")
		return
	}
	// Run tear down on all cleanable
	log.Info("Starting Cleanup")
	fn := t.popCleanupAction()
	for fn != nil {
		if err := fn(); err != nil {
			log.Errorf("Failed to cleanup. Error %s", err)
		}
		fn = t.popCleanupAction()
	}
	log.Info("Cleanup complete")
}

// Save test logs to tmp dir
// Fetch and save cluster pod logs using kuebctl
// Logs are uploaded during test tear down
func (c *CommonConfig) saveLogs(r int) error {
	// Logs are fetched even if skip_cleanup is called - the namespace is left around.
	if c.Info == nil {
		log.Warn("Skipping log saving as Info is not initialized")
		return nil
	}
	log.Info("Saving logs")
	if err := c.Info.Update(r); err != nil {
		log.Errorf("Could not create status file. Error %s", err)
		return err
	}
	return c.Info.FetchAndSaveClusterLogs(c.Kube.Namespace, c.Kube.KubeConfig)
}

// RunTest sets up all registered cleanables in FIFO order
// Execute the runnable
// Call teardown on all the cleanables in LIFO order.
func (c *CommonConfig) RunTest(m runnable) int {
	var ret int
	if err := c.Cleanup.init(); err != nil {
		log.Errorf("Failed to complete Init. Error %s", err)
		ret = 1
	} else {
		log.Info("Running test")
		ret = m.Run()
	}
	if err := c.saveLogs(ret); err != nil {
		log.Warnf("Log saving incomplete: %v", err)
	}
	c.Cleanup.cleanup()
	return ret
}
