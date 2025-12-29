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
	"encoding/json"
	"regexp"
	"strconv"
	"testing"
	"time"
)

func testOptions() *Options {
	return DefaultOptions()
}

func TestDefault(t *testing.T) {
	cases := []struct {
		f          func()
		pat        string
		json       bool
		caller     bool
		wantExit   bool
		stackLevel Level
	}{
		{
			f:   func() { Debug("Hello") },
			pat: timePattern + "\tdebug\tHello",
		},
		{
			f:   func() { Debugf("Hello") },
			pat: timePattern + "\tdebug\tHello",
		},
		{
			f:   func() { Debugf("%s", "Hello") },
			pat: timePattern + "\tdebug\tHello",
		},

		{
			f:   func() { Info("Hello") },
			pat: timePattern + "\tinfo\tHello",
		},
		{
			f:   func() { Infof("Hello") },
			pat: timePattern + "\tinfo\tHello",
		},
		{
			f:   func() { Infof("%s", "Hello") },
			pat: timePattern + "\tinfo\tHello",
		},
		{
			f:   func() { Warn("Hello") },
			pat: timePattern + "\twarn\tHello",
		},
		{
			f:   func() { Warnf("Hello") },
			pat: timePattern + "\twarn\tHello",
		},
		{
			f:   func() { Warnf("%s", "Hello") },
			pat: timePattern + "\twarn\tHello",
		},

		{
			f:   func() { Error("Hello") },
			pat: timePattern + "\terror\tHello",
		},
		{
			f:   func() { Errorf("Hello") },
			pat: timePattern + "\terror\tHello",
		},
		{
			f:   func() { Errorf("%s", "Hello") },
			pat: timePattern + "\terror\tHello",
		},

		{
			f:        func() { Fatal("Hello") },
			pat:      timePattern + "\tfatal\tHello",
			wantExit: true,
		},
		{
			f:        func() { Fatalf("Hello") },
			pat:      timePattern + "\tfatal\tHello",
			wantExit: true,
		},
		{
			f:        func() { Fatalf("%s", "Hello") },
			pat:      timePattern + "\tfatal\tHello",
			wantExit: true,
		},

		{
			f:      func() { Debug("Hello") },
			pat:    timePattern + "\tdebug\tlog/default_test.go:.*\tHello",
			caller: true,
		},

		{
			f: func() { Debug("Hello") },
			pat: "{\"level\":\"debug\",\"time\":\"" + timePattern + "\",\"caller\":\"log/default_test.go:.*\",\"msg\":\"Hello\"," +
				"\"stack\":\".*\"}",
			json:       true,
			caller:     true,
			stackLevel: DebugLevel,
		},
		{
			f: func() { Info("Hello") },
			pat: "{\"level\":\"info\",\"time\":\"" + timePattern + "\",\"caller\":\"log/default_test.go:.*\",\"msg\":\"Hello\"," +
				"\"stack\":\".*\"}",
			json:       true,
			caller:     true,
			stackLevel: DebugLevel,
		},
		{
			f: func() { Warn("Hello") },
			pat: "{\"level\":\"warn\",\"time\":\"" + timePattern + "\",\"caller\":\"log/default_test.go:.*\",\"msg\":\"Hello\"," +
				"\"stack\":\".*\"}",
			json:       true,
			caller:     true,
			stackLevel: DebugLevel,
		},
		{
			f: func() { Error("Hello") },
			pat: "{\"level\":\"error\",\"time\":\"" + timePattern + "\",\"caller\":\"log/default_test.go:.*\",\"msg\":\"Hello\"," +
				"\"stack\":\".*\"}",
			json:       true,
			caller:     true,
			stackLevel: DebugLevel,
		},
		{
			f: func() { Fatal("Hello") },
			pat: "{\"level\":\"fatal\",\"time\":\"" + timePattern + "\",\"caller\":\"log/default_test.go:.*\",\"msg\":\"Hello\"," +
				"\"stack\":\".*\"}",
			json:       true,
			caller:     true,
			wantExit:   true,
			stackLevel: DebugLevel,
		},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			var exitCalled bool
			lines, err := captureStdout(func() {
				o := testOptions()
				o.JSONEncoding = c.json

				if err := Configure(o); err != nil {
					t.Errorf("Got err '%v', expecting success", err)
				}

				pt := funcs.Load().(patchTable)
				pt.exitProcess = func(_ int) {
					exitCalled = true
				}
				funcs.Store(pt)

				defaultScope.SetOutputLevel(DebugLevel)
				defaultScope.SetStackTraceLevel(c.stackLevel)
				defaultScope.SetLogCallers(c.caller)

				c.f()
				_ = Sync()
			})

			if exitCalled != c.wantExit {
				var verb string
				if c.wantExit {
					verb = " never"
				}
				t.Errorf("os.Exit%s called", verb)
			}

			if err != nil {
				t.Errorf("Got error '%v', expected success", err)
			}

			if match, _ := regexp.MatchString(c.pat, lines[0]); !match {
				t.Errorf("Got '%v', expected a match with '%v'", lines[0], c.pat)
			}
		})
	}
}

func TestEnabled(t *testing.T) {
	cases := []struct {
		level        Level
		debugEnabled bool
		infoEnabled  bool
		warnEnabled  bool
		errorEnabled bool
		fatalEnabled bool
	}{
		{DebugLevel, true, true, true, true, true},
		{InfoLevel, false, true, true, true, true},
		{WarnLevel, false, false, true, true, true},
		{ErrorLevel, false, false, false, true, true},
		{FatalLevel, false, false, false, false, true},
		{NoneLevel, false, false, false, false, false},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			o := testOptions()
			o.SetDefaultOutputLevel(DefaultScopeName, c.level)

			_ = Configure(o)

			pt := funcs.Load().(patchTable)
			pt.exitProcess = func(_ int) {
			}
			funcs.Store(pt)

			if c.debugEnabled != DebugEnabled() {
				t.Errorf("Got %v, expecting %v", DebugEnabled(), c.debugEnabled)
			}

			if c.infoEnabled != InfoEnabled() {
				t.Errorf("Got %v, expecting %v", InfoEnabled(), c.infoEnabled)
			}

			if c.warnEnabled != WarnEnabled() {
				t.Errorf("Got %v, expecting %v", WarnEnabled(), c.warnEnabled)
			}

			if c.errorEnabled != ErrorEnabled() {
				t.Errorf("Got %v, expecting %v", ErrorEnabled(), c.errorEnabled)
			}

			if c.fatalEnabled != FatalEnabled() {
				t.Errorf("Got %v, expecting %v", FatalEnabled(), c.fatalEnabled)
			}
		})
	}
}

func TestDefaultWithLabel(t *testing.T) {
	lines, err := captureStdout(func() {
		Configure(DefaultOptions())
		funcs.Store(funcs.Load().(patchTable))
		WithLabels("foo", "bar").WithLabels("baz", 123, "qux", 0.123).Error("Hello")

		_ = Sync()
	})
	if err != nil {
		t.Errorf("Got error '%v', expected success", err)
	}

	mustRegexMatchString(t, lines[0], `Hello	foo=bar baz=123 qux=0.123`)
}

func TestLogWithTime(t *testing.T) {
	getLogTime := func(t *testing.T, line string) time.Time {
		type logEntry struct {
			Time time.Time `json:"time"`
		}
		var e logEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("Failed to unmarshal log entry: %v", err)
		}
		return e.Time
	}

	t.Run("specified time", func(t *testing.T) {
		yesterday := time.Now().Add(-time.Hour * 24).Truncate(time.Microsecond)

		stdoutLines, _ := captureStdout(func() {
			o := DefaultOptions()
			o.JSONEncoding = true
			_ = Configure(o)
			defaultScope.LogWithTime(InfoLevel, "Hello", yesterday)
		})

		gotTime := getLogTime(t, stdoutLines[0])
		if gotTime.UnixNano() != yesterday.UnixNano() {
			t.Fatalf("Got time %v, expected %v", gotTime, yesterday)
		}
	})

	t.Run("empty time", func(t *testing.T) {
		stdoutLines, _ := captureStdout(func() {
			o := DefaultOptions()
			o.JSONEncoding = true
			_ = Configure(o)

			var ti time.Time
			defaultScope.LogWithTime(InfoLevel, "Hello", ti)
		})

		gotTime := getLogTime(t, stdoutLines[0])
		if gotTime.IsZero() {
			t.Fatalf("Got %v, expected non-zero", gotTime)
		}
	})
}
