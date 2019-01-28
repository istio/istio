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
	"regexp"
	"strconv"
	"testing"
)

func testOptions() *Options {
	o := DefaultOptions()
	o.testonlyExit = func() {
		panic("os.Exit would have been called")
	}
	return o
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
			f:   func() { Debuga("Hello") },
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
			f:   func() { Infoa("Hello") },
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
			f:   func() { Warna("Hello") },
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
			f:   func() { Errora("Hello") },
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
			f:        func() { Fatala("Hello") },
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
				o.testonlyExit = func() {
					exitCalled = true
				}

				if err := Configure(o); err != nil {
					t.Errorf("Got err '%v', expecting success", err)
				}

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
			o.SetOutputLevel(DefaultScopeName, c.level)
			o.testonlyExit = func() {}
			_ = Configure(o)

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
