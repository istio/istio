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

package test

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/dependency"
	"istio.io/istio/pkg/test/internal"
	"istio.io/istio/pkg/test/label"
)

// Run is a helper for executing test main with appropriate resource allocation/doCleanup steps.
// It allows us to do post-run doCleanup, and flag parsing.
func Run(testID string, m *testing.M) {
	if len(testID) > maxTestIDLength {
		panic(fmt.Sprintf("test id cannot be longer than %d characters", maxTestIDLength))
	}

	// TODO: Protect against double-run, invalid driverState etc.
	err := setup(testID)
	if err != nil {
		fmt.Printf("Error performing setup: %v\n", err)
		os.Exit(-1)
	}
	rt := m.Run()
	doCleanup()
	os.Exit(rt)
}

// Ignore the test with the given reason.
func Ignore(t testing.TB, reason string) {
	t.Skipf("Skipping(Ignored): %s", reason)
}

// Requires ensures that the given dependencies will be satisfied. If they cannot, then the
// test will fail.
func Requires(t testing.TB, dependencies ...dependency.Dependency) {
	driver.Lock()
	defer driver.Unlock()

	// Initialize dependencies only once.
	for _, d := range dependencies {
		s, ok := d.(internal.Stateful)
		if !ok {
			continue
		}

		instance, ok := driver.initializedDependencies[d]
		if ok {
			// If they are already satisfied, then signal a "reset", to ensure a clean, well-known driverState.
			if err := s.Reset(instance); err != nil {
				t.Fatalf("Unable to reset dependency '%v': %v", d, err)
				return
			}
			continue
		}

		var err error
		if instance, err = s.Initialize(); err != nil {
			t.Fatalf("Unable to satisfy dependency '%v': %v", d, err)
			return
		}

		driver.initializedDependencies[d] = instance
	}
}

// Tag the test with the given labels. The user can filter using the labels.
// TODO: The polarity of this is a bit borked. If the test doesn't call Tag, then it won't get filtered out.
func Tag(t testing.TB, labels ...label.Label) {
	driver.Lock()
	defer driver.Unlock()

	skip := false
	if driver.labels != "" {
		// Only filter if the labels are specified.
		skip = true
		for _, l := range labels {
			allowed := strings.Split(driver.labels, ",")
			for _, a := range allowed {
				if label.Label(a) == l {
					skip = false
					break
				}
			}
		}
	}

	if !skip {
		checkWellknownLabels(t, labels...)
	}

	if skip && !t.Skipped() {
		t.Skip("Skipping(Filtered): No matching label found")
	}
}

func checkWellknownLabels(t testing.TB, labels ...label.Label) {
	for _, l := range labels {
		switch l {
		case label.LinuxOnly:
			if runtime.GOOS != "linux" {
				t.Skipf("Skipping(Filtered): The operating system is not Linux: %s", runtime.GOOS)
			}
		}
	}
}
