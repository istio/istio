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

package test

import (
	"fmt"
	"sync"
	"testing"

	"istio.io/mixer/pkg/adapter"
)

// Env is an adapter environment that defers to the testing context t. Tracks all messages logged so they can be tested against.
type Env struct {
	t *testing.T

	lock sync.Mutex // guards logs
	logs []string
}

// NewEnv returns an adapter environment that redirects logging output to the given testing context.
func NewEnv(t *testing.T) *Env {
	return &Env{t, sync.Mutex{}, make([]string, 0)}
}

// Logger returns a logger that writes to testing.T.Log
func (e *Env) Logger() adapter.Logger {
	return e
}

// ScheduleWork runs the given function asynchronously.
func (e *Env) ScheduleWork(fn adapter.WorkFunc) {
	go fn()
}

// ScheduleDaemon runs the given function asynchronously.
func (e *Env) ScheduleDaemon(fn adapter.DaemonFunc) {
	go fn()
}

// Infof logs the provided message.
func (e *Env) Infof(format string, args ...interface{}) {
	e.log(format, args...)
}

// Warningf logs the provided message.
func (e *Env) Warningf(format string, args ...interface{}) {
	e.log(format, args...)
}

// Errorf logs the provided message and returns it as an error.
func (e *Env) Errorf(format string, args ...interface{}) error {
	s := e.log(format, args...)
	return fmt.Errorf(s)
}

// VerbosityLevel return true for test envs (all verbosity levels enabled).
func (e *Env) VerbosityLevel(level adapter.VerbosityLevel) bool {
	return true
}

// GetLogs returns a snapshot of all logs that've been written to this environment
func (e *Env) GetLogs() []string {
	e.lock.Lock()
	snapshot := make([]string, len(e.logs))
	_ = copy(snapshot, e.logs)
	e.lock.Unlock()
	return snapshot
}

func (e *Env) log(format string, args ...interface{}) string {
	l := fmt.Sprintf(format, args...)

	e.lock.Lock()
	e.logs = append(e.logs, l)
	e.lock.Unlock()

	e.t.Log(l)
	return l
}
