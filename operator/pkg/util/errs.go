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
	"fmt"
	"strings"
)

const (
	defaultSeparator = ", "
)

// Errors is a slice of error.
type Errors []error

// Error implements the error#Error method.
func (e Errors) Error() string {
	return ToString(e, defaultSeparator)
}

// String implements the stringer#String method.
func (e Errors) String() string {
	return e.Error()
}

// ToError returns an error from Errors.
func (e Errors) ToError() error {
	if len(e) == 0 {
		return nil
	}
	return fmt.Errorf("%s", e)
}

// Dedup removes any duplicated errors.
func (e Errors) Dedup() Errors {
	logCountMap := make(map[string]int)
	for _, ee := range e {
		if ee == nil {
			continue
		}
		item := ee.Error()
		_, exist := logCountMap[item]
		if exist {
			logCountMap[item]++
		} else {
			logCountMap[item] = 1
		}
	}
	var out Errors
	for _, ee := range e {
		item := ee.Error()
		count := logCountMap[item]
		if count == 0 {
			continue
		}
		times := ""
		if count > 1 {
			times = fmt.Sprintf(" (repeated %d times)", count)
		}
		out = AppendErr(out, fmt.Errorf("%s%s", ee, times))
		// reset seen log count
		logCountMap[item] = 0
	}
	return out
}

// NewErrs returns a slice of error with a single element err.
// If err is nil, returns nil.
func NewErrs(err error) Errors {
	if err == nil {
		return nil
	}
	return []error{err}
}

// AppendErr appends err to errors if it is not nil and returns the result.
// If err is nil, it is not appended.
func AppendErr(errors []error, err error) Errors {
	if err == nil {
		if len(errors) == 0 {
			return nil
		}
		return errors
	}
	return append(errors, err)
}

// AppendErrs appends newErrs to errors and returns the result.
// If newErrs is empty, nothing is appended.
func AppendErrs(errors []error, newErrs []error) Errors {
	if len(newErrs) == 0 {
		return errors
	}
	for _, e := range newErrs {
		errors = AppendErr(errors, e)
	}
	if len(errors) == 0 {
		return nil
	}
	return errors
}

// ToString returns a string representation of errors, with elements separated by separator string. Any nil errors in the
// slice are skipped.
func ToString(errors []error, separator string) string {
	var out strings.Builder
	for i, e := range errors {
		if e == nil {
			continue
		}
		if i != 0 {
			out.WriteString(separator)
		}
		out.WriteString(e.Error())
	}
	return out.String()
}

// EqualErrors reports whether a and b are equal, regardless of ordering.
func EqualErrors(a, b Errors) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]bool)
	for _, e := range b {
		m[e.Error()] = true
	}
	for _, ea := range a {
		if !m[ea.Error()] {
			return false
		}
	}
	return true
}
