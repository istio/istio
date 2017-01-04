// Copyright 2016 Google Inc.
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

package envoy

import (
	"log"
	"reflect"
	"testing"
)

// Ensure that parse(build(s)) == s
func TestBuildParseServiceKey(t *testing.T) {
	type TestCase struct {
		Service string
		Tags    []string
	}

	testCases := []TestCase{
		{
			Service: "service1",
			Tags:    []string{},
		},
		{
			Service: "service2",
			Tags:    []string{"A"},
		},
		{
			Service: "service3",
			Tags:    []string{"A", "B", "C"},
		},
		{
			Service: "ser__vice4_",
			Tags:    []string{"A_", "_B", "_C_"},
		},
		{
			Service: "_service5__",
			Tags:    []string{"_A", "B", "C"},
		},
		{
			Service: "",
			Tags:    []string{},
		},
	}

	for _, testCase := range testCases {
		s := BuildServiceKey(testCase.Service, testCase.Tags)
		log.Printf("%q", s)
		service, tags := ParseServiceKey(s)
		if testCase.Service != service {
			t.Errorf("Got %q, wanted %q", service, testCase.Service)
		}
		if !reflect.DeepEqual(testCase.Tags, tags) {
			t.Errorf("Got %q, wanted %q", tags, testCase.Tags)
		}
	}
}
