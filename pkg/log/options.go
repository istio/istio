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
)

const (
	// DefaultScopeName defines the name of the default scope.
	DefaultScopeName          = "default"
	defaultOutputLevel        = InfoLevel
	defaultStackTraceLevel    = NoneLevel
	defaultOutputPath         = "stdout"
	defaultErrorOutputPath    = "stderr"
	defaultRotationMaxAge     = 30
	defaultRotationMaxSize    = 100 * 1024 * 1024
	defaultRotationMaxBackups = 1000
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

	// RotateOutputPath is the path to a rotating log file. This file should
	// be automatically rotated over time, based on the rotation parameters such
	// as RotationMaxSize and RotationMaxAge. The default is to not rotate.
	//
	// This path is used as a foundational path. This is where log output is normally
	// saved. When a rotation needs to take place because the file got too big or too
	// old, then the file is renamed by appending a timestamp to the name. Such renamed
	// files are called backups. Once a backup has been created,
	// output resumes to this path.
	RotateOutputPath string

	// RotationMaxSize is the maximum size in megabytes of a log file before it gets
	// rotated. It defaults to 100 megabytes.
	RotationMaxSize int

	// RotationMaxAge is the maximum number of days to retain old log files based on the
	// timestamp encoded in their filename. Note that a day is defined as 24
	// hours and may not exactly correspond to calendar days due to daylight
	// savings, leap seconds, etc. The default is to remove log files
	// older than 30 days.
	RotationMaxAge int

	// RotationMaxBackups is the maximum number of old log files to retain.  The default
	// is to retain at most 1000 logs.
	RotationMaxBackups int

	// JSONEncoding controls whether the log is formatted as JSON.
	JSONEncoding bool

	// LogGrpc indicates that Grpc logs should be captured. The default is true.
	// This is not exposed through the command-line flags, as this flag is mainly useful for testing: Grpc
	// stack will hold on to the logger even though it gets closed. This causes data races.
	LogGrpc bool

	outputLevels     string
	logCallers       string
	stackTraceLevels string

	// If not nil, called instead of os.Exit.
	testonlyExit func()
}

// DefaultOptions returns a new set of options, initialized to the defaults
func DefaultOptions() *Options {
	return &Options{
		OutputPaths:        []string{defaultOutputPath},
		ErrorOutputPaths:   []string{defaultErrorOutputPath},
		RotationMaxSize:    defaultRotationMaxSize,
		RotationMaxAge:     defaultRotationMaxAge,
		RotationMaxBackups: defaultRotationMaxBackups,
		outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
		stackTraceLevels:   DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
		LogGrpc:            true,
	}
}

// SetOutputLevel sets the minimum log output level for a given scope.
func (o *Options) SetOutputLevel(scope string, level Level) {
	sl := scope + ":" + levelToString[level]
	levels := strings.Split(o.outputLevels, ",")

	if scope == DefaultScopeName {
		// see if we have an entry without a scope prefix (which represents the default scope)
		for i, ol := range levels {
			if !strings.Contains(ol, ":") {
				levels[i] = sl
				o.outputLevels = strings.Join(levels, ",")
				return
			}
		}
	}

	prefix := scope + ":"
	for i, ol := range levels {
		if strings.HasPrefix(ol, prefix) {
			levels[i] = sl
			o.outputLevels = strings.Join(levels, ",")
			return
		}
	}

	levels = append(levels, sl)
	o.outputLevels = strings.Join(levels, ",")
}

// GetOutputLevel returns the minimum log output level for a given scope.
func (o *Options) GetOutputLevel(scope string) (Level, error) {
	levels := strings.Split(o.outputLevels, ",")

	if scope == DefaultScopeName {
		// see if we have an entry without a scope prefix (which represents the default scope)
		for _, ol := range levels {
			if !strings.Contains(ol, ":") {
				_, l, err := convertScopedLevel(ol)
				return l, err
			}
		}
	}

	prefix := scope + ":"
	for _, ol := range levels {
		if strings.HasPrefix(ol, prefix) {
			_, l, err := convertScopedLevel(ol)
			return l, err
		}
	}

	return NoneLevel, fmt.Errorf("no level defined for scope '%s'", scope)
}

// SetStackTraceLevel sets the minimum stack tracing level for a given scope.
func (o *Options) SetStackTraceLevel(scope string, level Level) {
	sl := scope + ":" + levelToString[level]
	levels := strings.Split(o.stackTraceLevels, ",")

	if scope == DefaultScopeName {
		// see if we have an entry without a scope prefix (which represents the default scope)
		for i, ol := range levels {
			if !strings.Contains(ol, ":") {
				levels[i] = sl
				o.stackTraceLevels = strings.Join(levels, ",")
				return
			}
		}
	}

	prefix := scope + ":"
	for i, ol := range levels {
		if strings.HasPrefix(ol, prefix) {
			levels[i] = sl
			o.stackTraceLevels = strings.Join(levels, ",")
			return
		}
	}

	levels = append(levels, sl)
	o.stackTraceLevels = strings.Join(levels, ",")
}

// GetStackTraceLevel returns the minimum stack tracing level for a given scope.
func (o *Options) GetStackTraceLevel(scope string) (Level, error) {
	levels := strings.Split(o.stackTraceLevels, ",")

	if scope == DefaultScopeName {
		// see if we have an entry without a scope prefix (which represents the default scope)
		for _, ol := range levels {
			if !strings.Contains(ol, ":") {
				_, l, err := convertScopedLevel(ol)
				return l, err
			}
		}
	}

	prefix := scope + ":"
	for _, ol := range levels {
		if strings.HasPrefix(ol, prefix) {
			_, l, err := convertScopedLevel(ol)
			return l, err
		}
	}

	return NoneLevel, fmt.Errorf("no level defined for scope '%s'", scope)
}

// SetLogCallers sets whether to output the caller's source code location for a given scope.
func (o *Options) SetLogCallers(scope string, include bool) {
	scopes := strings.Split(o.logCallers, ",")

	// remove any occurrence of the scope
	for i, s := range scopes {
		if s == scope {
			scopes[i] = ""
		}
	}

	if include {
		// find a free slot if there is one
		for i, s := range scopes {
			if s == "" {
				scopes[i] = scope
				o.logCallers = strings.Join(scopes, ",")
				return
			}
		}

		scopes = append(scopes, scope)
	}

	o.logCallers = strings.Join(scopes, ",")
}

// GetLogCallers returns whether the caller's source code location is output for a given scope.
func (o *Options) GetLogCallers(scope string) bool {
	scopes := strings.Split(o.logCallers, ",")

	for _, s := range scopes {
		if s == scope {
			return true
		}
	}

	return false
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
		return "", NoneLevel, fmt.Errorf("invalid output level '%s'", sl)
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
	intVar func(p *int, name string, value int, usage string),
	boolVar func(p *bool, name string, value bool, usage string)) {

	stringArrayVar(&o.OutputPaths, "log_target", o.OutputPaths,
		"The set of paths where to output the log. This can be any path as well as the special values stdout and stderr")

	stringVar(&o.RotateOutputPath, "log_rotate", o.RotateOutputPath,
		"The path for the optional rotating log file")

	intVar(&o.RotationMaxAge, "log_rotate_max_age", o.RotationMaxAge,
		"The maximum age in days of a log file beyond which the file is rotated (0 indicates no limit)")

	intVar(&o.RotationMaxSize, "log_rotate_max_size", o.RotationMaxSize,
		"The maximum size in megabytes of a log file beyond which the file is rotated")

	intVar(&o.RotationMaxBackups, "log_rotate_max_backups", o.RotationMaxBackups,
		"The maximum number of log file backups to keep before older files are deleted (0 indicates no limit)")

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
