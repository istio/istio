// Copyright Istio Authors
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

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/environment"
)

func defaultExitFn(_ int) {}

func settingsFn(s *resource.Settings) func(string) (*resource.Settings, error) {
	return func(testID string) (*resource.Settings, error) {
		s.TestID = testID
		s.BaseDir = os.TempDir()
		return s, nil
	}
}

func defaultSettingsFn(testID string) (*resource.Settings, error) {
	s := resource.DefaultSettings()
	s.TestID = testID
	s.BaseDir = os.TempDir()

	return s, nil
}

func cleanupRT() {
	rtMu.Lock()
	defer rtMu.Unlock()
	rt = nil
}

// Create a bogus environment for testing. This can be removed when "environments" are removed
func newTestSuite(testID string, fn mRunFn, osExit func(int), getSettingsFn getSettingsFunc) *Suite {
	s := newSuite(testID, fn, osExit, getSettingsFn)
	s.envFactory = func(ctx resource.Context) (resource.Environment, error) {
		return fakeEnvironment{}, nil
	}
	return s
}

func TestSuite_Basic(t *testing.T) {
	defer cleanupRT()
	g := NewGomegaWithT(t)

	var runCalled bool
	var runSkipped bool
	var exitCode int
	runFn := func(ctx *suiteContext) int {
		runCalled = true
		runSkipped = ctx.skipped
		return -1
	}
	exitFn := func(code int) {
		exitCode = code
	}

	s := newTestSuite("tid", runFn, exitFn, defaultSettingsFn)
	s.Run()

	g.Expect(runCalled).To(BeTrue())
	g.Expect(runSkipped).To(BeFalse())
	g.Expect(exitCode).To(Equal(-1))
}

func TestSuite_Label_SuiteFilter(t *testing.T) {
	defer cleanupRT()
	g := NewGomegaWithT(t)

	var runSkipped bool
	runFn := func(ctx *suiteContext) int {
		runSkipped = ctx.skipped
		return 0
	}

	sel, err := label.ParseSelector("-customsetup")
	g.Expect(err).To(BeNil())
	settings := resource.DefaultSettings()
	settings.Selector = sel

	s := newTestSuite("tid", runFn, defaultExitFn, settingsFn(settings))
	s.Label(label.CustomSetup)
	s.Run()

	g.Expect(runSkipped).To(BeTrue())
}

func TestSuite_Label_SuiteAllow(t *testing.T) {
	defer cleanupRT()
	g := NewGomegaWithT(t)

	var runCalled bool
	var runSkipped bool
	runFn := func(ctx *suiteContext) int {
		runCalled = true
		runSkipped = ctx.skipped
		return 0
	}

	sel, err := label.ParseSelector("+postsubmit")
	g.Expect(err).To(BeNil())
	settings := resource.DefaultSettings()
	settings.Selector = sel

	s := newTestSuite("tid", runFn, defaultExitFn, settingsFn(settings))
	s.Label(label.CustomSetup)
	s.Run()

	g.Expect(runCalled).To(BeTrue())
	g.Expect(runSkipped).To(BeFalse())
}

func TestSuite_RequireMinMaxClusters(t *testing.T) {
	cases := []struct {
		name       string
		min        int
		max        int
		actual     int
		expectSkip bool
	}{
		{
			name:       "min less than zero",
			min:        0,
			max:        100,
			actual:     1,
			expectSkip: false,
		},
		{
			name:       "less than min",
			min:        2,
			max:        100,
			actual:     1,
			expectSkip: true,
		},
		{
			name:       "equal to min",
			min:        1,
			max:        100,
			actual:     1,
			expectSkip: false,
		},
		{
			name:       "greater than min",
			min:        1,
			max:        100,
			actual:     2,
			expectSkip: false,
		},
		{
			name:       "max less than zero",
			min:        1,
			max:        0,
			actual:     1,
			expectSkip: false,
		},
		{
			name:       "greater than max",
			min:        1,
			max:        1,
			actual:     2,
			expectSkip: true,
		},
		{
			name:       "equal to max",
			min:        1,
			max:        2,
			actual:     2,
			expectSkip: false,
		},
		{
			name:       "less than max",
			min:        1,
			max:        2,
			actual:     1,
			expectSkip: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer cleanupRT()
			g := NewGomegaWithT(t)

			var runCalled bool
			var runSkipped bool
			runFn := func(ctx *suiteContext) int {
				runCalled = true
				runSkipped = ctx.skipped
				return 0
			}

			// Set the kube config flag.
			kubeConfigs := make([]string, c.actual)
			for i := 0; i < c.actual; i++ {
				kubeConfigs[i] = "~/.kube/config"
			}

			settings := resource.DefaultSettings()

			s := newTestSuite("tid", runFn, defaultExitFn, settingsFn(settings))
			s.envFactory = newFakeEnvironmentFactory(c.actual)
			s.RequireMinClusters(c.min)
			s.RequireMaxClusters(c.max)
			s.Run()

			g.Expect(runCalled).To(BeTrue())
			if c.expectSkip {
				g.Expect(runSkipped).To(BeTrue())
			} else {
				g.Expect(runSkipped).To(BeFalse())
			}
		})
	}
}

func TestSuite_Setup(t *testing.T) {
	defer cleanupRT()
	g := NewGomegaWithT(t)

	var runCalled bool
	var runSkipped bool
	runFn := func(ctx *suiteContext) int {
		runCalled = true
		runSkipped = ctx.skipped
		return 0
	}

	s := newTestSuite("tid", runFn, defaultExitFn, defaultSettingsFn)

	var setupCalled bool
	s.Setup(func(c resource.Context) error {
		setupCalled = true
		return nil
	})
	s.Run()

	g.Expect(setupCalled).To(BeTrue())
	g.Expect(runCalled).To(BeTrue())
	g.Expect(runSkipped).To(BeFalse())
}

func TestSuite_SetupFail(t *testing.T) {
	defer cleanupRT()
	g := NewGomegaWithT(t)

	var runCalled bool
	runFn := func(ctx *suiteContext) int {
		runCalled = true
		return 0
	}

	s := newTestSuite("tid", runFn, defaultExitFn, defaultSettingsFn)

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
	runFn := func(_ *suiteContext) int {
		runCalled = true
		return 0
	}

	settings := resource.DefaultSettings()
	settings.CIMode = true

	s := newTestSuite("tid", runFn, defaultExitFn, settingsFn(settings))

	var setupCalled bool
	s.Setup(func(c resource.Context) error {
		setupCalled = true
		return fmt.Errorf("can't run this")
	})
	s.Run()

	g.Expect(setupCalled).To(BeTrue())
	g.Expect(runCalled).To(BeFalse())
}

func TestSuite_DoubleInit_Error(t *testing.T) {
	defer cleanupRT()
	g := NewGomegaWithT(t)

	var waitForRun1 sync.WaitGroup
	waitForRun1.Add(1)
	var waitForTestCompletion sync.WaitGroup
	waitForTestCompletion.Add(1)
	runFn1 := func(ctx *suiteContext) int {
		waitForRun1.Done()
		waitForTestCompletion.Wait()
		return 0
	}

	runFn2 := func(ctx *suiteContext) int {
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

	s := newTestSuite("tid1", runFn1, exitFn1, defaultSettingsFn)

	s2 := newTestSuite("tid2", runFn2, exitFn2, defaultSettingsFn)

	go s.Run()
	waitForRun1.Wait()

	s2.Run()
	waitForTestCompletion.Done()
	waitForExit1Call.Wait()

	g.Expect(exit2Called).To(Equal(true))
	g.Expect(errCode1).To(Equal(0))
	g.Expect(errCode2).NotTo(Equal(0))
}

func TestSuite_GetResource(t *testing.T) {
	defer cleanupRT()

	act := func(refPtr interface{}, trackedResource resource.Resource) error {
		var err error
		runFn := func(ctx *suiteContext) int {
			err = ctx.GetResource(refPtr)
			return 0
		}
		s := newTestSuite("tid", runFn, defaultExitFn, defaultSettingsFn)
		s.Setup(func(c resource.Context) error {
			c.TrackResource(trackedResource)
			return nil
		})
		s.Run()
		return err
	}

	t.Run("struct reference", func(t *testing.T) {
		g := NewGomegaWithT(t)
		var ref *fakeCluster
		tracked := &fakeCluster{index: 1}
		// notice that we pass **fakeCluster:
		// GetResource requires *T where T implements resource.Resource.
		// *fakeCluster implements it but fakeCluster does not.
		err := act(&ref, tracked)
		g.Expect(err).To(BeNil())
		g.Expect(tracked).To(Equal(ref))
	})
	t.Run("interface reference", func(t *testing.T) {
		g := NewGomegaWithT(t)
		var ref resource.Cluster
		tracked := &fakeCluster{index: 1}
		err := act(&ref, tracked)
		g.Expect(err).To(BeNil())
		g.Expect(tracked).To(Equal(ref))
	})
	t.Run("slice reference", func(t *testing.T) {
		g := NewGomegaWithT(t)
		existing := &fakeCluster{index: 1}
		tracked := &fakeCluster{index: 2}
		ref := []resource.Cluster{existing}
		err := act(&ref, tracked)
		g.Expect(err).To(BeNil())
		g.Expect(ref).To(HaveLen(2))
		g.Expect(existing).To(Equal(ref[0]))
		g.Expect(tracked).To(Equal(ref[1]))
	})
	t.Run("non pointer ref", func(t *testing.T) {
		g := NewGomegaWithT(t)
		err := act(fakeCluster{}, &fakeCluster{})
		g.Expect(err).NotTo(BeNil())
	})
}

func newFakeEnvironmentFactory(numClusters int) resource.EnvironmentFactory {
	e := fakeEnvironment{numClusters: numClusters}
	return func(ctx resource.Context) (resource.Environment, error) {
		return e, nil
	}
}

var _ resource.Environment = fakeEnvironment{}

type fakeEnvironment struct {
	numClusters int
}

func (f fakeEnvironment) IsMulticluster() bool {
	return f.numClusters > 1
}

func (f fakeEnvironment) ID() resource.ID {
	return fakeID("fake")
}

func (f fakeEnvironment) EnvironmentName() environment.Name {
	return "fake"
}

func (f fakeEnvironment) Clusters() []resource.Cluster {
	out := make([]resource.Cluster, f.numClusters)
	for i := 0; i < f.numClusters; i++ {
		out[i] = &fakeCluster{index: i}
	}
	return out
}

func (f fakeEnvironment) Case(environment.Name, func()) {
	panic("not implemented")
}

type fakeID string

func (id fakeID) String() string {
	return string(id)
}

var _ resource.Resource = &fakeCluster{}
var _ resource.Cluster = &fakeCluster{}

type fakeCluster struct {
	index int
}

func (f fakeCluster) ApplyConfig(ns string, yamlText ...string) error {
	panic("implement me")
}

func (f fakeCluster) ApplyConfigOrFail(t test.Failer, ns string, yamlText ...string) {
	panic("implement me")
}

func (f fakeCluster) DeleteConfig(ns string, yamlText ...string) error {
	panic("implement me")
}

func (f fakeCluster) DeleteConfigOrFail(t test.Failer, ns string, yamlText ...string) {
	panic("implement me")
}

func (f fakeCluster) ApplyConfigDir(ns string, configDir string) error {
	panic("implement me")
}

func (f fakeCluster) DeleteConfigDir(ns string, configDir string) error {
	panic("implement me")
}

func (f *fakeCluster) Index() resource.ClusterIndex {
	return resource.ClusterIndex(f.index)
}

func (f *fakeCluster) String() string {
	return fmt.Sprintf("fake_cluster_%d", f.index)
}

func (f *fakeCluster) ID() resource.ID {
	return fakeID("fake")
}
