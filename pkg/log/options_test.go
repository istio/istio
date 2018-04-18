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
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestOpts(t *testing.T) {
	cases := []struct {
		cmdLine string
		result  Options
	}{
		{"--log_as_json", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			JSONEncoding:       true,
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_target stdout --log_target stderr", Options{
			OutputPaths:        []string{"stdout", "stderr"},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_caller default", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			logCallers:         DefaultScopeName,
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_stacktrace_level debug", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   levelToString[DebugLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_stacktrace_level default:debug", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[DebugLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_stacktrace_level info", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   levelToString[InfoLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_stacktrace_level default:info", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[InfoLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_stacktrace_level warn", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   levelToString[WarnLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_stacktrace_level default:warn", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[WarnLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_stacktrace_level error", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   levelToString[ErrorLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_stacktrace_level default:error", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[ErrorLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_stacktrace_level none", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   levelToString[NoneLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_stacktrace_level default:none", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[NoneLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_output_level debug", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       levelToString[DebugLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_output_level info", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       levelToString[InfoLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_output_level warn", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       levelToString[WarnLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_output_level error", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       levelToString[ErrorLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_output_level none", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       levelToString[NoneLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_rotate foobar", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotateOutputPath:   "foobar",
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_rotate_max_age 1234", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:     1234,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_rotate_max_size 1234", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    1234,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_rotate_max_backups 1234", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: 1234,
			LogGrpc:            true,
		}},

		// legacy, has no effect
		{"--v 2", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		// legacy, has no effect
		{"-v 2", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		// legacy, has no effect
		{"--stderrthreshold 2", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		// legacy, has no effect
		{"--logtostderr", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		// legacy, has no effect
		{"--alsologtostderr", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevels:       DefaultScopeName + ":" + levelToString[defaultOutputLevel],
			stackTraceLevels:   DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},
	}

	for j := 0; j < 2; j++ {
		for i, c := range cases {
			t.Run(strconv.Itoa(j*100+i), func(t *testing.T) {
				o := DefaultOptions()
				cmd := &cobra.Command{}
				o.AttachCobraFlags(cmd)
				cmd.SetArgs(strings.Split(c.cmdLine, " "))

				if err := cmd.Execute(); err != nil {
					t.Errorf("Got %v, expecting success", err)
				}

				if !reflect.DeepEqual(c.result, *o) {
					t.Errorf("Got %v, expected %v", *o, c.result)
				}
			})
		}

		_ = RegisterScope("foo", "bar", 0)
	}
}

func TestSetLevel(t *testing.T) {
	_ = RegisterScope("TestSetLevel", "", 0)

	cases := []struct {
		levels      string
		scope       string
		targetLevel Level
	}{
		{"debug", "default", DebugLevel},
		{"default:debug", "default", DebugLevel},
		{"info", "default", DebugLevel},
		{"default:info", "default", DebugLevel},
		{"warn", "default", DebugLevel},
		{"default:warn", "default", DebugLevel},
		{"error", "default", DebugLevel},
		{"default:error", "default", DebugLevel},
		{"none", "default", DebugLevel},
		{"default:none", "default", DebugLevel},

		{"debug", "default", ErrorLevel},
		{"default:debug", "default", ErrorLevel},
		{"info", "default", ErrorLevel},
		{"default:info", "default", ErrorLevel},
		{"warn", "default", ErrorLevel},
		{"default:warn", "default", ErrorLevel},
		{"error", "default", ErrorLevel},
		{"default:error", "default", ErrorLevel},
		{"none", "default", ErrorLevel},
		{"default:none", "default", ErrorLevel},

		{"default:none", "pizza", ErrorLevel},
		{"default:none,TestSetLevel:debug", "pizza", ErrorLevel},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			o := DefaultOptions()
			o.outputLevels = c.levels
			o.stackTraceLevels = c.levels

			o.SetOutputLevel(c.scope, c.targetLevel)
			o.SetStackTraceLevel(c.scope, c.targetLevel)

			if newLevel, err := o.GetOutputLevel(c.scope); err != nil {
				t.Errorf("Got error %v, expecting success", err)
			} else if newLevel != c.targetLevel {
				t.Errorf("Got level %v, expecting %v", newLevel, c.targetLevel)
			}

			if newLevel, err := o.GetStackTraceLevel(c.scope); err != nil {
				t.Errorf("Got error %v, expecting success", err)
			} else if newLevel != c.targetLevel {
				t.Errorf("Got level %v, expecting %v", newLevel, c.targetLevel)
			}
		})
	}
}

func TestGetLevel(t *testing.T) {
	cases := []struct {
		levels        string
		scope         string
		expectedLevel Level
		expectedFail  bool
	}{
		{"debug", "default", DebugLevel, false},
		{"default:debug", "default", DebugLevel, false},
		{"info", "default", InfoLevel, false},
		{"default:info", "default", InfoLevel, false},
		{"warn", "default", WarnLevel, false},
		{"default:warn", "default", WarnLevel, false},
		{"error", "default", ErrorLevel, false},
		{"default:error", "default", ErrorLevel, false},

		{"badLevel", "default", NoneLevel, true},
		{"default:badLevel", "default", NoneLevel, true},

		{"error", "badScope", NoneLevel, true},
		{"default:error", "badScope", NoneLevel, true},

		{"default:err:or", "default", NoneLevel, true},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			o := DefaultOptions()
			o.outputLevels = c.levels
			o.stackTraceLevels = c.levels

			l, err := o.GetOutputLevel(c.scope)
			if c.expectedFail {
				if err == nil {
					t.Errorf("Got success, expecting error")
				}
			} else {
				if err != nil {
					t.Errorf("Got error %s, expecting success", err)
				}

				if l != c.expectedLevel {
					t.Errorf("Got level %v, expecting %v", l, c.expectedLevel)
				}
			}

			l, err = o.GetStackTraceLevel(c.scope)
			if c.expectedFail {
				if err == nil {
					t.Errorf("Got success, expecting error")
				}
			} else {
				if err != nil {
					t.Errorf("Got error %s, expecting success", err)
				}

				if l != c.expectedLevel {
					t.Errorf("Got level %v, expecting %v", l, c.expectedLevel)
				}
			}
		})
	}
}

func TestLogCallers(t *testing.T) {
	o := DefaultOptions()

	o.SetLogCallers("s1", true)
	if !o.GetLogCallers("s1") {
		t.Error("Expecting true")
	}

	o.SetLogCallers("s1", false)
	if o.GetLogCallers("s1") {
		t.Error("Expecting false")
	}

	o.SetLogCallers("s1", true)
	o.SetLogCallers("s2", true)
	if !o.GetLogCallers("s1") {
		t.Error("Expecting true")
	}

	if !o.GetLogCallers("s2") {
		t.Error("Expecting true")
	}

	if o.GetLogCallers("s3") {
		t.Error("Expecting false")
	}
}
