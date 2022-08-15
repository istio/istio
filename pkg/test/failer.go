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

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"

	"istio.io/pkg/log"
)

var (
	_ Failer = &testing.T{}
	_ Failer = &testing.B{}
	_ Failer = &errorWrapper{}
)

// Failer is an interface to be provided to test functions of the form XXXOrFail. This is a
// substitute for testing.TB, which cannot be implemented outside of the testing
// package.
type Failer interface {
	Fail()
	FailNow()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
	Log(args ...any)
	Logf(format string, args ...any)
	TempDir() string
	Helper()
	Cleanup(func())
}

// errorWrapper is a Failer that can be used to just extract an `error`. This allows mixing
// functions that take in a Failer and those that take an error.
// The function must be called within a goroutine, or calls to Fatal will try to terminate the outer
// test context, which will cause the test to panic. The Wrap function handles this automatically
type errorWrapper struct {
	mu      sync.RWMutex
	failed  error
	cleanup func()
}

// Wrap executes a function with a fake Failer, and returns an error if the test failed. This allows
// calling functions that take a Failer and using them with functions that expect an error, or
// allowing calling functions that would cause a test to immediately fail to instead return an error.
// Wrap handles Cleanup() and short-circuiting of Fatal() just like the real testing.T.
func Wrap(f func(t Failer)) error {
	done := make(chan struct{})
	w := &errorWrapper{}
	go func() {
		defer close(done)
		f(w)
	}()
	<-done
	return w.ToErrorCleanup()
}

// ToErrorCleanup returns any errors encountered and executes any cleanup actions
func (e *errorWrapper) ToErrorCleanup() error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.cleanup != nil {
		e.cleanup()
	}
	return e.failed
}

func (e *errorWrapper) Fail() {
	e.Fatal("fail called")
}

func (e *errorWrapper) FailNow() {
	e.Fatal("fail now called")
}

func (e *errorWrapper) Fatal(args ...any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.failed == nil {
		e.failed = errors.New(fmt.Sprint(args...))
	}
	runtime.Goexit()
}

func (e *errorWrapper) Fatalf(format string, args ...any) {
	e.Fatal(fmt.Sprintf(format, args...))
}

func (e *errorWrapper) Helper() {
}

func (e *errorWrapper) Cleanup(f func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	oldCleanup := e.cleanup
	e.cleanup = func() {
		if oldCleanup != nil {
			defer func() {
				oldCleanup()
			}()
		}
		f()
	}
}

func (e *errorWrapper) Log(args ...any) {
	log.Info(args...)
}

func (e *errorWrapper) Logf(format string, args ...any) {
	ag := []any{format}
	ag = append(ag, args...)
	log.Infof(ag...)
}

func (e *errorWrapper) TempDir() string {
	tempDir, err := os.MkdirTemp("", "test")
	if err == nil {
		e.Cleanup(func() {
			os.RemoveAll(tempDir)
		})
	}
	return tempDir
}
