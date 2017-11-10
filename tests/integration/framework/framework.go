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
	"time"
	"log"
	"fmt"

	tu "istio.io/istio/tests/util"
)

// Runnable is used for Testing purposes.
type runnable interface {
	Run() int
}

type IstioTestFramework struct {
	TestEnv TestEnv
	TestID string
	Components []Component
}

func NewIstioTestFramework(env TestEnv, id string) *IstioTestFramework {
	return &IstioTestFramework{
		TestEnv: env,
		Components: env.GetComponents(),
		TestID: id,
	}
}

func (framework *IstioTestFramework) SetUp() error {
	for _, comp := range framework.Components {
		if err := comp.Start(); err != nil {
			log.Printf("Failed to setup environement")
			return err
		}
	}

	if ready, err := framework.IsEnvReady(); (err != nil || !ready) {
		return fmt.Errorf("failed to get env ready: %s", err)
	}

	// TODO: Find more reliable way to tell if local components are ready to serve
	time.Sleep(3 * time.Second)
	return nil
}

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

func (framework *IstioTestFramework) IsEnvReady() (bool, error) {
	retry := tu.Retrier{
		BaseDelay:  1 * time.Second,
		MaxDelay: 10 * time.Second,
		Retries: 5,
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
