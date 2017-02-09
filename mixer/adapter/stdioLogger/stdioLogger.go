// Copyright 2017 Google Inc.
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

// Package stdioLogger provides an implementation of the mixer logger aspect
// that writes logs (serialized as JSON) to a standard stream (stdout | stderr).
package stdioLogger

import (
	"encoding/json"
	"io"
	"os"

	"istio.io/mixer/adapter/stdioLogger/config"
	"istio.io/mixer/pkg/adapter"

	me "github.com/hashicorp/go-multierror"
)

type (
	builder struct{ adapter.DefaultBuilder }
	logger  struct{ logStream io.Writer }
)

// Register records the builders exposed by this adapter.
func Register(r adapter.Registrar) {
	b := builder{adapter.NewDefaultBuilder(
		"stdioLogger",
		"Writes log entries to a standard I/O stream",
		&config.Params{},
	)}

	r.RegisterApplicationLogsBuilder(b)
	r.RegisterAccessLogsBuilder(b)
}

func (builder) NewApplicationLogsAspect(env adapter.Env, cfg adapter.AspectConfig) (adapter.ApplicationLogsAspect, error) {
	return newLogger(env, cfg)
}

func (builder) NewAccessLogsAspect(env adapter.Env, cfg adapter.AspectConfig) (adapter.AccessLogsAspect, error) {
	return newLogger(env, cfg)
}

func newLogger(env adapter.Env, cfg adapter.AspectConfig) (*logger, error) {
	c := cfg.(*config.Params)

	w := os.Stderr
	if c.LogStream == config.STDOUT {
		w = os.Stdout
	}

	return &logger{w}, nil
}

func (l *logger) Log(entries []adapter.LogEntry) error {
	return l.log(entries)
}

func (l *logger) LogAccess(entries []adapter.LogEntry) error {
	return l.log(entries)
}

func (l *logger) log(entries []adapter.LogEntry) error {
	var errors *me.Error
	for _, entry := range entries {
		if err := writeJSON(l.logStream, entry); err != nil {
			errors = me.Append(errors, err)
		}
	}

	return errors.ErrorOrNil()
}

func (l *logger) Close() error { return nil }

func writeJSON(w io.Writer, le interface{}) error {
	return json.NewEncoder(w).Encode(le)
}
