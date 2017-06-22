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

// Simple tests for non github part of githubContrib.go

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"runtime"
	"strings"
	"testing"
)

func CheckEqual(t *testing.T, msg interface{}, expected interface{}, actual interface{}) {
	if expected != actual {
		_, file, line, _ := runtime.Caller(1)
		file = file[strings.LastIndex(file, "/")+1:]
		fmt.Printf("%s:%d mismatch!\nexpected:\n%+v\nactual:\n%+v\nfor %+v\n", file, line, expected, actual, msg)
		t.Fail()
	}
}

func TestCompanyFromUser(r *testing.T) {
	var tests = []struct {
		user     userData // input
		expected string   // expected company
	}{
		{userData{Login: "ALogin", Name: "No Email or Company"}, "Unknown"},
		{userData{Company: "FOO"}, "Foo"},
		{userData{Company: "inc"}, "Inc"},
		{userData{Company: "Company Inc."}, "Company"},
		{userData{Company: "Company, Inc"}, "Company"},
		{userData{Company: "@blaH.Inc.  "}, "Blah"},
		{userData{Company: "@tada.Inc...  "}, "Tada"},
		{userData{Company: "    ", Email: "blah@place.com"}, "Place"},
		{userData{Email: "foo@bAr.com"}, "Bar"},
		{userData{Email: "joe@apache.org"}, "Apache.org"},
		{userData{Email: "joe@gmail.com"}, "Unknown"},
	}
	// Logger capture:
	var b bytes.Buffer
	w := bufio.NewWriter(&b)
	log.SetOutput(w)
	log.SetFlags(0)
	for _, t := range tests {
		CheckEqual(r, t.user, t.expected, companyFromUser(t.user, 42))
	}
	// Check what was logged:
	w.Flush() // nolint: errcheck
	expectedLog := `ALogin (No Email or Company) <> has 42 contributions but no company nor (useful) email
 () <joe@gmail.com> has 42 contributions but no company nor (useful) email
`
	actualLog := string(b.Bytes())
	CheckEqual(r, "companyFromUser() log", expectedLog, actualLog)
}
