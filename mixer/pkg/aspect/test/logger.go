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

// Package test provides utilities for testing the //pkg/aspect code.
package test

import (
	"errors"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/config"
)

// Logger is a test struct that implements the application-logs and access-logs aspects.
type Logger struct {
	adapter.AccessLogsBuilder
	adapter.ApplicationLogsBuilder

	DefaultCfg     config.AspectParams
	EntryCount     int
	Logs           []adapter.LogEntry
	AccessLogs     []adapter.LogEntry
	ErrOnNewAspect bool
	ErrOnLog       bool
	Closed         bool
}

// NewApplicationLogsAspect returns a new instance of the Logger aspect.
func (t *Logger) NewApplicationLogsAspect(adapter.Env, adapter.Config) (adapter.ApplicationLogsAspect, error) {
	if t.ErrOnNewAspect {
		return nil, errors.New("new aspect error")
	}
	return t, nil
}

// NewAccessLogsAspect returns a new instance of the accessLogger aspect.
func (t *Logger) NewAccessLogsAspect(adapter.Env, adapter.Config) (adapter.AccessLogsAspect, error) {
	if t.ErrOnNewAspect {
		return nil, errors.New("new aspect error")
	}
	return t, nil
}

// Name returns the official name of this builder.
func (t *Logger) Name() string { return "testLogger" }

// Description returns a user-friendly description of this builder.
func (t *Logger) Description() string { return "A test logger" }

// DefaultConfig returns a default configuration struct for this adapter.
func (t *Logger) DefaultConfig() adapter.Config { return t.DefaultCfg }

// ValidateConfig determines whether the given configuration meets all correctness requirements.
func (t *Logger) ValidateConfig(c adapter.Config) (ce *adapter.ConfigErrors) { return nil }

// Log simulates processing a batch of log entries.
func (t *Logger) Log(l []adapter.LogEntry) error {
	if t.ErrOnLog {
		return errors.New("log error")
	}
	t.EntryCount += len(l)
	t.Logs = append(t.Logs, l...)
	return nil
}

// LogAccess simulates processing a batch of access log entries.
func (t *Logger) LogAccess(l []adapter.LogEntry) error {
	if t.ErrOnLog {
		return errors.New("log access error")
	}
	t.EntryCount += len(l)
	t.AccessLogs = append(t.AccessLogs, l...)
	return nil
}

// Close marks the logger as being closed.
func (t *Logger) Close() error {
	t.Closed = true
	return nil
}
