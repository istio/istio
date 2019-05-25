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
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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
	outputLevel     atomic.Value
	stackTraceLevel atomic.Value
	logCallers      atomic.Value
}

var scopes = make(map[string]*Scope)
var lock = sync.RWMutex{}

// set by the Configure method
var writeFn atomic.Value
var errorSink atomic.Value

// RegisterScope registers a new logging scope. If the same name is used multiple times
// for a single process, the same Scope struct is returned.
//
// Scope names cannot include colons, commas, or periods.
func RegisterScope(name string, description string, callerSkip int) *Scope {
	if strings.ContainsAny(name, ":,.") {
		return nil
	}

	lock.Lock()
	defer lock.Unlock()

	s, ok := scopes[name]
	if !ok {
		s = &Scope{
			name:        name,
			description: description,
			callerSkip:  callerSkip,
		}
		s.SetOutputLevel(InfoLevel)
		s.SetStackTraceLevel(NoneLevel)
		s.SetLogCallers(false)

		if name != DefaultScopeName {
			s.nameToEmit = name
		}

		scopes[name] = s
	}

	return s
}

// FindScope returns a previously registered scope, or nil if the named scope wasn't previously registered
func FindScope(scope string) *Scope {
	lock.RLock()
	defer lock.RUnlock()

	s := scopes[scope]
	return s
}

// Scopes returns a snapshot of the currently defined set of scopes
func Scopes() map[string]*Scope {
	lock.RLock()
	defer lock.RUnlock()

	s := make(map[string]*Scope, len(scopes))
	for k, v := range scopes {
		s[k] = v
	}

	return s
}

// Fatal outputs a message at fatal level.
func (s *Scope) Fatal(msg string, fields ...zapcore.Field) {
	if s.GetOutputLevel() >= FatalLevel {
		s.emit(zapcore.FatalLevel, s.GetStackTraceLevel() >= FatalLevel, msg, fields)
	}
}

// Fatala uses fmt.Sprint to construct and log a message at fatal level.
func (s *Scope) Fatala(args ...interface{}) {
	if s.GetOutputLevel() >= FatalLevel {
		s.emit(zapcore.FatalLevel, s.GetStackTraceLevel() >= FatalLevel, fmt.Sprint(args...), nil)
	}
}

// Fatalf uses fmt.Sprintf to construct and log a message at fatal level.
func (s *Scope) Fatalf(template string, args ...interface{}) {
	if s.GetOutputLevel() >= FatalLevel {
		msg := template
		if len(args) > 0 {
			msg = fmt.Sprintf(template, args...)
		}
		s.emit(zapcore.FatalLevel, s.GetStackTraceLevel() >= FatalLevel, msg, nil)
	}
}

// FatalEnabled returns whether output of messages using this scope is currently enabled for fatal-level output.
func (s *Scope) FatalEnabled() bool {
	return s.GetOutputLevel() >= FatalLevel
}

// Error outputs a message at error level.
func (s *Scope) Error(msg string, fields ...zapcore.Field) {
	if s.GetOutputLevel() >= ErrorLevel {
		s.emit(zapcore.ErrorLevel, s.GetStackTraceLevel() >= ErrorLevel, msg, fields)
	}
}

// Errora uses fmt.Sprint to construct and log a message at error level.
func (s *Scope) Errora(args ...interface{}) {
	if s.GetOutputLevel() >= ErrorLevel {
		s.emit(zapcore.ErrorLevel, s.GetStackTraceLevel() >= ErrorLevel, fmt.Sprint(args...), nil)
	}
}

// Errorf uses fmt.Sprintf to construct and log a message at error level.
func (s *Scope) Errorf(template string, args ...interface{}) {
	if s.GetOutputLevel() >= ErrorLevel {
		msg := template
		if len(args) > 0 {
			msg = fmt.Sprintf(template, args...)
		}
		s.emit(zapcore.ErrorLevel, s.GetStackTraceLevel() >= ErrorLevel, msg, nil)
	}
}

// ErrorEnabled returns whether output of messages using this scope is currently enabled for error-level output.
func (s *Scope) ErrorEnabled() bool {
	return s.GetOutputLevel() >= ErrorLevel
}

// Warn outputs a message at warn level.
func (s *Scope) Warn(msg string, fields ...zapcore.Field) {
	if s.GetOutputLevel() >= WarnLevel {
		s.emit(zapcore.WarnLevel, s.GetStackTraceLevel() >= ErrorLevel, msg, fields)
	}
}

// Warna uses fmt.Sprint to construct and log a message at warn level.
func (s *Scope) Warna(args ...interface{}) {
	if s.GetOutputLevel() >= WarnLevel {
		s.emit(zapcore.WarnLevel, s.GetStackTraceLevel() >= ErrorLevel, fmt.Sprint(args...), nil)
	}
}

// Warnf uses fmt.Sprintf to construct and log a message at warn level.
func (s *Scope) Warnf(template string, args ...interface{}) {
	if s.GetOutputLevel() >= WarnLevel {
		msg := template
		if len(args) > 0 {
			msg = fmt.Sprintf(template, args...)
		}
		s.emit(zapcore.WarnLevel, s.GetStackTraceLevel() >= ErrorLevel, msg, nil)
	}
}

// WarnEnabled returns whether output of messages using this scope is currently enabled for warn-level output.
func (s *Scope) WarnEnabled() bool {
	return s.GetOutputLevel() >= WarnLevel
}

// Info outputs a message at info level.
func (s *Scope) Info(msg string, fields ...zapcore.Field) {
	if s.GetOutputLevel() >= InfoLevel {
		s.emit(zapcore.InfoLevel, s.GetStackTraceLevel() >= ErrorLevel, msg, fields)
	}
}

// Infoa uses fmt.Sprint to construct and log a message at info level.
func (s *Scope) Infoa(args ...interface{}) {
	if s.GetOutputLevel() >= InfoLevel {
		s.emit(zapcore.InfoLevel, s.GetStackTraceLevel() >= ErrorLevel, fmt.Sprint(args...), nil)
	}
}

// Infof uses fmt.Sprintf to construct and log a message at info level.
func (s *Scope) Infof(template string, args ...interface{}) {
	if s.GetOutputLevel() >= InfoLevel {
		msg := template
		if len(args) > 0 {
			msg = fmt.Sprintf(template, args...)
		}
		s.emit(zapcore.InfoLevel, s.GetStackTraceLevel() >= ErrorLevel, msg, nil)
	}
}

// InfoEnabled returns whether output of messages using this scope is currently enabled for info-level output.
func (s *Scope) InfoEnabled() bool {
	return s.GetOutputLevel() >= InfoLevel
}

// Debug outputs a message at debug level.
func (s *Scope) Debug(msg string, fields ...zapcore.Field) {
	if s.GetOutputLevel() >= DebugLevel {
		s.emit(zapcore.DebugLevel, s.GetStackTraceLevel() >= ErrorLevel, msg, fields)
	}
}

// Debuga uses fmt.Sprint to construct and log a message at debug level.
func (s *Scope) Debuga(args ...interface{}) {
	if s.GetOutputLevel() >= DebugLevel {
		s.emit(zapcore.DebugLevel, s.GetStackTraceLevel() >= ErrorLevel, fmt.Sprint(args...), nil)
	}
}

// Debugf uses fmt.Sprintf to construct and log a message at debug level.
func (s *Scope) Debugf(template string, args ...interface{}) {
	if s.GetOutputLevel() >= DebugLevel {
		msg := template
		if len(args) > 0 {
			msg = fmt.Sprintf(template, args...)
		}
		s.emit(zapcore.DebugLevel, s.GetStackTraceLevel() >= ErrorLevel, msg, nil)
	}
}

// DebugEnabled returns whether output of messages using this scope is currently enabled for debug-level output.
func (s *Scope) DebugEnabled() bool {
	return s.GetOutputLevel() >= DebugLevel
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

	if s.GetLogCallers() {
		e.Caller = zapcore.NewEntryCaller(runtime.Caller(s.callerSkip + callerSkipOffset))
	}

	if dumpStack {
		e.Stack = zap.Stack("").String
	}

	if w := writeFn.Load().(func(zapcore.Entry, []zapcore.Field) error); w != nil {
		if err := w(e, fields); err != nil {
			if es := errorSink.Load().(zapcore.WriteSyncer); es != nil {
				fmt.Fprintf(es, "%v log write error: %v\n", time.Now(), err)
				_ = es.Sync()
			}
		}
	}
}

// SetOutputLevel adjusts the output level associated with the scope.
func (s *Scope) SetOutputLevel(l Level) {
	s.outputLevel.Store(l)
}

// GetOutputLevel returns the output level associated with the scope.
func (s *Scope) GetOutputLevel() Level {
	return s.outputLevel.Load().(Level)
}

// SetStackTraceLevel adjusts the stack tracing level associated with the scope.
func (s *Scope) SetStackTraceLevel(l Level) {
	s.stackTraceLevel.Store(l)
}

// GetStackTraceLevel returns the stack tracing level associated with the scope.
func (s *Scope) GetStackTraceLevel() Level {
	return s.stackTraceLevel.Load().(Level)
}

// SetLogCallers adjusts the output level associated with the scope.
func (s *Scope) SetLogCallers(logCallers bool) {
	s.logCallers.Store(logCallers)
}

// GetLogCallers returns the output level associated with the scope.
func (s *Scope) GetLogCallers() bool {
	return s.logCallers.Load().(bool)
}
