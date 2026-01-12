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

package compare

import (
	"bytes"
	"os"
	"testing"
)

// TestComparatorMatchingConfigs tests the scenario where Istiod and Envoy configurations match
func TestComparatorMatchingSameConfigs(t *testing.T) {
	cfg, err := os.ReadFile("testdata/configdump.json")
	if err != nil {
		t.Fatalf("Failed to read test data: %v", err)
	}

	var outputBuffer bytes.Buffer
	comparator, err := NewComparator(&outputBuffer, map[string][]byte{"default": cfg}, cfg)
	if err != nil {
		t.Fatalf("Failed to create Comparator: %v", err)
	}
	err = comparator.Diff()
	if err != nil {
		t.Errorf("Unexpected error during diff: %v", err)
	}

	expected := []string{"Clusters Match", "Listeners Match", "Routes Match"}
	for _, exp := range expected {
		if !bytes.Contains(outputBuffer.Bytes(), []byte(exp)) {
			t.Errorf("Expected %s, but it was not found", exp)
		}
	}
}

// TestComparatorMismatchedConfigs tests the scenario where Istiod and Envoy configurations do not match
func TestComparatorMismatchedConfigs(t *testing.T) {
	cfg, err := os.ReadFile("testdata/configdump.json")
	if err != nil {
		t.Fatalf("Failed to read test data: %v", err)
	}
	diffCfg, err := os.ReadFile("testdata/configdump_diff.json")
	if err != nil {
		t.Fatalf("Failed to read test data: %v", err)
	}

	var outputBuffer bytes.Buffer
	comparator, err := NewComparator(&outputBuffer, map[string][]byte{"default": cfg}, diffCfg)
	if err != nil {
		t.Fatalf("Failed to create Comparator: %v", err)
	}
	err = comparator.Diff()
	if err != nil {
		t.Errorf("Unexpected error during diff: %v", err)
	}

	expectedNotMatch := []string{"Clusters Don't Match", "Listeners Don't Match", "Routes Don't Match"}
	for _, exp := range expectedNotMatch {
		if !bytes.Contains(outputBuffer.Bytes(), []byte(exp)) {
			t.Errorf("Expected %s, but it was not found", exp)
		}
	}
}
