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
	"io"
	"time"
)

// Writer manages writing out kubetest2 metadata, namely JUnit
type Writer struct {
	suite     testSuite
	start     time.Time
	runnerOut io.Writer
	// for faking out time when testing
	timeNow func() time.Time
}

// NewWriter constructs a new writer, the junit_runner.xml contents
// will be written to runnerOut, with the top level kubetest2 stages as
// metadata (Up, Down, etc.)
func NewWriter(suiteName string, runnerOut io.Writer) *Writer {
	suite := testSuite{Name: suiteName}
	return &Writer{
		suite:     suite,
		runnerOut: runnerOut,
		start:     time.Now(),
		timeNow:   time.Now,
	}
}

// WrapStep executes doStep and captures the output to be written to the
// kubetest2 runner metadata. If doStep returns a JUnitError this metadata
// will be captured
func (w *Writer) WrapStep(name string, doStep func() error) error {
	start := w.timeNow()
	err := doStep()
	finish := w.timeNow()
	tc := testCase{
		Name:      name,
		ClassName: w.suite.Name,
		Time:      finish.Sub(start).Seconds(),
	}
	if err != nil {
		tc.Failure = err.Error()
	}
	if v, ok := err.(JUnitError); ok {
		tc.SystemOut = v.SystemOut()
	}
	w.suite.AddTestCase(tc)
	return err
}

// Finish finalizes the metadata (time) and writes it out
func (w *Writer) Finish() error {
	w.suite.Time = w.timeNow().Sub(w.start).Seconds()
	return w.suite.Write(w.runnerOut)
}
