// Copyright Istio Authors.
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

package test

import "testing"

var _ Failer = &testing.T{}

// Failer is an interface to be provided to test functions of the form XXXOrFail. This is a
// substitute for testing.TB, which cannot be implemented outside of the testing
// package.
type Failer interface {
	Fail()
	FailNow()
	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})
	Helper()
	Cleanup(func())
}
