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
	"go.uber.org/zap/zapcore"
)

var toLevel = map[zapcore.Level]Level{
	zapcore.FatalLevel: FatalLevel,
	zapcore.ErrorLevel: ErrorLevel,
	zapcore.WarnLevel:  WarnLevel,
	zapcore.InfoLevel:  InfoLevel,
	zapcore.DebugLevel: DebugLevel,
}

// callerSkipOffset is how many callers to pop off the stack to determine the caller function locality, used for
// adding file/line number to log output.
const callerSkipOffset = 2

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
