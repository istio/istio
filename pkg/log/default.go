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

import (
	"fmt"

	"go.uber.org/zap/zapcore"
)

var defaultScope = RegisterScope(DefaultScopeName, "Unscoped logging messages.", 0)

// Error outputs a message at error level.
func Error(msg string, fields ...zapcore.Field) {
	if defaultScope.GetOutputLevel() >= ErrorLevel {
		defaultScope.emit(zapcore.ErrorLevel, defaultScope.GetStackTraceLevel() >= ErrorLevel, msg, fields)
	}
}

// Errora uses fmt.Sprint to construct and log a message at error level.
func Errora(args ...interface{}) {
	if defaultScope.GetOutputLevel() >= ErrorLevel {
		defaultScope.emit(zapcore.ErrorLevel, defaultScope.GetStackTraceLevel() >= ErrorLevel, fmt.Sprint(args...), nil)
	}
}

// Errorf uses fmt.Sprintf to construct and log a message at error level.
func Errorf(template string, args ...interface{}) {
	if defaultScope.GetOutputLevel() >= ErrorLevel {
		msg := template
		if len(args) > 0 {
			msg = fmt.Sprintf(template, args...)
		}
		defaultScope.emit(zapcore.ErrorLevel, defaultScope.GetStackTraceLevel() >= ErrorLevel, msg, nil)
	}
}

// ErrorEnabled returns whether output of messages using this scope is currently enabled for error-level output.
func ErrorEnabled() bool {
	return defaultScope.GetOutputLevel() >= ErrorLevel
}

// Warn outputs a message at warn level.
func Warn(msg string, fields ...zapcore.Field) {
	if defaultScope.GetOutputLevel() >= WarnLevel {
		defaultScope.emit(zapcore.WarnLevel, defaultScope.GetStackTraceLevel() >= WarnLevel, msg, fields)
	}
}

// Warna uses fmt.Sprint to construct and log a message at warn level.
func Warna(args ...interface{}) {
	if defaultScope.GetOutputLevel() >= WarnLevel {
		defaultScope.emit(zapcore.WarnLevel, defaultScope.GetStackTraceLevel() >= WarnLevel, fmt.Sprint(args...), nil)
	}
}

// Warnf uses fmt.Sprintf to construct and log a message at warn level.
func Warnf(template string, args ...interface{}) {
	if defaultScope.GetOutputLevel() >= WarnLevel {
		msg := template
		if len(args) > 0 {
			msg = fmt.Sprintf(template, args...)
		}
		defaultScope.emit(zapcore.WarnLevel, defaultScope.GetStackTraceLevel() >= WarnLevel, msg, nil)
	}
}

// WarnEnabled returns whether output of messages using this scope is currently enabled for warn-level output.
func WarnEnabled() bool {
	return defaultScope.GetOutputLevel() >= WarnLevel
}

// Info outputs a message at info level.
func Info(msg string, fields ...zapcore.Field) {
	if defaultScope.GetOutputLevel() >= InfoLevel {
		defaultScope.emit(zapcore.InfoLevel, defaultScope.GetStackTraceLevel() >= InfoLevel, msg, fields)
	}
}

// Infoa uses fmt.Sprint to construct and log a message at info level.
func Infoa(args ...interface{}) {
	if defaultScope.GetOutputLevel() >= InfoLevel {
		defaultScope.emit(zapcore.InfoLevel, defaultScope.GetStackTraceLevel() >= InfoLevel, fmt.Sprint(args...), nil)
	}
}

// Infof uses fmt.Sprintf to construct and log a message at info level.
func Infof(template string, args ...interface{}) {
	if defaultScope.GetOutputLevel() >= InfoLevel {
		msg := template
		if len(args) > 0 {
			msg = fmt.Sprintf(template, args...)
		}
		defaultScope.emit(zapcore.InfoLevel, defaultScope.GetStackTraceLevel() >= InfoLevel, msg, nil)
	}
}

// InfoEnabled returns whether output of messages using this scope is currently enabled for info-level output.
func InfoEnabled() bool {
	return defaultScope.GetOutputLevel() >= InfoLevel
}

// Debug outputs a message at debug level.
func Debug(msg string, fields ...zapcore.Field) {
	if defaultScope.GetOutputLevel() >= DebugLevel {
		defaultScope.emit(zapcore.DebugLevel, defaultScope.GetStackTraceLevel() >= DebugLevel, msg, fields)
	}
}

// Debuga uses fmt.Sprint to construct and log a message at debug level.
func Debuga(args ...interface{}) {
	if defaultScope.GetOutputLevel() >= DebugLevel {
		defaultScope.emit(zapcore.DebugLevel, defaultScope.GetStackTraceLevel() >= DebugLevel, fmt.Sprint(args...), nil)
	}
}

// Debugf uses fmt.Sprintf to construct and log a message at debug level.
func Debugf(template string, args ...interface{}) {
	if defaultScope.GetOutputLevel() >= DebugLevel {
		msg := template
		if len(args) > 0 {
			msg = fmt.Sprintf(template, args...)
		}
		defaultScope.emit(zapcore.DebugLevel, defaultScope.GetStackTraceLevel() >= DebugLevel, msg, nil)
	}
}

// DebugEnabled returns whether output of messages using this scope is currently enabled for debug-level output.
func DebugEnabled() bool {
	return defaultScope.GetOutputLevel() >= DebugLevel
}
