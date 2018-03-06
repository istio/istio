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
			outputLevel:        string(defaultOutputLevel),
			stackTraceLevel:    string(defaultStackTraceLevel),
			JSONEncoding:       true,
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_target stdout --log_target stderr", Options{
			OutputPaths:        []string{"stdout", "stderr"},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevel:        string(defaultOutputLevel),
			stackTraceLevel:    string(defaultStackTraceLevel),
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_callers", Options{
			OutputPaths:                 []string{defaultOutputPath},
			ErrorOutputPaths:            []string{defaultErrorOutputPath},
			outputLevel:                 string(defaultOutputLevel),
			stackTraceLevel:             string(defaultStackTraceLevel),
			IncludeCallerSourceLocation: true,
			RotationMaxAge:              defaultRotationMaxAge,
			RotationMaxSize:             defaultRotationMaxSize,
			RotationMaxBackups:          defaultRotationMaxBackups,
			LogGrpc:                     true,
		}},

		{"--log_stacktrace_level debug", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevel:        string(defaultOutputLevel),
			stackTraceLevel:    string(DebugLevel),
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_stacktrace_level info", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevel:        string(defaultOutputLevel),
			stackTraceLevel:    string(InfoLevel),
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_stacktrace_level warn", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevel:        string(defaultOutputLevel),
			stackTraceLevel:    string(WarnLevel),
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_stacktrace_level error", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevel:        string(defaultOutputLevel),
			stackTraceLevel:    string(ErrorLevel),
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_stacktrace_level none", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevel:        string(defaultOutputLevel),
			stackTraceLevel:    string(NoneLevel),
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_output_level debug", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevel:        string(DebugLevel),
			stackTraceLevel:    string(defaultStackTraceLevel),
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_output_level info", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevel:        string(InfoLevel),
			stackTraceLevel:    string(defaultStackTraceLevel),
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_output_level warn", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevel:        string(WarnLevel),
			stackTraceLevel:    string(defaultStackTraceLevel),
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_output_level error", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevel:        string(ErrorLevel),
			stackTraceLevel:    string(defaultStackTraceLevel),
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_output_level none", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevel:        string(NoneLevel),
			stackTraceLevel:    string(defaultStackTraceLevel),
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_rotate foobar", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevel:        string(defaultOutputLevel),
			stackTraceLevel:    string(defaultStackTraceLevel),
			RotateOutputPath:   "foobar",
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_rotate_max_age 1234", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevel:        string(defaultOutputLevel),
			stackTraceLevel:    string(defaultStackTraceLevel),
			RotationMaxAge:     1234,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_rotate_max_size 1234", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevel:        string(defaultOutputLevel),
			stackTraceLevel:    string(defaultStackTraceLevel),
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    1234,
			RotationMaxBackups: defaultRotationMaxBackups,
			LogGrpc:            true,
		}},

		{"--log_rotate_max_backups 1234", Options{
			OutputPaths:        []string{defaultOutputPath},
			ErrorOutputPaths:   []string{defaultErrorOutputPath},
			outputLevel:        string(defaultOutputLevel),
			stackTraceLevel:    string(defaultStackTraceLevel),
			RotationMaxAge:     defaultRotationMaxAge,
			RotationMaxSize:    defaultRotationMaxSize,
			RotationMaxBackups: 1234,
			LogGrpc:            true,
		}},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			o := NewOptions()
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
}

func TestLevel(t *testing.T) {
	cases := []struct {
		outputLevel     Level
		stackTraceLevel Level
		fail            bool
	}{
		{DebugLevel, InfoLevel, false},
		{InfoLevel, WarnLevel, false},
		{WarnLevel, ErrorLevel, false},
		{ErrorLevel, NoneLevel, false},
		{NoneLevel, DebugLevel, false},
		{"bad", "bad", true},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			o := NewOptions()

			oldLevel, _ := o.GetOutputLevel()
			err := o.SetOutputLevel(c.outputLevel)
			newLevel, _ := o.GetOutputLevel()

			checkSetLevel(t, oldLevel, c.outputLevel, newLevel, err, c.fail)

			oldLevel, _ = o.GetStackTraceLevel()
			err = o.SetStackTraceLevel(c.stackTraceLevel)
			newLevel, _ = o.GetStackTraceLevel()

			checkSetLevel(t, oldLevel, c.stackTraceLevel, newLevel, err, c.fail)
		})
	}

	// Now test setting the underlying field directly. This simulates what would
	// happen if an invalid CLI flag was provided.
	o := NewOptions()
	o.outputLevel = "foobar"
	_, err := o.GetOutputLevel()
	if err == nil {
		t.Errorf("Got nil, expecting error")
	}

	o = NewOptions()
	o.stackTraceLevel = "foobar"
	_, err = o.GetStackTraceLevel()
	if err == nil {
		t.Errorf("Got nil, expecting error")
	}
}

func checkSetLevel(t *testing.T, oldLevel Level, requestedLevel Level, newLevel Level, err error, expectError bool) {
	if expectError { // Expecting Error
		if err == nil {
			t.Errorf("Got success, expecting failure")
		}

		if newLevel != oldLevel {
			t.Errorf("Got %v, expecting %v", newLevel, oldLevel)
		}
	} else { // Expecting success
		if err != nil {
			t.Errorf("Got failure '%v', expecting success", err)
		}

		if newLevel != requestedLevel {
			t.Errorf("Got %v, expecting %v", newLevel, requestedLevel)
		}
	}
}
