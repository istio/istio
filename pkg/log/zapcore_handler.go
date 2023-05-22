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
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"istio.io/istio/pkg/structured"
)

var (
	toLevel = map[zapcore.Level]Level{
		zapcore.FatalLevel: FatalLevel,
		zapcore.ErrorLevel: ErrorLevel,
		zapcore.WarnLevel:  WarnLevel,
		zapcore.InfoLevel:  InfoLevel,
		zapcore.DebugLevel: DebugLevel,
	}
	toZapLevel = map[Level]zapcore.Level{
		FatalLevel: zapcore.FatalLevel,
		ErrorLevel: zapcore.ErrorLevel,
		WarnLevel:  zapcore.WarnLevel,
		InfoLevel:  zapcore.InfoLevel,
		DebugLevel: zapcore.DebugLevel,
	}
)

func init() {
	registerDefaultHandler(ZapLogHandlerCallbackFunc)
}

// ZapLogHandlerCallbackFunc is the handler function that emulates the previous Istio logging output and adds
// support for errdict package and labels logging.
func ZapLogHandlerCallbackFunc(
	level Level,
	scope *Scope,
	ie *structured.Error,
	msg string,
) {
	var fields []zapcore.Field
	if useJSON.Load().(bool) {
		if ie != nil {
			fields = appendNotEmptyField(fields, "message", msg)
			// Unlike zap, don't leave the message in CLI format.
			msg = ""
			fields = appendNotEmptyField(fields, "moreInfo", ie.MoreInfo)
			fields = appendNotEmptyField(fields, "impact", ie.Impact)
			fields = appendNotEmptyField(fields, "action", ie.Action)
			fields = appendNotEmptyField(fields, "likelyCause", ie.LikelyCause)
			fields = appendNotEmptyField(fields, "err", toErrString(ie.Err))
		}
		for _, k := range scope.labelKeys {
			v := scope.labels[k]
			fields = append(fields, zap.Field{
				Key:       k,
				Type:      zapcore.ReflectType,
				Interface: v,
			})
		}
	} else {
		sb := &strings.Builder{}
		sb.WriteString(msg)
		if ie != nil || len(scope.labelKeys) > 0 {
			sb.WriteString("\t")
		}
		if ie != nil {
			appendNotEmptyString(sb, "moreInfo", ie.MoreInfo)
			appendNotEmptyString(sb, "impact", ie.Impact)
			appendNotEmptyString(sb, "action", ie.Action)
			appendNotEmptyString(sb, "likelyCause", ie.LikelyCause)
			appendNotEmptyString(sb, "err", toErrString(ie.Err))
		}
		space := false
		for _, k := range scope.labelKeys {
			if space {
				sb.WriteString(" ")
			}
			sb.WriteString(fmt.Sprintf("%s=%v", k, scope.labels[k]))
			space = true
		}
		msg = sb.String()
	}
	emit(scope, toZapLevel[level], msg, fields)
}

// appendNotEmptyField appends a field with key:value to fields. If value is empty, it does nothing.
func appendNotEmptyField(fields []zapcore.Field, key, value string) []zapcore.Field {
	if key == "" || value == "" {
		return fields
	}
	return append(fields, zap.String(key, value))
}

// appendNotEmptyString appends a key=value string to sb. If value is empty, it does nothing.
func appendNotEmptyString(sb *strings.Builder, key, value string) {
	if key == "" || value == "" {
		return
	}
	sb.WriteString(fmt.Sprintf("%s=%v ", key, value))
}

// callerSkipOffset is how many callers to pop off the stack to determine the caller function locality, used for
// adding file/line number to log output.
const callerSkipOffset = 4

func dumpStack(level zapcore.Level, scope *Scope) bool {
	thresh := toLevel[level]
	if scope != defaultScope {
		thresh = ErrorLevel
		switch level {
		case zapcore.FatalLevel:
			thresh = FatalLevel
		}
	}
	return scope.GetStackTraceLevel() >= thresh
}

func emit(scope *Scope, level zapcore.Level, msg string, fields []zapcore.Field) {
	e := zapcore.Entry{
		Message:    msg,
		Level:      level,
		Time:       time.Now(),
		LoggerName: scope.nameToEmit,
	}

	if scope.GetLogCallers() {
		e.Caller = zapcore.NewEntryCaller(runtime.Caller(scope.callerSkip + callerSkipOffset))
	}

	if dumpStack(level, scope) {
		e.Stack = zap.Stack("").String
	}

	pt := funcs.Load().(patchTable)
	if pt.write != nil {
		if err := pt.write(e, fields); err != nil {
			_, _ = fmt.Fprintf(pt.errorSink, "%v log write error: %v\n", time.Now(), err)
			_ = pt.errorSink.Sync()
		}
	}
}

// toErrString returns the string representation of err, handling the nil case.
func toErrString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
