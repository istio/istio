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

package log

// These functions enable logging using a global Scope. See scope.go for usage information.

func registerDefaultScope() *Scope {
	return registerScope(DefaultScopeName, "Unscoped logging messages.", 1)
}

var defaultScope = registerDefaultScope()

// Fatal outputs a message at fatal level.
func Fatal(fields ...any) {
	defaultScope.Fatal(fields...)
}

// Fatalf uses fmt.Sprintf to construct and log a message at fatal level.
func Fatalf(args ...any) {
	defaultScope.Fatalf(args...)
}

// FatalEnabled returns whether output of messages using this scope is currently enabled for fatal-level output.
func FatalEnabled() bool {
	return defaultScope.FatalEnabled()
}

// Error outputs a message at error level.
func Error(fields any) {
	defaultScope.Error(fields)
}

// Errorf uses fmt.Sprintf to construct and log a message at error level.
func Errorf(args ...any) {
	defaultScope.Errorf(args...)
}

// ErrorEnabled returns whether output of messages using this scope is currently enabled for error-level output.
func ErrorEnabled() bool {
	return defaultScope.ErrorEnabled()
}

// Warn outputs a message at warn level.
func Warn(fields any) {
	defaultScope.Warn(fields)
}

// Warnf uses fmt.Sprintf to construct and log a message at warn level.
func Warnf(args ...any) {
	defaultScope.Warnf(args...)
}

// WarnEnabled returns whether output of messages using this scope is currently enabled for warn-level output.
func WarnEnabled() bool {
	return defaultScope.WarnEnabled()
}

// Info outputs a message at info level.
func Info(fields any) {
	defaultScope.Info(fields)
}

// Infof uses fmt.Sprintf to construct and log a message at info level.
func Infof(args ...any) {
	defaultScope.Infof(args...)
}

// InfoEnabled returns whether output of messages using this scope is currently enabled for info-level output.
func InfoEnabled() bool {
	return defaultScope.InfoEnabled()
}

// Debug outputs a message at debug level.
func Debug(fields any) {
	defaultScope.Debug(fields)
}

// Debugf uses fmt.Sprintf to construct and log a message at debug level.
func Debugf(args ...any) {
	defaultScope.Debugf(args...)
}

// DebugEnabled returns whether output of messages using this scope is currently enabled for debug-level output.
func DebugEnabled() bool {
	return defaultScope.DebugEnabled()
}

// WithLabels adds a key-value pairs to the labels in s. The key must be a string, while the value may be any type.
// It returns a copy of the default scope, with the labels added.
func WithLabels(kvlist ...any) *Scope {
	return defaultScope.WithLabels(kvlist...)
}
