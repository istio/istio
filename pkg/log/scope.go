// Copyright 2018 Istio Authors
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

package log

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Scope let's you log data for an area of code, enabling the user full control over
// the level of logging output produced.
type Scope struct {
	// immutable, set at creation
	name        string
	nameToEmit  string
	description string
	callerSkip  int

	// set by the Configure method and adjustable dynamically
	Level           Level
	StackTraceLevel Level
	LogCallers      bool
}

var scopes = make(map[string]*Scope, 0)
var lock = sync.Mutex{}

// set by the Configure method
var writeFn = nopWrite
var errorSink zapcore.WriteSyncer = os.Stderr

// RegisterScope registers a new logging scope. If the same name is used multiple times
// for a single process, the same Scope struct is returned.
//
// Scope names cannot include colons, commas, or periods.
func RegisterScope(name string, description string, callerSkip int) *Scope {
	if strings.IndexAny(name, ":,.") >= 0 {
		return nil
	}

	lock.Lock()
	defer lock.Unlock()

	s, ok := scopes[name]
	if !ok {
		s = &Scope{
			name:            name,
			description:     description,
			callerSkip:      callerSkip,
			LogCallers:      false,
			Level:           InfoLevel,
			StackTraceLevel: NoneLevel,
		}

		if name != DefaultScopeName {
			s.nameToEmit = name
		}

		scopes[name] = s
	}

	return s
}

// FindScope returns a previously registered scope, or nil if the named scope wasn't previously registered
func FindScope(scope string) *Scope {
	lock.Lock()
	defer lock.Unlock()

	s, _ := scopes[scope]
	return s
}

// Error outputs a message at error level.
func (s *Scope) Error(msg string, fields ...zapcore.Field) {
	if s.Level >= ErrorLevel {
		s.emit(zapcore.ErrorLevel, s.StackTraceLevel >= ErrorLevel, msg, fields)
	}
}

// Errora uses fmt.Sprint to construct and log a message at error level.
func (s *Scope) Errora(args ...interface{}) {
	if s.Level >= ErrorLevel {
		s.emit(zapcore.ErrorLevel, s.StackTraceLevel >= ErrorLevel, fmt.Sprint(args...), nil)
	}
}

// Errorf uses fmt.Sprintf to construct and log a message at error level.
func (s *Scope) Errorf(template string, args ...interface{}) {
	if s.Level >= ErrorLevel {
		msg := template
		if len(args) > 0 {
			msg = fmt.Sprintf(template, args...)
		}
		s.emit(zapcore.ErrorLevel, s.StackTraceLevel >= ErrorLevel, msg, nil)
	}
}

// ErrorEnabled returns whether output of messages using this scope is currently enabled for error-level output.
func (s *Scope) ErrorEnabled() bool {
	return s.Level >= ErrorLevel
}

// Warn outputs a message at warn level.
func (s *Scope) Warn(msg string, fields ...zapcore.Field) {
	if s.Level >= WarnLevel {
		s.emit(zapcore.WarnLevel, s.StackTraceLevel >= ErrorLevel, msg, fields)
	}
}

// Warna uses fmt.Sprint to construct and log a message at warn level.
func (s *Scope) Warna(args ...interface{}) {
	if s.Level >= WarnLevel {
		s.emit(zapcore.WarnLevel, s.StackTraceLevel >= ErrorLevel, fmt.Sprint(args...), nil)
	}
}

// Warnf uses fmt.Sprintf to construct and log a message at warn level.
func (s *Scope) Warnf(template string, args ...interface{}) {
	if s.Level >= WarnLevel {
		msg := template
		if len(args) > 0 {
			msg = fmt.Sprintf(template, args...)
		}
		s.emit(zapcore.WarnLevel, s.StackTraceLevel >= ErrorLevel, msg, nil)
	}
}

// WarnEnabled returns whether output of messages using this scope is currently enabled for warn-level output.
func (s *Scope) WarnEnabled() bool {
	return s.Level >= WarnLevel
}

// Info outputs a message at info level.
func (s *Scope) Info(msg string, fields ...zapcore.Field) {
	if s.Level >= InfoLevel {
		s.emit(zapcore.InfoLevel, s.StackTraceLevel >= ErrorLevel, msg, fields)
	}
}

// Infoa uses fmt.Sprint to construct and log a message at info level.
func (s *Scope) Infoa(args ...interface{}) {
	if s.Level >= InfoLevel {
		s.emit(zapcore.InfoLevel, s.StackTraceLevel >= ErrorLevel, fmt.Sprint(args...), nil)
	}
}

// Infof uses fmt.Sprintf to construct and log a message at info level.
func (s *Scope) Infof(template string, args ...interface{}) {
	if s.Level >= InfoLevel {
		msg := template
		if len(args) > 0 {
			msg = fmt.Sprintf(template, args...)
		}
		s.emit(zapcore.InfoLevel, s.StackTraceLevel >= ErrorLevel, msg, nil)
	}
}

// InfoEnabled returns whether output of messages using this scope is currently enabled for info-level output.
func (s *Scope) InfoEnabled() bool {
	return s.Level >= InfoLevel
}

// Debug outputs a message at debug level.
func (s *Scope) Debug(msg string, fields ...zapcore.Field) {
	if s.Level >= DebugLevel {
		s.emit(zapcore.DebugLevel, s.StackTraceLevel >= ErrorLevel, msg, fields)
	}
}

// Debuga uses fmt.Sprint to construct and log a message at debug level.
func (s *Scope) Debuga(args ...interface{}) {
	if s.Level >= DebugLevel {
		s.emit(zapcore.DebugLevel, s.StackTraceLevel >= ErrorLevel, fmt.Sprint(args...), nil)
	}
}

// Debugf uses fmt.Sprintf to construct and log a message at debug level.
func (s *Scope) Debugf(template string, args ...interface{}) {
	if s.Level >= DebugLevel {
		msg := template
		if len(args) > 0 {
			msg = fmt.Sprintf(template, args...)
		}
		s.emit(zapcore.DebugLevel, s.StackTraceLevel >= ErrorLevel, msg, nil)
	}
}

// DebugEnabled returns whether output of messages using this scope is currently enabled for debug-level output.
func (s *Scope) DebugEnabled() bool {
	return s.Level >= DebugLevel
}

// Name returns this scope's name.
func (s *Scope) Name() string {
	return s.name
}

// Description returns this scope's description
func (s *Scope) Description() string {
	return s.description
}

const callerSkipOffset = 2

func (s *Scope) emit(level zapcore.Level, dumpStack bool, msg string, fields []zapcore.Field) {
	e := zapcore.Entry{
		Message:    msg,
		Level:      level,
		Time:       time.Now(),
		LoggerName: s.nameToEmit,
	}

	if s.LogCallers {
		e.Caller = zapcore.NewEntryCaller(runtime.Caller(s.callerSkip + callerSkipOffset))
	}

	if dumpStack {
		e.Stack = zap.Stack("").String
	}

	if writeFn != nil {
		if err := writeFn(e, fields); err != nil {
			fmt.Fprintf(errorSink, "%v log write error: %v\n", time.Now(), err)
			errorSink.Sync()
		}
	}
}

func nopWrite(zapcore.Entry, []zapcore.Field) error {
	return nil
}
