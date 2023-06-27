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
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/logging"
	"go.uber.org/zap/zapcore"
	"google.golang.org/api/option"
	"google.golang.org/genproto/googleapis/api/monitoredres"
)

// stackdriverSeverityMapping maps the Zap log levels to the correct level names as
// defined by Stackdriver.
//
// The Stackdriver logging levels are documented as:
//
// DEFAULT     (0) The log entry has no assigned severity level.
// DEBUG     (100) Debug or trace information.
// INFO      (200) Routine information, such as ongoing status or performance.
// NOTICE    (300) Normal but significant events, such as start up, shut down, or a configuration change.
// WARNING   (400) Warning events might cause problems.
// ERROR     (500) Error events are likely to cause problems.
// CRITICAL  (600) Critical events cause more severe problems or outages.
// ALERT     (700) A person must take an action immediately.
// EMERGENCY (800) One or more systems are unusable.
//
// See: https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry#LogSeverity
var stackdriverSeverityMapping = map[zapcore.Level]logging.Severity{
	zapcore.DebugLevel:  logging.Debug,
	zapcore.InfoLevel:   logging.Info,
	zapcore.WarnLevel:   logging.Warning,
	zapcore.ErrorLevel:  logging.Error,
	zapcore.DPanicLevel: logging.Critical,
	zapcore.FatalLevel:  logging.Critical,
	zapcore.PanicLevel:  logging.Critical,
}

func encodeStackdriverLevel(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(stackdriverSeverityMapping[l].String())
}

// A stackdriverCore writes entries to a Google Cloud Logging API.
type stackdriverCore struct {
	logger       *logging.Logger
	minimumLevel zapcore.Level
	fields       map[string]any
}

type CloseFunc func() error

// teeToStackdriver returns a zapcore.Core that writes entries to both the provided core and to Stackdriver.
func teeToStackdriver(baseCore zapcore.Core, project, quotaProject, logName string, mr *monitoredres.MonitoredResource) (zapcore.Core, CloseFunc, error) {
	if project == "" {
		return nil, func() error { return nil }, errors.New("a project must be provided for stackdriver export")
	}

	client, err := logging.NewClient(context.Background(), project, option.WithQuotaProject(quotaProject))
	if err != nil {
		return nil, func() error { return nil }, err
	}

	var logger *logging.Logger
	if mr != nil {
		logger = client.Logger(logName, logging.CommonResource(mr))
	} else {
		logger = client.Logger(logName)
	}
	sdCore := &stackdriverCore{logger: logger}

	for l := zapcore.DebugLevel; l <= zapcore.FatalLevel; l++ {
		if baseCore.Enabled(l) {
			sdCore.minimumLevel = l
			break
		}
	}

	return zapcore.NewTee(baseCore, sdCore), func() error { return client.Close() }, nil
}

// Enabled implements zapcore.Core.
func (sc *stackdriverCore) Enabled(l zapcore.Level) bool {
	return l >= sc.minimumLevel
}

// With implements zapcore.Core.
func (sc *stackdriverCore) With(fields []zapcore.Field) zapcore.Core {
	return &stackdriverCore{
		logger:       sc.logger,
		minimumLevel: sc.minimumLevel,
		fields:       clone(sc.fields, fields),
	}
}

// Check implements zapcore.Core.
func (sc *stackdriverCore) Check(e zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if sc.Enabled(e.Level) {
		return ce.AddCore(e, sc)
	}
	return ce
}

// Sync implements zapcore.Core.
func (sc *stackdriverCore) Sync() error {
	if err := sc.logger.Flush(); err != nil {
		return fmt.Errorf("error writing logs to Stackdriver: %v", err)
	}
	return nil
}

// Write implements zapcore.Core. It writes a log entry to Stackdriver.
func (sc *stackdriverCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	severity, specified := stackdriverSeverityMapping[entry.Level]
	if !specified {
		severity = logging.Default
	}

	payload := clone(sc.fields, fields)

	payload["logger"] = entry.LoggerName
	payload["message"] = entry.Message

	sc.logger.Log(logging.Entry{
		Timestamp: entry.Time,
		Severity:  severity,
		Payload:   payload,
	})

	return nil
}

// clone creates a new field map without mutating the original.
func clone(orig map[string]any, newFields []zapcore.Field) map[string]any {
	clone := make(map[string]any)

	for k, v := range orig {
		clone[k] = v
	}

	for _, f := range newFields {
		switch f.Type {
		case zapcore.ArrayMarshalerType:
			clone[f.Key] = f.Interface
		case zapcore.ObjectMarshalerType:
			clone[f.Key] = f.Interface
		case zapcore.BinaryType:
			clone[f.Key] = f.Interface
		case zapcore.BoolType:
			clone[f.Key] = f.Integer == 1
		case zapcore.ByteStringType:
			clone[f.Key] = f.String
		case zapcore.Complex128Type:
			clone[f.Key] = fmt.Sprint(f.Interface)
		case zapcore.Complex64Type:
			clone[f.Key] = fmt.Sprint(f.Interface)
		case zapcore.DurationType:
			clone[f.Key] = time.Duration(f.Integer).String()
		case zapcore.Float64Type:
			clone[f.Key] = float64(f.Integer)
		case zapcore.Float32Type:
			clone[f.Key] = float32(f.Integer)
		case zapcore.Int64Type:
			clone[f.Key] = f.Integer
		case zapcore.Int32Type:
			clone[f.Key] = int32(f.Integer)
		case zapcore.Int16Type:
			clone[f.Key] = int16(f.Integer)
		case zapcore.Int8Type:
			clone[f.Key] = int8(f.Integer)
		case zapcore.StringType:
			clone[f.Key] = f.String
		case zapcore.TimeType:
			clone[f.Key] = f.Interface.(time.Time)
		case zapcore.Uint64Type:
			clone[f.Key] = uint64(f.Integer)
		case zapcore.Uint32Type:
			clone[f.Key] = uint32(f.Integer)
		case zapcore.Uint16Type:
			clone[f.Key] = uint16(f.Integer)
		case zapcore.Uint8Type:
			clone[f.Key] = uint8(f.Integer)
		case zapcore.UintptrType:
			clone[f.Key] = uintptr(f.Integer)
		case zapcore.ReflectType:
			clone[f.Key] = f.Interface
		case zapcore.StringerType:
			clone[f.Key] = f.Interface.(fmt.Stringer).String()
		case zapcore.ErrorType:
			clone[f.Key] = f.Interface.(error).Error()
		case zapcore.SkipType:
			continue
		default:
			clone[f.Key] = f.Interface
		}
	}

	return clone
}
