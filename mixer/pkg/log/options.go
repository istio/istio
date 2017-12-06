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

	"github.com/spf13/cobra"
	"go.uber.org/zap/zapcore"
)

const (
	// None is used to disable logging output as well as to disable stack tracing.
	None zapcore.Level = 100
)

// Options defines the set of options supported by Istio's component logging package.
type Options struct {
	// OutputPaths is a list of file system paths to write the log data to.
	// The special values stdout and stderr can be used to output to the
	// standard I/O streams.
	OutputPaths []string

	// JSONEncoding controls whether the log is formatted as JSON.
	JSONEncoding bool

	// IncludeCallerSourceLocation determines whether log messages include the source location of the caller.
	IncludeCallerSourceLocation bool

	stackTraceLevel string
	outputLevel     string
}

var levelToString = map[zapcore.Level]string{
	zapcore.DebugLevel: "debug",
	zapcore.InfoLevel:  "info",
	zapcore.WarnLevel:  "warn",
	zapcore.ErrorLevel: "error",
	None:               "none",
}

var stringToLevel = map[string]zapcore.Level{
	"debug": zapcore.DebugLevel,
	"info":  zapcore.InfoLevel,
	"warn":  zapcore.WarnLevel,
	"error": zapcore.ErrorLevel,
	"none":  None,
}

// NewOptions returns a new set of options, initialized to the defaults
func NewOptions() *Options {
	return &Options{
		OutputPaths:     []string{"stdout"},
		outputLevel:     "info",
		stackTraceLevel: "none",
	}
}

// SetOutputLevel sets the minimum log output level.
//
// The level can be one of zapcore.DebugLevel, zapcore.InfoLevel,
// zapcore.WarnLevel, zapcore.ErrorLevel, or None. The default is
// zapcore.InfoLevel.
func (o *Options) SetOutputLevel(level zapcore.Level) error {
	s, ok := levelToString[level]
	if !ok {
		return fmt.Errorf("unknown output level: %v", level)
	}

	o.outputLevel = s
	return nil
}

// GetOutputLevel returns the minimum log output level.
func (o *Options) GetOutputLevel() (zapcore.Level, error) {
	l, ok := stringToLevel[o.outputLevel]
	if !ok {
		return 0, fmt.Errorf("unknown output level: %s", o.outputLevel)
	}

	return l, nil
}

// SetStackTraceLevel sets the minimum stack trace capture level.
//
// The level can be one of zapcore.DebugLevel, zapcore.InfoLevel,
// zapcore.WarnLevel, zapcore.ErrorLevel, or None. The default is
// None.
func (o *Options) SetStackTraceLevel(level zapcore.Level) error {
	s, ok := levelToString[level]
	if !ok {
		return fmt.Errorf("unknown stack trace level: %v", level)
	}

	o.stackTraceLevel = s
	return nil
}

// GetStackTraceLevel returns the current stack trace level.
func (o *Options) GetStackTraceLevel() (zapcore.Level, error) {
	l, ok := stringToLevel[o.stackTraceLevel]
	if !ok {
		return 0, fmt.Errorf("unknown stack trace level: %s", o.stackTraceLevel)
	}

	return l, nil
}

// AttachCobraFlags attaches a set of Cobra flags to the given Cobra command.
//
// Cobra is the command-line processor that Istio uses. This command attaches
// the necessary set of flags to expose a CLI to let the user control all
// logging options.
func (o *Options) AttachCobraFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringArrayVar(&o.OutputPaths, "log_target", o.OutputPaths,
		"The set of paths where to output the log. This can be any path as well as the special values stdout and stderr")

	cmd.PersistentFlags().BoolVar(&o.JSONEncoding, "log_as_json", o.JSONEncoding,
		"Whether to format output as JSON or in plain console-friendly format")

	cmd.PersistentFlags().StringVar(&o.outputLevel, "log_output_level", o.outputLevel,
		"The minimum logging level of messages to output, can be one of debug, info, warning, error, or none")

	cmd.PersistentFlags().BoolVar(&o.IncludeCallerSourceLocation, "log_callers", o.IncludeCallerSourceLocation,
		"Include caller information, useful for debugging")

	cmd.PersistentFlags().StringVar(&o.stackTraceLevel, "log_stacktrace_level", o.stackTraceLevel,
		"The minimum logging level at which stack traces are captured, can be one of debug, info, warning, error, or none")
}
