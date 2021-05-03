/*
Copyright 2019 The Kubernetes Authors.

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

package metadata

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

// fake time.Now that always increments by one second
func makeFakeNow() func() time.Time {
	var t time.Time
	return func() time.Time {
		t = t.Add(time.Second)
		return t
	}
}

// junitError impl for testing
type junitError struct {
	name      string
	systemout string
}

// assert that junitError is actually a JUnitError
var _ JUnitError = &junitError{}

func (j junitError) Error() string {
	return j.name
}

func (j *junitError) SystemOut() string {
	return j.systemout
}

func TestWriter(t *testing.T) {
	type step = struct {
		name        string
		doStep      func() error
		expectError bool
	}

	var testCases = []struct {
		name           string
		steps          []step
		expectedOutput string
	}{
		{
			name: "all passing",
			steps: []step{
				{
					name:   "noop",
					doStep: func() error { return nil },
				},
			},
			expectedOutput: strings.TrimPrefix(
				`
<?xml version="1.0" encoding="UTF-8"?><testsuite name="kubetest2" failures="0" tests="1" time="3">
    <testcase name="noop" classname="kubetest2" time="1"></testcase>
</testsuite>`,
				"\n",
			),
		},
		{
			name: "one failed step",
			steps: []step{
				{
					name:        "always fails",
					doStep:      func() error { return errors.New("oh noes") },
					expectError: true,
				},
			},
			expectedOutput: strings.TrimPrefix(
				`
<?xml version="1.0" encoding="UTF-8"?><testsuite name="kubetest2" failures="1" tests="1" time="3">
    <testcase name="always fails" classname="kubetest2" time="1">
        <failure>oh noes</failure>
    </testcase>
</testsuite>`,
				"\n",
			),
		},
		{
			name: "one failed step with junitError",
			steps: []step{
				{
					name: "always fails (junitError)",
					doStep: func() error {
						return &junitError{
							name:      "on noes",
							systemout: "uh oh",
						}
					},
					expectError: true,
				},
			},
			expectedOutput: strings.TrimPrefix(
				`
<?xml version="1.0" encoding="UTF-8"?><testsuite name="kubetest2" failures="1" tests="1" time="3">
    <testcase name="always fails (junitError)" classname="kubetest2" time="1">
        <failure>on noes</failure>
        <system-out>uh oh</system-out>
    </testcase>
</testsuite>`,
				"\n",
			),
		},
		{
			name: "two passing steps and one failed step with junitError",
			steps: []step{
				{
					name:   "noop",
					doStep: func() error { return nil },
				},
				{
					name:   "noop2",
					doStep: func() error { return nil },
				},
				{
					name: "always fails (junitError)",
					doStep: func() error {
						return &junitError{
							name:      "on noes",
							systemout: "uh oh",
						}
					},
					expectError: true,
				},
			},
			expectedOutput: strings.TrimPrefix(
				`
<?xml version="1.0" encoding="UTF-8"?><testsuite name="kubetest2" failures="1" tests="3" time="7">
    <testcase name="noop" classname="kubetest2" time="1"></testcase>
    <testcase name="noop2" classname="kubetest2" time="1"></testcase>
    <testcase name="always fails (junitError)" classname="kubetest2" time="1">
        <failure>on noes</failure>
        <system-out>uh oh</system-out>
    </testcase>
</testsuite>`,
				"\n",
			),
		},
	}
	for _, tc := range testCases {
		tc := tc // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			// these are parallel safe, we faked out time etc.
			t.Parallel()
			// fake output io.WriteCloser
			runnerOut := bytes.NewBuffer([]byte{})
			// create a new writer and fake out the time
			w := NewWriter("kubetest2", runnerOut)
			w.timeNow = makeFakeNow()
			w.start = w.timeNow()
			// run all the steps
			for _, step := range tc.steps {
				err := w.WrapStep(step.name, step.doStep)
				if err != nil && !step.expectError {
					t.Errorf("got unexpected error for step %#v %v", step.name, err)
				} else if err == nil && step.expectError {
					t.Errorf("expected error for step: %#v and got none", step.name)
				}
			}
			// finish writing the writer and check the output
			err := w.Finish()
			if err != nil {
				t.Errorf("unexpected error for writer.Finish() %v", err)
			}
			output := runnerOut.String()
			if output != tc.expectedOutput {
				t.Errorf("runnerOut did not match expected \n%v\nVERSUS:\n %v", tc.expectedOutput, output)
			}
		})
	}
}
