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
	"fmt"
	"log"
	"time"

	tu "istio.io/istio/tests/util"
)

// runnable is used for Testing purposes.
type runnable interface {
	Run() int
}

// IstioTestFramework is core test framework struct
type IstioTestFramework struct {
	TestEnv    TestEnv
	TestID     string
	Components []Component
}

// NewIstioTestFramework create a IstioTestFramework with a given environment and ID
func NewIstioTestFramework(env TestEnv, id string) *IstioTestFramework {
	return &IstioTestFramework{
		TestEnv:    env,
		Components: env.GetComponents(),
		TestID:     id,
	}
}

// SetUp sets up the whole environment as well brings up components
func (framework *IstioTestFramework) SetUp() (err error) {
	if err = framework.TestEnv.Bringup(); err != nil {
		log.Printf("Failed to bring up environment")
		return
	}
	for _, comp := range framework.Components {
		if err := comp.Start(); err != nil {
			log.Printf("Failed to setup component: %s", comp.GetName())
			return err
		}
	}

	if ready, err := framework.IsEnvReady(); err != nil || !ready {
		err = fmt.Errorf("failed to get env ready: %s", err)
		return err
	}

	// TODO: Find more reliable way to tell if local components are ready to serve
	time.Sleep(3 * time.Second)
	return
}

// TearDown stop components and clean up environment
func (framework *IstioTestFramework) TearDown() {
	for _, comp := range framework.Components {
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
	framework.TestEnv.Cleanup()
}

// IsEnvReady check if the whole environment is ready for running tests
func (framework *IstioTestFramework) IsEnvReady() (bool, error) {
	retry := tu.Retrier{
		BaseDelay: 1 * time.Second,
		MaxDelay:  10 * time.Second,
		Retries:   5,
	}

	ready := false
	retryFn := func(i int) error {
		for _, comp := range framework.Components {
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

// RunTest main entry for framework: setup, run tests and clean up
func (framework *IstioTestFramework) RunTest(m runnable) (ret int) {
	if err := framework.SetUp(); err != nil {
		log.Printf("Failed to setup framework: %s", err)
		ret = 1
	} else {
		log.Printf("\nStart testing ......")
		ret = m.Run()
	}
	framework.TearDown()
	return ret
}
