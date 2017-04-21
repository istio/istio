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

package util

import (
	"errors"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/pmezard/go-difflib/difflib"
)

// Refresh controls whether to update the golden artifacts instead.
// It is set using the environment variable REFRESH_GOLDEN.
func Refresh() bool {
	v, exists := os.LookupEnv("REFRESH_GOLDEN")
	return exists && v == "true"
}

// Compare compares two byte slices. It returns an error with a
// contextual diff if they are not equal.
func Compare(content, golden []byte) error {
	data := strings.TrimSpace(string(content))
	expected := strings.TrimSpace(string(golden))

	if data != expected {
		diff := difflib.UnifiedDiff{
			A:       difflib.SplitLines(expected),
			B:       difflib.SplitLines(data),
			Context: 2,
		}
		text, err := difflib.GetUnifiedDiffString(diff)
		if err != nil {
			return err
		}
		return errors.New(text)
	}

	return nil
}

// CompareYAML compares a file "x" against a golden file "x.golden"
func CompareYAML(filename string, t *testing.T) {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatalf(err.Error())
	}
	goldenFile := filename + ".golden"
	if Refresh() {
		t.Logf("Refreshing golden file for %s", filename)
		if err = ioutil.WriteFile(goldenFile, content, 0644); err != nil {
			t.Errorf(err.Error())
		}
	}

	golden, err := ioutil.ReadFile(goldenFile)
	if err != nil {
		t.Fatalf(err.Error())
	}
	if err = Compare(content, golden); err != nil {
		t.Errorf("Failed validating artifact %s:\n%v", filename, err)
	}
}

// CompareContent compares the content value against the golden file
func CompareContent(content []byte, goldenFile string, t *testing.T) {
	if Refresh() {
		t.Logf("Refreshing golden file %s", goldenFile)
		if err := ioutil.WriteFile(goldenFile, content, 0644); err != nil {
			t.Errorf(err.Error())
		}
	}

	golden, err := ioutil.ReadFile(goldenFile)
	if err != nil {
		t.Fatalf(err.Error())
	}
	if err = Compare(content, golden); err != nil {
		t.Fatalf("Failed validating golden file %s:\n%v", goldenFile, err)
	}
}
