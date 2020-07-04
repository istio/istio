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

package util

import (
	"errors"
	"io/ioutil"
	"regexp"
	"strings"
	"testing"

	"github.com/pmezard/go-difflib/difflib"

	"istio.io/pkg/env"
)

const (
	statusReplacement = "sidecar.istio.io/status: '{\"version\":\"\","
)

var (
	statusPattern = regexp.MustCompile("sidecar.istio.io/status: '{\"version\":\"([0-9a-f]+)\",")
)

// Refresh controls whether to update the golden artifacts instead.
// It is set using the environment variable REFRESH_GOLDEN.
func Refresh() bool {
	return env.RegisterBoolVar("REFRESH_GOLDEN", false, "").Get()
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
	t.Helper()
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

// CompareContent compares the content value against the golden file and fails the test if they differ
func CompareContent(content []byte, goldenFile string, t *testing.T) {
	t.Helper()
	golden := ReadGoldenFile(content, goldenFile, t)
	CompareBytes(content, golden, goldenFile, t)
}

// ReadGoldenFile reads the content of the golden file and fails the test if an error is encountered
func ReadGoldenFile(content []byte, goldenFile string, t *testing.T) []byte {
	t.Helper()
	RefreshGoldenFile(content, goldenFile, t)

	return ReadFile(goldenFile, t)
}

// StripVersion strips the version fields of a YAML content.
func StripVersion(yaml []byte) []byte {
	return statusPattern.ReplaceAllLiteral(yaml, []byte(statusReplacement))
}

// RefreshGoldenFile updates the golden file with the given content
func RefreshGoldenFile(content []byte, goldenFile string, t *testing.T) {
	if Refresh() {
		t.Logf("Refreshing golden file %s", goldenFile)
		if err := ioutil.WriteFile(goldenFile, content, 0644); err != nil {
			t.Errorf(err.Error())
		}
	}
}

// ReadFile reads the content of the given file or fails the test if an error is encountered.
func ReadFile(file string, t testing.TB) []byte {
	t.Helper()
	golden, err := ioutil.ReadFile(file)
	if err != nil {
		t.Fatalf(err.Error())
	}
	return golden
}

// CompareBytes compares the content value against the golden bytes and fails the test if they differ
func CompareBytes(content []byte, golden []byte, name string, t *testing.T) {
	t.Helper()
	if err := Compare(content, golden); err != nil {
		t.Fatalf("Failed validating golden file %s:\n%v", name, err)
	}
}
