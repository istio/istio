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
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// Compare compares two byte slices. It returns an error with a
// contextual diff if they are not equal.
func Compare(out, model []byte) error {
	data := strings.TrimSpace(string(out))
	expected := strings.TrimSpace(string(model))

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

// CompareFiles compares the content of two files
func CompareFiles(outFile, modelFile string) error {
	var out, model []byte
	var err error
	out, err = ioutil.ReadFile(outFile)
	if err != nil {
		return err
	}

	model, err = ioutil.ReadFile(modelFile)
	if err != nil {
		return err
	}

	return Compare(out, model)
}

// CompareToFile compares a content with a file
func CompareToFile(out []byte, modelFile string) error {
	model, err := ioutil.ReadFile(modelFile)
	if err != nil {
		return err
	}
	return Compare(out, model)
}
