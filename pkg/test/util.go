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

package test

import (
	"os"

	"go.uber.org/atomic"
	"golang.org/x/net/context"
)

// SetForTest sets a variable for the duration of a test, then resets it once the test is complete.
func SetForTest[T any](t Failer, vv *T, v T) {
	old := *vv
	*vv = v
	t.Cleanup(func() {
		*vv = old
	})
}

// SetEnvForTest sets an environment variable for the duration of a test, then resets it once the test is complete.
func SetEnvForTest(t Failer, k, v string) {
	old := os.Getenv(k)
	if err := os.Setenv(k, v); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Setenv(k, old); err != nil {
			t.Fatal(err)
		}
	})
}

// SetAtomicBoolForTest sets a variable for the duration of a test, then resets it once the test is complete atomically.
func SetAtomicBoolForTest(t Failer, vv *atomic.Bool, v bool) {
	old := vv.Load()
	vv.Store(v)
	t.Cleanup(func() {
		vv.Store(old)
	})
}

// NewStop returns a stop channel that will automatically be closed when the test is complete
func NewStop(t Failer) chan struct{} {
	s := make(chan struct{})
	t.Cleanup(func() {
		close(s)
	})
	return s
}

// NewContext returns a context that will automatically be closed when the test is complete
func NewContext(t Failer) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}
