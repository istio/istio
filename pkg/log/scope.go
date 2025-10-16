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

// Scope constrains logging control to a named scope level. It gives users a fine grained control over output severity
// threshold and stack traces.
//
// Scope supports structured logging using WithLabels:
//
//	s := RegisterScope("MyScope", "Description", 0)
//	s = s.WithLabels("foo", "bar", "baz", 123, "qux", 0.123)
//	s.Info("Hello")                      // <time>   info   MyScope   Hello  foo=bar baz=123 qux=0.123
//
// The output format can be globally configured to be JSON instead, using Options in this package.
//
//	e.g. <time>   info   MyScope   { "message":"Hello","foo":"bar","baz":123 }
//
// Scope also supports an error dictionary. The caller can pass a *structured.Error object as the first parameter
// to any of the output functions (Fatal*, Error* etc.) and this will append the fields in the object to the output:
//
//	e := &structured.Error{MoreInfo:"See the documentation in istio.io/helpful_link"}
//	s.WithLabels("foo", "bar").Error(e, "Hello")
//	  <time>   info   MyScope   Hello  moreInfo=See the documentation in istio.io/helpful_link foo=bar
//
// See structured.Error for additional guidance on defining errors in a dictionary.
type Scope struct {
	// immutable, set at creation
	name        string
	nameToEmit  string
	description string
	callerSkip  int

	// set by the Configure method and adjustable dynamically
	outputLevel     *atomic.Value
	stackTraceLevel *atomic.Value
	logCallers      *atomic.Value

	// labels data - key slice to preserve ordering
	labelKeys []string
	labels    map[string]any
}

var (
	scopes = make(map[string]*Scope)
	lock   sync.RWMutex
)

// RegisterScope registers a new logging scope. If the same name is used multiple times
// for a single process, the same Scope struct is returned.
//
// Scope names cannot include colons, commas, or periods.
func RegisterScope(name string, description string) *Scope {
	// We only allow internal callers to set callerSkip
	return registerScope(name, description, 0)
}

func registerScope(name string, description string, callerSkip int) *Scope {
	if strings.ContainsAny(name, ":,.") {
		panic(fmt.Sprintf("scope name %s is invalid, it cannot contain colons, commas, or periods", name))
	}

	lock.Lock()
	defer lock.Unlock()

	s, ok := scopes[name]
	if !ok {
		s = &Scope{
			name:            name,
			description:     description,
			callerSkip:      callerSkip,
			outputLevel:     &atomic.Value{},
			stackTraceLevel: &atomic.Value{},
			logCallers:      &atomic.Value{},
		}
		s.SetOutputLevel(InfoLevel)
		s.SetStackTraceLevel(NoneLevel)
		s.SetLogCallers(false)

		if name != DefaultScopeName {
			s.nameToEmit = name
		}

		scopes[name] = s
	}

	s.labels = make(map[string]any)

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

// Fatal uses fmt.Sprint to construct and log a message at fatal level.
func (s *Scope) Fatal(msg any) {
	if s.GetOutputLevel() >= FatalLevel {
		s.emit(zapcore.FatalLevel, fmt.Sprint(msg))
	}
}

// Fatalf uses fmt.Sprintf to construct and log a message at fatal level.
func (s *Scope) Fatalf(format string, args ...any) {
	if s.GetOutputLevel() >= FatalLevel {
		msg := maybeSprintf(format, args)
		s.emit(zapcore.FatalLevel, msg)
	}
}

// FatalEnabled returns whether output of messages using this scope is currently enabled for fatal-level output.
func (s *Scope) FatalEnabled() bool {
	return s.GetOutputLevel() >= FatalLevel
}

// Error outputs a message at error level.
func (s *Scope) Error(msg any) {
	if s.GetOutputLevel() >= ErrorLevel {
		s.emit(zapcore.ErrorLevel, fmt.Sprint(msg))
	}
}

// Errorf uses fmt.Sprintf to construct and log a message at error level.
func (s *Scope) Errorf(format string, args ...any) {
	if s.GetOutputLevel() >= ErrorLevel {
		msg := maybeSprintf(format, args)
		s.emit(zapcore.ErrorLevel, msg)
	}
}

// ErrorEnabled returns whether output of messages using this scope is currently enabled for error-level output.
func (s *Scope) ErrorEnabled() bool {
	return s.GetOutputLevel() >= ErrorLevel
}

// Warn outputs a message at warn level.
func (s *Scope) Warn(msg any) {
	if s.GetOutputLevel() >= WarnLevel {
		s.emit(zapcore.WarnLevel, fmt.Sprint(msg))
	}
}

// Warnf uses fmt.Sprintf to construct and log a message at warn level.
func (s *Scope) Warnf(format string, args ...any) {
	if s.GetOutputLevel() >= WarnLevel {
		msg := maybeSprintf(format, args)
		s.emit(zapcore.WarnLevel, msg)
	}
}

// WarnEnabled returns whether output of messages using this scope is currently enabled for warn-level output.
func (s *Scope) WarnEnabled() bool {
	return s.GetOutputLevel() >= WarnLevel
}

// Info outputs a message at info level.
func (s *Scope) Info(msg any) {
	if s.GetOutputLevel() >= InfoLevel {
		s.emit(zapcore.InfoLevel, fmt.Sprint(msg))
	}
}

// Infof uses fmt.Sprintf to construct and log a message at info level.
func (s *Scope) Infof(format string, args ...any) {
	if s.GetOutputLevel() >= InfoLevel {
		msg := maybeSprintf(format, args)
		s.emit(zapcore.InfoLevel, msg)
	}
}

// InfoEnabled returns whether output of messages using this scope is currently enabled for info-level output.
func (s *Scope) InfoEnabled() bool {
	return s.GetOutputLevel() >= InfoLevel
}

// Debug outputs a message at debug level.
func (s *Scope) Debug(msg any) {
	if s.GetOutputLevel() >= DebugLevel {
		s.emit(zapcore.DebugLevel, fmt.Sprint(msg))
	}
}

// LogWithTime outputs a message with a given timestamp.
func (s *Scope) LogWithTime(level Level, msg string, t time.Time) {
	if s.GetOutputLevel() >= level {
		s.emitWithTime(levelToZap[level], msg, t)
	}
}

// Debugf uses fmt.Sprintf to construct and log a message at debug level.
func (s *Scope) Debugf(format string, args ...any) {
	if s.GetOutputLevel() >= DebugLevel {
		msg := maybeSprintf(format, args)
		s.emit(zapcore.DebugLevel, msg)
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

// copy makes a copy of s and returns a pointer to it.
func (s *Scope) copy() *Scope {
	out := *s
	out.labels = copyStringInterfaceMap(s.labels)
	return &out
}

// WithLabels adds a key-value pairs to the labels in s. The key must be a string, while the value may be any type.
// It returns a copy of s, with the labels added.
// e.g. newScope := oldScope.WithLabels("foo", "bar", "baz", 123, "qux", 0.123)
func (s *Scope) WithLabels(kvlist ...any) *Scope {
	out := s.copy()
	if len(kvlist)%2 != 0 {
		out.labels["WithLabels error"] = fmt.Sprintf("even number of parameters required, got %d", len(kvlist))
		return out
	}

	for i := 0; i < len(kvlist); i += 2 {
		keyi := kvlist[i]
		key, ok := keyi.(string)
		if !ok {
			out.labels["WithLabels error"] = fmt.Sprintf("label name %v must be a string, got %T ", keyi, keyi)
			return out
		}
		_, override := out.labels[key]
		out.labels[key] = kvlist[i+1]
		if override {
			// Key already set, just modify the value
			continue
		}
		out.labelKeys = append(out.labelKeys, key)
	}
	return out
}

func (s *Scope) emit(level zapcore.Level, msg string) {
	s.emitWithTime(level, msg, time.Now())
}

func (s *Scope) emitWithTime(level zapcore.Level, msg string, t time.Time) {
	if t.IsZero() {
		t = time.Now()
	}

	e := zapcore.Entry{
		Message:    msg,
		Level:      level,
		Time:       t,
		LoggerName: s.nameToEmit,
	}

	if s.GetLogCallers() {
		e.Caller = zapcore.NewEntryCaller(runtime.Caller(s.callerSkip + callerSkipOffset))
	}

	if dumpStack(level, s) {
		e.Stack = zap.Stack("").String
	}

	var fields []zapcore.Field
	if useJSON.Load().(bool) {
		fields = make([]zapcore.Field, 0, len(s.labelKeys))
		for _, k := range s.labelKeys {
			v := s.labels[k]
			if d, ok := v.(time.Duration); ok {
				fields = append(fields, zap.Field{
					Key:     k,
					Integer: int64(d),
					Type:    zapcore.DurationType,
				})
			} else {
				fields = append(fields, zap.Field{
					Key:       k,
					Interface: v,
					Type:      zapcore.ReflectType,
				})
			}
		}
	} else if len(s.labelKeys) > 0 {
		sb := &strings.Builder{}
		// Assume roughly 15 chars per kv pair. Its fine to be off, this is just an optimization
		sb.Grow(len(msg) + 15*len(s.labelKeys))
		sb.WriteString(msg)
		sb.WriteString("\t")
		space := false
		for _, k := range s.labelKeys {
			if space {
				sb.WriteString(" ")
			}
			sb.WriteString(k)
			sb.WriteString("=")
			sb.WriteString(fmt.Sprint(s.labels[k]))
			space = true
		}
		e.Message = sb.String()
	}

	pt := funcs.Load().(patchTable)
	if pt.write != nil {
		if err := pt.write(e, fields); err != nil {
			_, _ = fmt.Fprintf(pt.errorSink, "%v log write error: %v\n", time.Now(), err)
			_ = pt.errorSink.Sync()
		}
	}
}

func copyStringInterfaceMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func maybeSprintf(format string, args []any) string {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	return msg
}
