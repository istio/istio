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
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap/zapcore"
)

const (
	DefaultScopeName       = "default"
	OverrideScopeName      = "all"
	defaultOutputLevel     = InfoLevel
	defaultStackTraceLevel = NoneLevel
	defaultOutputPath      = "stdout"
	defaultErrorOutputPath = "stderr"
)

// Level is an enumeration of all supported log levels.
type Level int

const (
	// NoneLevel disables logging
	NoneLevel Level = iota
	// FatalLevel enables fatal level logging
	FatalLevel
	// ErrorLevel enables error level logging
	ErrorLevel
	// WarnLevel enables warn level logging
	WarnLevel
	// InfoLevel enables info level logging
	InfoLevel
	// DebugLevel enables debug level logging
	DebugLevel
)

var levelToString = map[Level]string{
	DebugLevel: "debug",
	InfoLevel:  "info",
	WarnLevel:  "warn",
	ErrorLevel: "error",
	FatalLevel: "fatal",
	NoneLevel:  "none",
}

var stringToLevel = map[string]Level{
	"debug": DebugLevel,
	"info":  InfoLevel,
	"warn":  WarnLevel,
	"error": ErrorLevel,
	"fatal": FatalLevel,
	"none":  NoneLevel,
}

func StringToLevel(level string) Level {
	return stringToLevel[level]
}

func LevelToString(level Level) string {
	return levelToString[level]
}

// Options defines the set of options supported by Istio's component logging package.
type Options struct {
	// OutputPaths is a list of file system paths to write the log data to.
	// The special values stdout and stderr can be used to output to the
	// standard I/O streams. This defaults to stdout.
	OutputPaths []string

	// ErrorOutputPaths is a list of file system paths to write logger errors to.
	// The special values stdout and stderr can be used to output to the
	// standard I/O streams. This defaults to stderr.
	ErrorOutputPaths []string

	// JSONEncoding controls whether the log is formatted as JSON.
	JSONEncoding bool

	// logGRPC indicates that Grpc logs should be captured.
	// This is enabled by a --log_output_level=grpc:<level> typically
	logGRPC bool

	outputLevels        string
	defaultOutputLevels string
	logCallers          string
	stackTraceLevels    string

	useStackdriverFormat bool
	extensions           []Extension
}

// DefaultOptions returns a new set of options, initialized to the defaults
func DefaultOptions() *Options {
	return &Options{
		OutputPaths:          []string{defaultOutputPath},
		ErrorOutputPaths:     []string{defaultErrorOutputPath},
		defaultOutputLevels:  "default:info,grpc:none",
		stackTraceLevels:     DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
		logGRPC:              false,
		useStackdriverFormat: false,
	}
}

// WithStackdriverLoggingFormat configures logging output to match Stackdriver structured logging conventions.
func (o *Options) WithStackdriverLoggingFormat() *Options {
	o.useStackdriverFormat = true
	return o
}

// WithTeeToUDS configures a parallel logging pipeline that writes logs to a server over UDS.
// addr is the socket that the server listens on, and path is the HTTP path that process the log message.
func (o *Options) WithTeeToUDS(addr, path string) *Options {
	return o.WithExtension(func(c zapcore.Core) (zapcore.Core, func() error, error) {
		return teeToUDSServer(c, addr, path), func() error { return nil }, nil
	})
}

// WithTeeToRolling configures a parallel logging pipeline that writes logs to a local rolling log of fixed size.
// This is mainly used by the CNI plugin, and so the size and rollover is intentionally kept small.
// rollingPath is the path the rolling log(s) will be written to.
func (o *Options) WithTeeToRollingLocal(rollingPath string, maxSizeInMB int) *Options {
	return o.WithExtension(func(c zapcore.Core) (zapcore.Core, func() error, error) {
		return teeToRollingLocal(c, rollingPath, maxSizeInMB), func() error { return nil }, nil
	})
}

// Extension provides an extension mechanism for logs.
// This is essentially like https://pkg.go.dev/golang.org/x/exp/slog#Handler.
// This interface should be considered unstable; we will likely swap it for slog in the future and not expose zap internals.
// Returns a modified Core interface, and a Close() function.
type Extension func(c zapcore.Core) (zapcore.Core, func() error, error)

func (o *Options) WithExtension(e Extension) *Options {
	o.extensions = append(o.extensions, e)
	return o
}

// SetDefaultOutputLevel sets the minimum log output level for a given scope.
// This can be overwritten by flags
func (o *Options) SetDefaultOutputLevel(scope string, level Level) {
	sl := scope + ":" + levelToString[level]
	levels := strings.Split(o.defaultOutputLevels, ",")
	if scope == DefaultScopeName {
		// see if we have an entry without a scope prefix (which represents the default scope)
		for i, ol := range levels {
			if !strings.Contains(ol, ":") {
				levels[i] = sl
				o.defaultOutputLevels = strings.Join(levels, ",")
				return
			}
		}
	}

	prefix := scope + ":"
	for i, ol := range levels {
		if strings.HasPrefix(ol, prefix) {
			levels[i] = sl
			o.defaultOutputLevels = strings.Join(levels, ",")
			return
		}
	}

	levels = append(levels, sl)
	o.defaultOutputLevels = strings.Join(levels, ",")
}

func convertScopedLevel(sl string) (string, Level, error) {
	var s string
	var l string

	pieces := strings.Split(sl, ":")
	if len(pieces) == 1 {
		s = DefaultScopeName
		l = pieces[0]
	} else if len(pieces) == 2 {
		s = pieces[0]
		l = pieces[1]
	} else {
		return "", NoneLevel, fmt.Errorf("invalid output level format '%s'", sl)
	}

	level, ok := stringToLevel[l]
	if !ok {
		return "", NoneLevel, fmt.Errorf("invalid output level '%s'", l)
	}

	return s, level, nil
}

// AttachCobraFlags attaches a set of Cobra flags to the given Cobra command.
//
// Cobra is the command-line processor that Istio uses. This command attaches
// the necessary set of flags to expose a CLI to let the user control all
// logging options.
func (o *Options) AttachCobraFlags(cmd *cobra.Command) {
	o.AttachFlags(
		cmd.PersistentFlags().StringArrayVar,
		cmd.PersistentFlags().StringVar,
		cmd.PersistentFlags().IntVar,
		cmd.PersistentFlags().BoolVar)
}

// AttachFlags allows attaching of flags through a set of lambda functions.
func (o *Options) AttachFlags(
	stringArrayVar func(p *[]string, name string, value []string, usage string),
	stringVar func(p *string, name string, value string, usage string),
	_ func(p *int, name string, value int, usage string),
	boolVar func(p *bool, name string, value bool, usage string),
) {
	stringArrayVar(&o.OutputPaths, "log_target", o.OutputPaths,
		"The set of paths where to output the log. This can be any path as well as the special values stdout and stderr")

	boolVar(&o.JSONEncoding, "log_as_json", o.JSONEncoding,
		"Whether to format output as JSON or in plain console-friendly format")

	levelListString := fmt.Sprintf("[%s, %s, %s, %s, %s, %s]",
		levelToString[DebugLevel],
		levelToString[InfoLevel],
		levelToString[WarnLevel],
		levelToString[ErrorLevel],
		levelToString[FatalLevel],
		levelToString[NoneLevel])

	allScopes := Scopes()
	if len(allScopes) > 1 {
		keys := make([]string, 0, len(allScopes))
		for name := range allScopes {
			keys = append(keys, name)
		}
		keys = append(keys, OverrideScopeName)
		sort.Strings(keys)
		s := strings.Join(keys, ", ")

		stringVar(&o.outputLevels, "log_output_level", o.outputLevels,
			fmt.Sprintf("Comma-separated minimum per-scope logging level of messages to output, in the form of "+
				"<scope>:<level>,<scope>:<level>,... where scope can be one of [%s] and level can be one of %s",
				s, levelListString))

		stringVar(&o.stackTraceLevels, "log_stacktrace_level", o.stackTraceLevels,
			fmt.Sprintf("Comma-separated minimum per-scope logging level at which stack traces are captured, in the form of "+
				"<scope>:<level>,<scope:level>,... where scope can be one of [%s] and level can be one of %s",
				s, levelListString))

		stringVar(&o.logCallers, "log_caller", o.logCallers,
			fmt.Sprintf("Comma-separated list of scopes for which to include caller information, scopes can be any of [%s]", s))
	} else {
		stringVar(&o.outputLevels, "log_output_level", o.outputLevels,
			fmt.Sprintf("The minimum logging level of messages to output,  can be one of %s",
				levelListString))

		stringVar(&o.stackTraceLevels, "log_stacktrace_level", o.stackTraceLevels,
			fmt.Sprintf("The minimum logging level at which stack traces are captured, can be one of %s",
				levelListString))

		stringVar(&o.logCallers, "log_caller", o.logCallers,
			"Comma-separated list of scopes for which to include called information, scopes can be any of [default]")
	}

	// NOTE: we don't currently expose a command-line option to control ErrorOutputPaths since it
	// seems too esoteric.
}
