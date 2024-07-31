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
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc/grpclog"
)

const timePattern = "[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z"

type testDateEncoder struct {
	zapcore.PrimitiveArrayEncoder
	output string
}

func (tde *testDateEncoder) AppendString(s string) {
	tde.output = s
}

func TestTimestampProperYear(t *testing.T) {
	testEnc := &testDateEncoder{}
	cases := []struct {
		name  string
		input time.Time
		want  string
	}{
		{"1", time.Date(1, time.April, 1, 1, 1, 1, 1, time.UTC), "0001"},
		{"1989", time.Date(1989, time.February, 1, 1, 1, 1, 1, time.UTC), "1989"},
		{"2017", time.Date(2017, time.January, 1, 1, 1, 1, 1, time.UTC), "2017"},
		{"2083", time.Date(2083, time.March, 1, 1, 1, 1, 1, time.UTC), "2083"},
		{"2573", time.Date(2573, time.June, 1, 1, 1, 1, 1, time.UTC), "2573"},
		{"9999", time.Date(9999, time.May, 1, 1, 1, 1, 1, time.UTC), "9999"},
	}

	for _, v := range cases {
		t.Run(v.name, func(t *testing.T) {
			formatDate(v.input, testEnc)
			if !strings.HasPrefix(testEnc.output, v.want) {
				t.Errorf("formatDate(%v) => %s, want year: %s", v.input, testEnc.output, v.want)
			}
		})
	}
}

func TestTimestampProperMicros(t *testing.T) {
	testEnc := &testDateEncoder{}
	cases := []struct {
		name  string
		input time.Time
		want  string
	}{
		{"1", time.Date(2017, time.April, 1, 1, 1, 1, 1000, time.UTC), "1"},
		{"99", time.Date(1989, time.February, 1, 1, 1, 1, 99000, time.UTC), "99"},
		{"999", time.Date(2017, time.January, 1, 1, 1, 1, 999000, time.UTC), "999"},
		{"9999", time.Date(2083, time.March, 1, 1, 1, 1, 9999000, time.UTC), "9999"},
		{"99999", time.Date(2083, time.March, 1, 1, 1, 1, 99999000, time.UTC), "99999"},
		{"999999", time.Date(2083, time.March, 1, 1, 1, 1, 999999000, time.UTC), "999999"},
	}

	for _, v := range cases {
		t.Run(v.name, func(t *testing.T) {
			formatDate(v.input, testEnc)
			if !strings.HasSuffix(testEnc.output, v.want+"Z") {
				t.Errorf("formatDate(%v) => %s, want micros: %s", v.input, testEnc.output, v.want)
			}
		})
	}
}

func TestOverrides(t *testing.T) {
	resetGlobals()
	s := RegisterScope("TestOverrides", "For testing")

	o := DefaultOptions()
	o.outputLevels = "default:debug,all:info"
	if err := Configure(o); err != nil {
		t.Errorf("Expecting success, got %v", err)
	} else if s.GetOutputLevel() != InfoLevel {
		t.Errorf("Expecting InfoLevel, got %v", s.GetOutputLevel())
	}

	o = DefaultOptions()
	o.stackTraceLevels = "default:debug,all:info"
	if err := Configure(o); err != nil {
		t.Errorf("Expecting success, got %v", err)
	} else if s.GetStackTraceLevel() != InfoLevel {
		t.Errorf("Expecting InfoLevel, got %v", s.GetStackTraceLevel())
	}

	o = DefaultOptions()
	o.logCallers = "all"
	if err := Configure(o); err != nil {
		t.Errorf("Expecting success, got %v", err)
	} else if !s.GetLogCallers() {
		t.Error("Expecting true, got false")
	}
}

func TestGrpc(t *testing.T) {
	resetGlobals()

	o := DefaultOptions()
	o.outputLevels = "grpc:info"
	if err := Configure(o); err != nil {
		t.Errorf("Expecting success, got %v", err)
	} else if !o.logGRPC {
		t.Errorf("Expecting LogGrpc, got %v", o.logGRPC)
	}
}

func TestOddballs(t *testing.T) {
	resetGlobals()

	o := DefaultOptions()
	_ = Configure(o)

	o = DefaultOptions()
	o.outputLevels = "default,,"
	err := Configure(o)
	if err == nil {
		t.Error("Got success, expected failure")
	}

	o = DefaultOptions()
	o.outputLevels = "foobar"
	err = Configure(o)
	if err == nil {
		t.Error("Got success, expected failure")
	}

	o = DefaultOptions()
	o.outputLevels = "foobar:debug"
	err = Configure(o)
	if err != nil {
		t.Error("Got success, expected failure")
	}

	o = DefaultOptions()
	o.stackTraceLevels = "default,,"
	err = Configure(o)
	if err == nil {
		t.Error("Got success, expected failure")
	}

	o = DefaultOptions()
	o.stackTraceLevels = "foobar"
	err = Configure(o)
	if err == nil {
		t.Error("Got success, expected failure")
	}

	o = DefaultOptions()
	o.stackTraceLevels = "foobar:debug"
	err = Configure(o)
	if err != nil {
		t.Error("Got success, expected failure")
	}

	o = DefaultOptions()
	o.logCallers = "foobar"
	err = Configure(o)
	if err != nil {
		t.Error("Got success, expected failure")
	}

	o = DefaultOptions()
	// using invalid filename
	o.OutputPaths = []string{"//"}
	err = Configure(o)
	if err == nil {
		t.Errorf("Got success, expecting error")
	}

	o = DefaultOptions()
	o.ErrorOutputPaths = []string{"//"}
	err = Configure(o)
	if err == nil {
		t.Errorf("Got success, expecting error")
	}
}

func TestCapture(t *testing.T) {
	lines, _ := captureStdout(func() {
		o := DefaultOptions()
		o.logCallers = "default"
		o.SetDefaultOutputLevel(DefaultScopeName, DebugLevel)
		o.SetDefaultOutputLevel(GrpcScopeName, DebugLevel)
		_ = Configure(o)

		// output to the plain golang "log" package
		log.Println("golang")

		// output to the gRPC logging package
		grpclog.Error("grpc-error")
		grpclog.Warning("grpc-warn")
		grpclog.Info("grpc-info")

		// output directly to zap
		zap.L().Error("zap-error")
		zap.L().Warn("zap-warn")
		zap.L().Info("zap-info")
		zap.L().Debug("zap-debug")

		l := zap.L().With(zap.String("a", "b"))
		l.Error("zap-with")

		entry := zapcore.Entry{
			Message: "zap-write",
			Level:   zapcore.ErrorLevel,
		}
		_ = zap.L().Core().Write(entry, nil)

		defaultScope.SetOutputLevel(NoneLevel)
		grpcScope.SetOutputLevel(NoneLevel)

		// all these get thrown out since the level is set to none
		log.Println("golang-2")
		grpclog.Error("grpc-error-2")
		grpclog.Warning("grpc-warn-2")
		grpclog.Info("grpc-info-2")
		zap.L().Error("zap-error-2")
		zap.L().Warn("zap-warn-2")
		zap.L().Info("zap-info-2")
		zap.L().Debug("zap-debug-2")
	})

	patterns := []string{
		timePattern + "\tinfo\tlog/config_test.go:.*\tgolang",
		timePattern + "\terror\tlog/config_test.go:.*\tgrpc-error", // gRPC errors and warnings come out as info
		timePattern + "\twarn\tlog/config_test.go:.*\tgrpc-warn",
		timePattern + "\tinfo\tlog/config_test.go:.*\tgrpc-info",
		timePattern + "\terror\tlog/config_test.go:.*\tzap-error",
		timePattern + "\twarn\tlog/config_test.go:.*\tzap-warn",
		timePattern + "\tinfo\tlog/config_test.go:.*\tzap-info",
		timePattern + "\tdebug\tlog/config_test.go:.*\tzap-debug",
		timePattern + "\terror\tlog/config_test.go:.*\tzap-with",
		"error\tzap-write",
		"",
	}

	if len(lines) > len(patterns) {
		t.Errorf("Expecting %d lines of output, but got %d", len(patterns), len(lines))

		for i := len(patterns); i < len(lines); i++ {
			t.Errorf("  Extra line of output: %s", lines[i])
		}
	}

	for i, pat := range patterns {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			match, _ := regexp.MatchString(pat, lines[i])
			if !match {
				t.Errorf("Got '%s', expecting to match '%s'", lines[i], pat)
			}
		})
	}

	lines, _ = captureStdout(func() {
		o := DefaultOptions()
		o.stackTraceLevels = "default:debug"
		o.SetDefaultOutputLevel(DefaultScopeName, DebugLevel)
		_ = Configure(o)
		log.Println("golang")
	})

	for _, line := range lines {
		// see if the captured output contains the current file name
		if strings.Contains(line, "config_test.go") {
			return
		}
	}

	t.Error("Could not find stack trace info in output")
}

// Runs the given function while capturing everything sent to stdout
func captureStdout(f func()) ([]string, error) {
	tf, err := os.CreateTemp("", "log_test")
	if err != nil {
		return nil, err
	}

	old := os.Stdout
	os.Stdout = tf

	f()

	os.Stdout = old
	path := tf.Name()
	_ = tf.Sync()
	_ = tf.Close()

	content, err := os.ReadFile(path)
	_ = os.Remove(path)

	if err != nil {
		return nil, err
	}

	return strings.Split(string(content), "\n"), nil
}

func resetGlobals() {
	scopes = make(map[string]*Scope, 1)
	defaultScope, grpcScope = registerDefaultScopes()
}
