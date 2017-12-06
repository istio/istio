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
	"go.uber.org/zap/zapcore"
)

func TestOpts(t *testing.T) {
	cases := []struct {
		cmdLine string
		result  Options
	}{
		{"--log_as_json", Options{
			OutputPaths:                 []string{"stdout"},
			outputLevel:                 "info",
			stackTraceLevel:             "none",
			IncludeCallerSourceLocation: false,
			JSONEncoding:                true,
		}},

		{"--log_target stdout --log_target stderr", Options{
			OutputPaths:                 []string{"stdout", "stderr"},
			outputLevel:                 "info",
			stackTraceLevel:             "none",
			IncludeCallerSourceLocation: false,
			JSONEncoding:                false,
		}},

		{"--log_callers", Options{
			OutputPaths:                 []string{"stdout"},
			outputLevel:                 "info",
			stackTraceLevel:             "none",
			IncludeCallerSourceLocation: true,
			JSONEncoding:                false,
		}},

		{"--log_stacktrace_level debug", Options{
			OutputPaths:                 []string{"stdout"},
			outputLevel:                 "info",
			stackTraceLevel:             "debug",
			IncludeCallerSourceLocation: false,
			JSONEncoding:                false,
		}},

		{"--log_stacktrace_level info", Options{
			OutputPaths:                 []string{"stdout"},
			outputLevel:                 "info",
			stackTraceLevel:             "info",
			IncludeCallerSourceLocation: false,
			JSONEncoding:                false,
		}},

		{"--log_stacktrace_level warn", Options{
			OutputPaths:                 []string{"stdout"},
			outputLevel:                 "info",
			stackTraceLevel:             "warn",
			IncludeCallerSourceLocation: false,
			JSONEncoding:                false,
		}},

		{"--log_stacktrace_level error", Options{
			OutputPaths:                 []string{"stdout"},
			outputLevel:                 "info",
			stackTraceLevel:             "error",
			IncludeCallerSourceLocation: false,
			JSONEncoding:                false,
		}},

		{"--log_stacktrace_level none", Options{
			OutputPaths:                 []string{"stdout"},
			outputLevel:                 "info",
			stackTraceLevel:             "none",
			IncludeCallerSourceLocation: false,
			JSONEncoding:                false,
		}},

		{"--log_output_level debug", Options{
			OutputPaths:                 []string{"stdout"},
			outputLevel:                 "debug",
			stackTraceLevel:             "none",
			IncludeCallerSourceLocation: false,
			JSONEncoding:                false,
		}},

		{"--log_output_level info", Options{
			OutputPaths:                 []string{"stdout"},
			outputLevel:                 "info",
			stackTraceLevel:             "none",
			IncludeCallerSourceLocation: false,
			JSONEncoding:                false,
		}},

		{"--log_output_level warn", Options{
			OutputPaths:                 []string{"stdout"},
			outputLevel:                 "warn",
			stackTraceLevel:             "none",
			IncludeCallerSourceLocation: false,
			JSONEncoding:                false,
		}},

		{"--log_output_level error", Options{
			OutputPaths:                 []string{"stdout"},
			outputLevel:                 "error",
			stackTraceLevel:             "none",
			IncludeCallerSourceLocation: false,
			JSONEncoding:                false,
		}},

		{"--log_output_level none", Options{
			OutputPaths:                 []string{"stdout"},
			outputLevel:                 "none",
			stackTraceLevel:             "none",
			IncludeCallerSourceLocation: false,
			JSONEncoding:                false,
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
		inputLevel  zapcore.Level
		outputLevel zapcore.Level
		fail        bool
	}{
		{zapcore.DebugLevel, zapcore.DebugLevel, false},
		{zapcore.InfoLevel, zapcore.InfoLevel, false},
		{zapcore.WarnLevel, zapcore.WarnLevel, false},
		{zapcore.ErrorLevel, zapcore.ErrorLevel, false},
		{None, None, false},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			o := NewOptions()

			err := o.SetOutputLevel(c.inputLevel)
			if c.fail && err == nil {
				t.Errorf("Got success, expecting failure")
			} else if !c.fail && err != nil {
				t.Errorf("Got failure '%v', expecting success", err)
			}

			var l zapcore.Level
			l, err = o.GetOutputLevel()
			if err != nil {
				t.Errorf("Got failure %v, expecting success", err)
			}

			if c.outputLevel != l {
				t.Errorf("Got %v, expecting %v", l, c.outputLevel)
			}

			err = o.SetStackTraceLevel(c.inputLevel)
			if c.fail && err == nil {
				t.Errorf("Got success, expecting failure")
			} else if !c.fail && err != nil {
				t.Errorf("Got failure '%v', expecting success", err)
			}

			l, err = o.GetStackTraceLevel()
			if err != nil {
				t.Errorf("Got failure %v, expecting success", err)
			}

			if c.outputLevel != l {
				t.Errorf("Got %v, expecting %v", l, c.outputLevel)
			}
		})
	}

	o := NewOptions()
	o.outputLevel = "foobar"
	_, err := o.GetOutputLevel()
	if err == nil {
		t.Errorf("Got nil, expecting error")
	}

	if err = o.SetOutputLevel(127); err == nil {
		t.Errorf("Got success, expecting error")
	}

	o = NewOptions()
	o.stackTraceLevel = "foobar"
	_, err = o.GetStackTraceLevel()
	if err == nil {
		t.Errorf("Got nil, expecting error")
	}

	if err = o.SetStackTraceLevel(127); err == nil {
		t.Errorf("Got success, expecting error")
	}
}
