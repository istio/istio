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

// Package env makes it possible to track use of environment variables within procress
// in order to generate documentation for these uses.
package env

import (
	"os"
	"testing"
	"time"
)

const testVar = "TESTXYZ"

func reset() {
	_ = os.Unsetenv(testVar)
	mutex.Lock()
	allVars = make(map[string]Var)
	mutex.Unlock()
}

func TestString(t *testing.T) {
	reset()

	ev := RegisterStringVar(testVar, "123", "")
	v, present := ev.Lookup()
	if v != "123" {
		t.Errorf("Expected 123, got %s", v)
	}
	if present {
		t.Errorf("Expected not present")
	}

	v = ev.Get()
	if v != "123" {
		t.Errorf("Expected 123, got %s", v)
	}

	_ = os.Setenv(testVar, "ABC")

	ev = RegisterStringVar(testVar, "123", "")
	v, present = ev.Lookup()
	if v != "ABC" {
		t.Errorf("Expected ABC, got %s", v)
	}
	if !present {
		t.Errorf("Expected present")
	}

	v = ev.Get()
	if v != "ABC" {
		t.Errorf("Expected ABC, got %s", v)
	}
}

func TestInt(t *testing.T) {
	reset()

	ev := RegisterIntVar(testVar, 123, "")
	v, present := ev.Lookup()
	if v != 123 {
		t.Errorf("Expected 123, got %v", v)
	}
	if present {
		t.Errorf("Expected not present")
	}

	v = ev.Get()
	if v != 123 {
		t.Errorf("Expected 123, got %v", v)
	}

	_ = os.Setenv(testVar, "XXX")

	ev = RegisterIntVar(testVar, 123, "")
	v, present = ev.Lookup()
	if v != 123 {
		t.Errorf("Expected 123, got %v", v)
	}
	if !present {
		t.Errorf("Expected present")
	}

	v = ev.Get()
	if v != 123 {
		t.Errorf("Expected 123, got %v", v)
	}

	_ = os.Setenv(testVar, "789")

	ev = RegisterIntVar(testVar, 123, "")
	v, present = ev.Lookup()
	if v != 789 {
		t.Errorf("Expected 789, got %v", v)
	}
	if !present {
		t.Errorf("Expected present")
	}

	v = ev.Get()
	if v != 789 {
		t.Errorf("Expected 789, got %v", v)
	}
}

func TestBool(t *testing.T) {
	reset()

	ev := RegisterBoolVar(testVar, true, "")
	v, present := ev.Lookup()
	if v != true {
		t.Errorf("Expected true, got %v", v)
	}
	if present {
		t.Errorf("Expected not present")
	}

	v = ev.Get()
	if v != true {
		t.Errorf("Expected true, got %v", v)
	}

	_ = os.Setenv(testVar, "XXX")

	ev = RegisterBoolVar(testVar, true, "")
	v, present = ev.Lookup()
	if v != true {
		t.Errorf("Expected true, got %v", v)
	}
	if !present {
		t.Errorf("Expected present")
	}

	v = ev.Get()
	if v != true {
		t.Errorf("Expected true, got %v", v)
	}

	_ = os.Setenv(testVar, "true")

	ev = RegisterBoolVar(testVar, false, "")
	v, present = ev.Lookup()
	if v != true {
		t.Errorf("Expected true, got %v", v)
	}
	if !present {
		t.Errorf("Expected present")
	}

	v = ev.Get()
	if v != true {
		t.Errorf("Expected true, got %v", v)
	}
}

func TestFloat(t *testing.T) {
	reset()

	ev := RegisterFloatVar(testVar, 123.0, "")
	v, present := ev.Lookup()
	if v != 123.0 {
		t.Errorf("Expected 123.0, got %v", v)
	}
	if present {
		t.Errorf("Expected not present")
	}

	v = ev.Get()
	if v != 123.0 {
		t.Errorf("Expected 123.0, got %v", v)
	}

	_ = os.Setenv(testVar, "XXX")

	ev = RegisterFloatVar(testVar, 123.0, "")
	v, present = ev.Lookup()
	if v != 123.0 {
		t.Errorf("Expected 123.0, got %v", v)
	}
	if !present {
		t.Errorf("Expected present")
	}

	v = ev.Get()
	if v != 123.0 {
		t.Errorf("Expected 123.0, got %v", v)
	}

	_ = os.Setenv(testVar, "789")

	ev = RegisterFloatVar(testVar, 123.0, "")
	v, present = ev.Lookup()
	if v != 789 {
		t.Errorf("Expected 789.0, got %v", v)
	}
	if !present {
		t.Errorf("Expected present")
	}

	v = ev.Get()
	if v != 789 {
		t.Errorf("Expected 789.0, got %v", v)
	}
}

func TestDuration(t *testing.T) {
	reset()

	ev := RegisterDurationVar(testVar, 123*time.Second, "")
	v, present := ev.Lookup()
	if v != 123*time.Second {
		t.Errorf("Expected 123 seconds, got %v", v)
	}
	if present {
		t.Errorf("Expected not present")
	}

	v = ev.Get()
	if v != 123*time.Second {
		t.Errorf("Expected 123 seconds, got %v", v)
	}

	_ = os.Setenv(testVar, "XXX")

	ev = RegisterDurationVar(testVar, 123*time.Second, "")
	v, present = ev.Lookup()
	if v != 123*time.Second {
		t.Errorf("Expected 123 seconds, got %v", v)
	}
	if !present {
		t.Errorf("Expected present")
	}

	v = ev.Get()
	if v != 123*time.Second {
		t.Errorf("Expected 123 seconds, got %v", v)
	}

	_ = os.Setenv(testVar, "789s")

	ev = RegisterDurationVar(testVar, 123*time.Second, "")
	v, present = ev.Lookup()
	if v != 789*time.Second {
		t.Errorf("Expected 789 seconds, got %v", v)
	}
	if !present {
		t.Errorf("Expected present")
	}

	v = ev.Get()
	if v != 789*time.Second {
		t.Errorf("Expected 789 seconds, got %v", v)
	}
}

func TestDesc(t *testing.T) {
	reset()

	_ = RegisterDurationVar(testVar+"5", 123*time.Second, "A duration")
	_ = RegisterStringVar(testVar+"1", "123", "A string")
	_ = RegisterIntVar(testVar+"2", 456, "An int")
	_ = RegisterBoolVar(testVar+"3", true, "A bool")
	_ = RegisterFloatVar(testVar+"4", 789.0, "A float")

	vars := VarDescriptions()
	if vars[0].Name != "TESTXYZ1" {
		t.Errorf("Expecting TESTXYZ1, got %s", vars[0].Name)
	}
	if vars[0].Description != "A string" {
		t.Errorf("Expected 'A string', got '%s'", vars[0].Description)
	}

	if vars[1].Name != "TESTXYZ2" {
		t.Errorf("Expecting TESTXYZ2, got %s", vars[0].Name)
	}
	if vars[1].Description != "An int" {
		t.Errorf("Expected 'An int', got '%s'", vars[0].Description)
	}

	if vars[2].Name != "TESTXYZ3" {
		t.Errorf("Expecting TESTXYZ3, got %s", vars[0].Name)
	}
	if vars[2].Description != "A bool" {
		t.Errorf("Expected 'A bool', got '%s'", vars[0].Description)
	}

	if vars[3].Name != "TESTXYZ4" {
		t.Errorf("Expecting TESTXYZ4, got %s", vars[0].Name)
	}
	if vars[3].Description != "A float" {
		t.Errorf("Expected 'A float', got '%s'", vars[0].Description)
	}

	if vars[4].Name != "TESTXYZ5" {
		t.Errorf("Expecting TESTXYZ5, got %s", vars[0].Name)
	}
	if vars[4].Description != "A duration" {
		t.Errorf("Expected 'A duration', got '%s'", vars[0].Description)
	}
}

func TestDupes(t *testing.T) {

	// make sure var without a description doesn't overwrite one with
	reset()
	_ = RegisterStringVar(testVar, "123", "XYZ")
	v := RegisterStringVar(testVar, "123", "")
	if v.Description != "XYZ" {
		t.Errorf("Expected 'XYZ', got '%s'", v.Description)
	}

	// make sure var without a description doesn't overwrite one with
	reset()
	_ = RegisterStringVar(testVar, "123", "")
	v = RegisterStringVar(testVar, "123", "XYZ")
	if v.Description != "XYZ" {
		t.Errorf("Expected 'XYZ', got '%s'", v.Description)
	}
}
