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
	"encoding/xml"
	"io"
)

// JUnitError represents an error with extra metadata suitable for structured
// (JUnit.xml) output written by kubetest2
// If this type is returned as an error by a Deployer, kubetest2 may write the
// metadata to the output junit_runner.xml
type JUnitError interface {
	error
	// this should be the command output (stdout/stderr)
	SystemOut() string
}

type simpleJUnitError struct {
	error
	systemOut string
}

// ensure simpleJUnitError implements JUnitError
var _ JUnitError = &simpleJUnitError{}

func (s *simpleJUnitError) SystemOut() string {
	return s.systemOut
}

// NewJUnitError returns a simple instance of JUnitError wrapping systemOut
func NewJUnitError(inner error, systemOut string) error {
	return &simpleJUnitError{
		error:     inner,
		systemOut: systemOut,
	}
}

// testSuite holds a slice of TestCase and other summary metadata.
//
// A build (column in testgrid) is composed of one or more TestSuites.
type testSuite struct {
	XMLName  xml.Name `xml:"testsuite"`
	Name     string   `xml:"name,attr"`
	Failures int      `xml:"failures,attr"`
	Tests    int      `xml:"tests,attr"`
	Time     float64  `xml:"time,attr"`
	Cases    []testCase
}

func (t *testSuite) Write(writer io.Writer) error {
	// write xml header
	_, _ = io.WriteString(writer, `<?xml version="1.0" encoding="UTF-8"?>`)
	// write indented suite
	e := xml.NewEncoder(writer)
	e.Indent("", "    ")
	return e.Encode(t)
}

func (t *testSuite) AddTestCase(tc testCase) {
	t.Tests++
	if tc.Failure != "" {
		t.Failures++
	}
	t.Cases = append(t.Cases, tc)
}

// testCase holds the result of a test/step/command.
//
// This will become a row in testgrid.
type testCase struct {
	XMLName   xml.Name `xml:"testcase"`
	Name      string   `xml:"name,attr"`
	ClassName string   `xml:"classname,attr"`
	Time      float64  `xml:"time,attr"`
	Failure   string   `xml:"failure,omitempty"`
	Skipped   string   `xml:"skipped,omitempty"`
	SystemOut string   `xml:"system-out,omitempty"`
}
