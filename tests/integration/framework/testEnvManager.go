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
	"fmt"
	"log"
	"time"

	u "istio.io/istio/tests/util"
)

var (
	skipCleanup = flag.Bool("skip_cleanup", false, "Debug, skip clean up")
)

// runnable is used for Testing purposes.
type runnable interface {
	Run() int
}

// TestEnvManager is core test framework struct
type TestEnvManager struct {
	TestEnv     TestEnv
	TestID      string
	Components  []Component
	skipCleanup bool
}

// NewTestEnvManager create a TestEnvManager with a given environment and ID
func NewTestEnvManager(env TestEnv, id string) *TestEnvManager {
	return &TestEnvManager{
		TestEnv:     env,
		Components:  env.GetComponents(),
		TestID:      id,
		skipCleanup: *skipCleanup,
	}
}

// StartUp sets up the whole environment as well brings up components
func (envManager *TestEnvManager) StartUp() (err error) {
	if err = envManager.TestEnv.Bringup(); err != nil {
		log.Printf("Failed to bring up environment")
		return
	}
	for _, comp := range envManager.Components {
		if err := comp.Start(); err != nil {
			log.Printf("Failed to setup component: %s", comp.GetName())
			return err
		}
	}

	if ready, err := envManager.WaitUntilReady(); err != nil || !ready {
		err = fmt.Errorf("failed to get env ready: %s", err)
		return err
	}

	return
}

// TearDown stop components and clean up environment
func (envManager *TestEnvManager) TearDown() {
	if envManager.skipCleanup {
		log.Println("Dev mode (--skip_cleanup), skipping cleanup")
		return
	}

	for _, comp := range envManager.Components {
		if alive, err := comp.IsAlive(); err != nil {
			log.Printf("Failed to check if componment %s is alive: %s", comp.GetName(), err)
		} else if alive {
			if err = comp.Stop(); err != nil {
				log.Printf("Failed to stop componment %s: %s", comp.GetName(), err)
			}
		}
		if err := comp.Cleanup(); err != nil {
			log.Printf("Failed to cleanup %s: %s", comp.GetName(), err)
		}
	}
	_ = envManager.TestEnv.Cleanup()
}

// WaitUntilReady checks and waits until the whole environment is ready
// It retries several time before aborting and throwing error
func (envManager *TestEnvManager) WaitUntilReady() (bool, error) {
	retry := u.Retrier{
		BaseDelay: 1 * time.Second,
		MaxDelay:  10 * time.Second,
		Retries:   5,
	}

	ready := false
	retryFn := func(i int) error {
		for _, comp := range envManager.Components {
			if alive, err := comp.IsAlive(); err != nil {
				return fmt.Errorf("unable to comfirm compoment %s is alive %v", comp.GetName(), err)
			} else if !alive {
				return fmt.Errorf("component %s is not alive", comp.GetName())
			}
		}

		ready = true
		return nil
	}

	_, err := retry.Retry(retryFn)
	return ready, err
}

// RunTest is the main entry for framework: setup, run tests and clean up
func (envManager *TestEnvManager) RunTest(m runnable) (ret int) {
	defer envManager.TearDown()
	if err := envManager.StartUp(); err != nil {
		log.Printf("Failed to setup framework: %s", err)
		ret = 1
	} else {
		log.Printf("\nStart testing ......")
		ret = m.Run()
	}
	return ret
}

// TODO: USE glog instead of log
