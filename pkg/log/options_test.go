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
	resetGlobals()

	cases := []struct {
		cmdLine string
		result  Options
	}{
		{"--log_as_json", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			stackTraceLevels:    DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			JSONEncoding:        true,
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_target stdout --log_target stderr", Options{
			OutputPaths:         []string{"stdout", "stderr"},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			stackTraceLevels:    DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_caller default", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			stackTraceLevels:    DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			logCallers:          DefaultScopeName,
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_stacktrace_level debug", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			stackTraceLevels:    levelToString[DebugLevel],
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_stacktrace_level default:debug", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			stackTraceLevels:    DefaultScopeName + ":" + levelToString[DebugLevel],
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_stacktrace_level info", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			stackTraceLevels:    levelToString[InfoLevel],
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_stacktrace_level default:info", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			stackTraceLevels:    DefaultScopeName + ":" + levelToString[InfoLevel],
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_stacktrace_level warn", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			stackTraceLevels:    levelToString[WarnLevel],
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_stacktrace_level default:warn", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			stackTraceLevels:    DefaultScopeName + ":" + levelToString[WarnLevel],
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_stacktrace_level error", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			stackTraceLevels:    levelToString[ErrorLevel],
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_stacktrace_level default:error", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			stackTraceLevels:    DefaultScopeName + ":" + levelToString[ErrorLevel],
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_stacktrace_level none", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			stackTraceLevels:    levelToString[NoneLevel],
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_stacktrace_level default:none", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			stackTraceLevels:    "default:none",
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_output_level debug", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			outputLevels:        levelToString[DebugLevel],
			stackTraceLevels:    DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_output_level info", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			outputLevels:        levelToString[InfoLevel],
			stackTraceLevels:    DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_output_level warn", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			outputLevels:        levelToString[WarnLevel],
			stackTraceLevels:    DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_output_level error", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			outputLevels:        levelToString[ErrorLevel],
			stackTraceLevels:    DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_output_level none", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			outputLevels:        levelToString[NoneLevel],
			stackTraceLevels:    DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_rotate foobar", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			stackTraceLevels:    DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotateOutputPath:    "foobar",
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_rotate_max_age 1234", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			stackTraceLevels:    DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:      1234,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_rotate_max_size 1234", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			stackTraceLevels:    DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     1234,
			RotationMaxBackups:  defaultRotationMaxBackups,
		}},

		{"--log_rotate_max_backups 1234", Options{
			OutputPaths:         []string{defaultOutputPath},
			ErrorOutputPaths:    []string{defaultErrorOutputPath},
			defaultOutputLevels: "default:info,grpc:none",
			stackTraceLevels:    DefaultScopeName + ":" + levelToString[defaultStackTraceLevel],
			RotationMaxAge:      defaultRotationMaxAge,
			RotationMaxSize:     defaultRotationMaxSize,
			RotationMaxBackups:  1234,
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

		_ = RegisterScope("foo", "bar")
	}
}

func TestSetDefaultLevel(t *testing.T) {
	resetGlobals()

	_ = RegisterScope("TestSetLevel", "")

	cases := []struct {
		flagLevels    string
		scope         string
		defaultLevel  Level
		expectedLevel Level
	}{
		{"debug", "default", DebugLevel, DebugLevel},
		{"default:debug", "default", DebugLevel, DebugLevel},
		{"info", "default", DebugLevel, InfoLevel},
		{"default:info", "default", DebugLevel, InfoLevel},
		{"warn", "default", DebugLevel, WarnLevel},
		{"default:warn", "default", DebugLevel, WarnLevel},
		{"error", "default", DebugLevel, ErrorLevel},
		{"default:error", "default", DebugLevel, ErrorLevel},
		{"none", "default", DebugLevel, NoneLevel},
		{"default:none", "default", DebugLevel, NoneLevel},

		{"debug", "default", ErrorLevel, DebugLevel},
		{"default:debug", "default", ErrorLevel, DebugLevel},
		{"info", "default", ErrorLevel, InfoLevel},
		{"default:info", "default", ErrorLevel, InfoLevel},
		{"warn", "default", ErrorLevel, WarnLevel},
		{"default:warn", "default", ErrorLevel, WarnLevel},
		{"error", "default", ErrorLevel, ErrorLevel},
		{"default:error", "default", ErrorLevel, ErrorLevel},
		{"none", "default", ErrorLevel, NoneLevel},
		{"default:none", "default", ErrorLevel, NoneLevel},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			o := DefaultOptions()
			o.outputLevels = c.flagLevels
			o.stackTraceLevels = c.flagLevels

			o.SetDefaultOutputLevel(c.scope, c.defaultLevel)
			if err := Configure(o); err != nil {
				t.Fatal(err)
			}
			if got := FindScope(c.scope).GetOutputLevel(); got != c.expectedLevel {
				t.Fatalf("got %v want %v", got, c.expectedLevel)
			}
		})
	}
}
