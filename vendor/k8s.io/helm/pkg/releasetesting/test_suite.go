/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package releasetesting

import (
	"context"
	"fmt"
	"golang.org/x/sync/semaphore"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/golang/protobuf/ptypes/timestamp"
	"k8s.io/api/core/v1"

	"k8s.io/helm/pkg/hooks"
	"k8s.io/helm/pkg/proto/hapi/release"
	util "k8s.io/helm/pkg/releaseutil"
	"k8s.io/helm/pkg/timeconv"
)

// TestSuite what tests are run, results, and metadata
type TestSuite struct {
	StartedAt     *timestamp.Timestamp
	CompletedAt   *timestamp.Timestamp
	TestManifests []string
	Results       []*release.TestRun
}

type test struct {
	manifest        string
	expectedSuccess bool
	result          *release.TestRun
}

// NewTestSuite takes a release object and returns a TestSuite object with test definitions
//  extracted from the release
func NewTestSuite(rel *release.Release) (*TestSuite, error) {
	testManifests, err := extractTestManifestsFromHooks(rel.Hooks)
	if err != nil {
		return nil, err
	}

	results := []*release.TestRun{}

	return &TestSuite{
		TestManifests: testManifests,
		Results:       results,
	}, nil
}

// Run executes tests in a test suite and stores a result within a given environment
func (ts *TestSuite) Run(env *Environment) error {
	ts.StartedAt = timeconv.Now()

	if len(ts.TestManifests) == 0 {
		// TODO: make this better, adding test run status on test suite is weird
		env.streamMessage("No Tests Found", release.TestRun_UNKNOWN)
	}

	var tests []*test

	for _, testManifest := range ts.TestManifests {
		test, err := newTest(testManifest)
		if err != nil {
			return err
		}

		tests = append(tests, test)
	}

	if env.Parallel {
		c := make(chan error, len(tests))
		// Use a semaphore to restrict the number of tests running in parallel.
		sem := semaphore.NewWeighted(int64(env.Parallelism))
		ctx := context.Background()
		for _, t := range tests {
			sem.Acquire(ctx, 1)
			go func(t *test, sem *semaphore.Weighted) {
				defer sem.Release(1)
				c <- t.run(env)
			}(t, sem)
		}

		for range tests {
			if err := <-c; err != nil {
				return err
			}
		}

	} else {
		for _, t := range tests {
			if err := t.run(env); err != nil {
				return err
			}
		}
	}

	for _, t := range tests {
		ts.Results = append(ts.Results, t.result)
	}

	ts.CompletedAt = timeconv.Now()
	return nil
}

func (t *test) run(env *Environment) error {
	t.result.StartedAt = timeconv.Now()
	if err := env.streamRunning(t.result.Name); err != nil {
		return err
	}
	t.result.Status = release.TestRun_RUNNING

	resourceCreated := true
	if err := env.createTestPod(t); err != nil {
		resourceCreated = false
		if streamErr := env.streamError(t.result.Info); streamErr != nil {
			return err
		}
	}

	resourceCleanExit := true
	status := v1.PodUnknown
	if resourceCreated {
		var err error
		status, err = env.getTestPodStatus(t)
		if err != nil {
			resourceCleanExit = false
			if streamErr := env.streamError(t.result.Info); streamErr != nil {
				return streamErr
			}
		}
	}

	if resourceCreated && resourceCleanExit {
		if err := t.assignTestResult(status); err != nil {
			return err
		}

		if err := env.streamResult(t.result); err != nil {
			return err
		}
	}

	t.result.CompletedAt = timeconv.Now()
	return nil
}

func (t *test) assignTestResult(podStatus v1.PodPhase) error {
	switch podStatus {
	case v1.PodSucceeded:
		if t.expectedSuccess {
			t.result.Status = release.TestRun_SUCCESS
		} else {
			t.result.Status = release.TestRun_FAILURE
		}
	case v1.PodFailed:
		if !t.expectedSuccess {
			t.result.Status = release.TestRun_SUCCESS
		} else {
			t.result.Status = release.TestRun_FAILURE
		}
	default:
		t.result.Status = release.TestRun_UNKNOWN
	}

	return nil
}

func expectedSuccess(hookTypes []string) (bool, error) {
	for _, hookType := range hookTypes {
		hookType = strings.ToLower(strings.TrimSpace(hookType))
		if hookType == hooks.ReleaseTestSuccess {
			return true, nil
		} else if hookType == hooks.ReleaseTestFailure {
			return false, nil
		}
	}
	return false, fmt.Errorf("No %s or %s hook found", hooks.ReleaseTestSuccess, hooks.ReleaseTestFailure)
}

func extractTestManifestsFromHooks(h []*release.Hook) ([]string, error) {
	testHooks := hooks.FilterTestHooks(h)

	tests := []string{}
	for _, h := range testHooks {
		individualTests := util.SplitManifests(h.Manifest)
		for _, t := range individualTests {
			tests = append(tests, t)
		}
	}
	return tests, nil
}

func newTest(testManifest string) (*test, error) {
	var sh util.SimpleHead
	err := yaml.Unmarshal([]byte(testManifest), &sh)
	if err != nil {
		return nil, err
	}

	if sh.Kind != "Pod" {
		return nil, fmt.Errorf("%s is not a pod", sh.Metadata.Name)
	}

	hookTypes := sh.Metadata.Annotations[hooks.HookAnno]
	expected, err := expectedSuccess(strings.Split(hookTypes, ","))
	if err != nil {
		return nil, err
	}

	name := strings.TrimSuffix(sh.Metadata.Name, ",")
	return &test{
		manifest:        testManifest,
		expectedSuccess: expected,
		result: &release.TestRun{
			Name: name,
		},
	}, nil
}
