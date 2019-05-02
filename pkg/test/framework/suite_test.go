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

package framework

import (
	"fmt"
	"os"
	"sync"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/core"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

func defaultExitFn(int) {}

func settingsFn(s *core.Settings) func(string) (*core.Settings, error) {
	return func(testID string) (*core.Settings, error) {
		s.TestID = testID
		s.BaseDir = os.TempDir()
		return s, nil
	}
}

func defaultSettingsFn(testID string) (*core.Settings, error) {
	s := core.DefaultSettings()
	s.TestID = testID
	s.BaseDir = os.TempDir()

	return s, nil
}

func cleanupRT() {
	rtMu.Lock()
	defer rtMu.Unlock()
	rt = nil
}

func TestSuite_Basic(t *testing.T) {
	defer cleanupRT()
	g := NewGomegaWithT(t)

	var runCalled bool
	var exitCode int
	runFn := func() int {
		runCalled = true
		return -1
	}
	exitFn := func(code int) {
		exitCode = code
	}

	s := newSuite("tid", runFn, exitFn, defaultSettingsFn)
	s.Run()

	g.Expect(runCalled).To(BeTrue())
	g.Expect(exitCode).To(Equal(-1))
}

func TestSuite_Label_SuiteFilter(t *testing.T) {
	defer cleanupRT()
	g := NewGomegaWithT(t)

	var runCalled bool
	runFn := func() int {
		runCalled = true
		return 0
	}

	sel, err := label.ParseSelector("-presubmit")
	g.Expect((err)).To(BeNil())
	settings := core.DefaultSettings()
	settings.Selector = sel

	s := newSuite("tid", runFn, defaultExitFn, settingsFn(settings))
	s.Label(label.Presubmit)
	s.Run()

	g.Expect(runCalled).To(BeFalse())
}

func TestSuite_Label_SuiteAllow(t *testing.T) {
	defer cleanupRT()
	g := NewGomegaWithT(t)

	var runCalled bool
	runFn := func() int {
		runCalled = true
		return 0
	}

	sel, err := label.ParseSelector("+postsubmit")
	g.Expect((err)).To(BeNil())
	settings := core.DefaultSettings()
	settings.Selector = sel

	s := newSuite("tid", runFn, defaultExitFn, settingsFn(settings))
	s.Label(label.Presubmit)
	s.Run()

	g.Expect(runCalled).To(BeTrue())
}

func TestSuite_RequireEnvironment(t *testing.T) {
	defer cleanupRT()
	g := NewGomegaWithT(t)

	var runCalled bool
	runFn := func() int {
		runCalled = true
		return 0
	}

	settings := core.DefaultSettings()
	settings.Environment = environment.Native.String()

	s := newSuite("tid", runFn, defaultExitFn, settingsFn(settings))
	s.RequireEnvironment(environment.Kube)
	s.Run()

	g.Expect(runCalled).To(BeFalse())
}

func TestSuite_RequireEnvironment_Match(t *testing.T) {
	defer cleanupRT()
	g := NewGomegaWithT(t)

	var runCalled bool
	runFn := func() int {
		runCalled = true
		return 0
	}

	settings := core.DefaultSettings()
	settings.Environment = environment.Native.String()

	s := newSuite("tid", runFn, defaultExitFn, settingsFn(settings))
	s.RequireEnvironment(environment.Native)
	s.Run()

	g.Expect(runCalled).To(BeTrue())
}

func TestSuite_Setup(t *testing.T) {
	defer cleanupRT()
	g := NewGomegaWithT(t)

	var runCalled bool
	runFn := func() int {
		runCalled = true
		return 0
	}

	s := newSuite("tid", runFn, defaultExitFn, defaultSettingsFn)

	var setupCalled bool
	s.Setup(func(c resource.Context) error {
		setupCalled = true
		return nil
	})
	s.Run()

	g.Expect(setupCalled).To(BeTrue())
	g.Expect(runCalled).To(BeTrue())
}

func TestSuite_SetupFail(t *testing.T) {
	defer cleanupRT()
	g := NewGomegaWithT(t)

	var runCalled bool
	runFn := func() int {
		runCalled = true
		return 0
	}

	s := newSuite("tid", runFn, defaultExitFn, defaultSettingsFn)

	var setupCalled bool
	s.Setup(func(c resource.Context) error {
		setupCalled = true
		return fmt.Errorf("can't run this")
	})
	s.Run()

	g.Expect(setupCalled).To(BeTrue())
	g.Expect(runCalled).To(BeFalse())
}

func TestSuite_SetupFail_Dump(t *testing.T) {
	defer cleanupRT()
	g := NewGomegaWithT(t)

	var runCalled bool
	runFn := func() int {
		runCalled = true
		return 0
	}

	settings := core.DefaultSettings()
	settings.CIMode = true

	s := newSuite("tid", runFn, defaultExitFn, settingsFn(settings))

	var setupCalled bool
	s.Setup(func(c resource.Context) error {
		setupCalled = true
		return fmt.Errorf("can't run this")
	})
	s.Run()

	g.Expect(setupCalled).To(BeTrue())
	g.Expect(runCalled).To(BeFalse())
}

func TestSuite_SetupOnEnv(t *testing.T) {
	defer cleanupRT()
	g := NewGomegaWithT(t)

	var runCalled bool
	runFn := func() int {
		runCalled = true
		return 0
	}

	settings := core.DefaultSettings()
	settings.Environment = environment.Native.String()

	s := newSuite("tid", runFn, defaultExitFn, settingsFn(settings))

	var setupCalled bool
	s.SetupOnEnv(environment.Native, func(c resource.Context) error {
		setupCalled = true
		return nil
	})
	s.Run()

	g.Expect(setupCalled).To(BeTrue())
	g.Expect(runCalled).To(BeTrue())
}

func TestSuite_SetupOnEnv_Mismatch(t *testing.T) {
	defer cleanupRT()
	g := NewGomegaWithT(t)

	var runCalled bool
	runFn := func() int {
		runCalled = true
		return 0
	}

	settings := core.DefaultSettings()
	settings.Environment = environment.Native.String()

	s := newSuite("tid", runFn, defaultExitFn, settingsFn(settings))

	var setupCalled bool
	s.SetupOnEnv(environment.Kube, func(c resource.Context) error {
		setupCalled = true
		return nil
	})
	s.Run()

	g.Expect(setupCalled).To(BeFalse())
	g.Expect(runCalled).To(BeTrue())
}

func TestSuite_DoubleInit_Error(t *testing.T) {
	defer cleanupRT()
	g := NewGomegaWithT(t)

	var waitForRun1 sync.WaitGroup
	waitForRun1.Add(1)
	var waitForTestCompletion sync.WaitGroup
	waitForTestCompletion.Add(1)
	runFn1 := func() int {
		waitForRun1.Done()
		waitForTestCompletion.Wait()
		return 0
	}

	runFn2 := func() int {
		return 0
	}

	var waitForExit1Call sync.WaitGroup
	waitForExit1Call.Add(1)
	var errCode1 int
	exitFn1 := func(errCode int) {
		errCode1 = errCode
		waitForExit1Call.Done()
	}

	var exit2Called bool
	var errCode2 int
	exitFn2 := func(errCode int) {
		exit2Called = true
		errCode2 = errCode
	}

	s := newSuite("tid1", runFn1, exitFn1, defaultSettingsFn)

	s2 := newSuite("tid2", runFn2, exitFn2, defaultSettingsFn)

	go s.Run()
	waitForRun1.Wait()

	s2.Run()
	waitForTestCompletion.Done()
	waitForExit1Call.Wait()

	g.Expect(exit2Called).To(Equal(true))
	g.Expect(errCode1).To(Equal(0))
	g.Expect(errCode2).NotTo(Equal(0))
}
