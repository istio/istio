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
package annotations

import (
	"os"
	"testing"
)

const testAnnotation = "TESTXYZ"

func reset() {
	_ = os.Unsetenv(testAnnotation)
	mutex.Lock()
	allAnnotations = make(map[string]Annotation)
	mutex.Unlock()
}

func TestBasic(t *testing.T) {
	reset()

	_ = Register(testAnnotation+"2", "Two")
	_ = Register(testAnnotation+"0", "Zero")
	_ = Register(testAnnotation+"1", "One")

	a := Descriptions()
	if a[0].Name != "TESTXYZ0" {
		t.Errorf("Expecting TESTXYZ0, got %s", a[0].Name)
	}
	if a[0].Description != "Zero" {
		t.Errorf("Expected 'Zero', got '%s'", a[0].Description)
	}

	if a[1].Name != "TESTXYZ1" {
		t.Errorf("Expecting TESTXYZ1, got %s", a[0].Name)
	}
	if a[1].Description != "One" {
		t.Errorf("Expected 'One', got '%s'", a[0].Description)
	}

	if a[2].Name != "TESTXYZ2" {
		t.Errorf("Expecting TESTXYZ2, got %s", a[0].Name)
	}
	if a[2].Description != "Two" {
		t.Errorf("Expected '2', got '%s'", a[0].Description)
	}
}

func TestDupes(t *testing.T) {
	// make sure annotation without a description doesn't overwrite one with
	reset()
	_ = Register(testAnnotation, "XYZ")
	a := Register(testAnnotation, "")
	if a.Description != "XYZ" {
		t.Errorf("Expected 'XYZ', got '%s'", a.Description)
	}

	// make sure annotation without a description doesn't overwrite one with
	reset()
	_ = Register(testAnnotation, "")
	a = Register(testAnnotation, "XYZ")
	if a.Description != "XYZ" {
		t.Errorf("Expected 'XYZ', got '%s'", a.Description)
	}
}
