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

package integration

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
)

func TestSynchronous(t *testing.T) {
	// Root is always run sync.
	newLevels().
		// Level 1: bf=5(sync)
		Add(5, false).
		// Level 2: bf=2(sync)
		Add(2, false).
		// Level 3: bf=2(sync)
		Add(2, false).
		Build().
		Run(t)
}

func TestParallel(t *testing.T) {
	// Root is always run sync.
	newLevels().
		// Level 1: bf=20(parallel)
		Add(20, true).
		// Level 2: bf=5(parallel)
		Add(5, true).
		// Level 3: bf=2(parallel)
		Add(2, true).
		Build().
		Run(t)
}

func TestMix(t *testing.T) {
	// Root is always run sync.
	newLevels().
		// Level 1: bf=20(parallel)
		Add(20, true).
		// Level 2: bf=5(sync)
		Add(5, false).
		// Level 3: bf=2(parallel)
		Add(2, true).
		Build().
		Run(t)
}

func newLevels() levels {
	return levels{
		{
			name: "root",
		},
	}
}

type level struct {
	name string

	// branchFactor indicates how the parent should branch for this level
	// (i.e. how many child tests should be created per parent test in the previous level).
	branchFactor int

	// runParallel if true, all tests in this level will be run in parallel.
	runParallel bool
}

func (l level) runParallelString() string {
	if l.runParallel {
		return "parallel"
	}
	return "sync"
}

type levels []level

func (ls levels) Add(branchFactor int, runParallel bool) levels {
	return append(ls, level{
		name:         fmt.Sprintf("level%d", len(ls)),
		branchFactor: branchFactor,
		runParallel:  runParallel,
	})
}

func (ls levels) Build() *test {
	// Create the root test, which will always be run synchronously.
	root := &test{
		name: ls[0].name + "[sync]",
	}

	testsInPrevLevel := []*test{root}
	var testsInCurrentLevel []*test
	for _, l := range ls[1:] {
		for _, parent := range testsInPrevLevel {
			parent.runChildrenParallel = l.runParallel
			for i := 0; i < l.branchFactor; i++ {
				child := &test{
					name: fmt.Sprintf("%s_%d[%s]", l.name, len(testsInCurrentLevel), l.runParallelString()),
				}
				parent.children = append(parent.children, child)
				testsInCurrentLevel = append(testsInCurrentLevel, child)
			}
		}

		// Update for next iteration.
		testsInPrevLevel = testsInCurrentLevel
		testsInCurrentLevel = nil
	}

	return root
}

type test struct {
	name                string
	c                   *component
	runStart            time.Time
	runEnd              time.Time
	cleanupStart        time.Time
	cleanupEnd          time.Time
	componentCloseStart time.Time
	componentCloseEnd   time.Time

	runChildrenParallel bool
	children            []*test
}

func (tst *test) Run(t *testing.T) {
	t.Helper()
	framework.NewTest(t).
		Features("infrastructure.framework").
		Run(func(t framework.TestContext) {
			t.NewSubTest(tst.name).Run(tst.runInternal)
		})

	if err := tst.doCheck(); err != nil {
		t.Fatal(err)
	}
}

func (tst *test) runInternal(t framework.TestContext) {
	tst.runStart = time.Now()
	t.Cleanup(tst.cleanup)
	tst.c = newComponent(t, t.Name(), tst.componentClosed)

	if len(tst.children) == 0 {
		doWork()
	} else {
		for _, child := range tst.children {
			subTest := t.NewSubTest(child.name)
			if tst.runChildrenParallel {
				subTest.RunParallel(child.runInternal)
			} else {
				subTest.Run(child.runInternal)
			}
		}
	}
	tst.runEnd = time.Now()
}

func (tst *test) timeRange() timeRange {
	return timeRange{
		start: tst.runStart,
		end:   tst.cleanupEnd,
	}
}

func (tst *test) doCheck() error {
	// Make sure the component was closed after the test's run method exited.
	if tst.componentCloseStart.Before(tst.runEnd) {
		return fmt.Errorf("test %s: componentCloseStart (%v) occurred before runEnd (%v)",
			tst.name, tst.componentCloseStart, tst.runEnd)
	}

	// Make sure the component was closed before the cleanup for this test was performed.
	if tst.cleanupStart.Before(tst.componentCloseEnd) {
		return fmt.Errorf("test %s: closeStart (%v) occurred before componentCloseEnd (%v)",
			tst.name, tst.cleanupStart, tst.componentCloseEnd)
	}

	// Now make sure children cleanup occurred after cleanup for this test.
	for _, child := range tst.children {
		if child.cleanupEnd.After(tst.cleanupStart) {
			return fmt.Errorf("child %s cleanupEnd (%v) occurred after parent %s cleanupStart (%v)",
				child.name, child.cleanupEnd, tst.name, tst.cleanupStart)
		}
	}

	if !tst.runChildrenParallel {
		// The children were run synchronously. Make sure they don't overlap in time.
		for i := 0; i < len(tst.children); i++ {
			ci := tst.children[i]
			ciRange := ci.timeRange()
			for j := i + 1; j < len(tst.children); j++ {
				cj := tst.children[j]
				cjRange := cj.timeRange()
				if ciRange.overlaps(cjRange) {
					return fmt.Errorf("test %s: child %s[%s] overlaps with child %s[%s]",
						tst.name, ci.name, ciRange, cj.name, cjRange)
				}
			}
		}
	}

	// Now check the children.
	for _, child := range tst.children {
		if err := child.doCheck(); err != nil {
			return err
		}
	}
	return nil
}

func (tst *test) componentClosed(*component) {
	tst.componentCloseStart = time.Now()
	doWork()
	tst.componentCloseEnd = time.Now()
}

func (tst *test) cleanup() {
	tst.cleanupStart = time.Now()
	doWork()
	tst.cleanupEnd = time.Now()
}

func doWork() {
	time.Sleep(time.Millisecond * 10)
}

type timeRange struct {
	start, end time.Time
}

func (r timeRange) overlaps(o timeRange) bool {
	return r.start.Before(o.end) && r.end.After(o.start)
}

func (r timeRange) String() string {
	return fmt.Sprintf("start=%v, end=%v", r.start, r.end)
}
