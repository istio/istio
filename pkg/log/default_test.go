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

func TestDefault(t *testing.T) {
	cases := []struct {
		f          func()
		pat        string
		json       bool
		caller     bool
		stackLevel Level
	}{
		{func() { Debug("Hello") }, timePattern + "\tdebug\tHello", false, false, NoneLevel},
		{func() { Debugf("Hello") }, timePattern + "\tdebug\tHello", false, false, NoneLevel},
		{func() { Debugf("%s", "Hello") }, timePattern + "\tdebug\tHello", false, false, NoneLevel},
		{func() { Debuga("Hello") }, timePattern + "\tdebug\tHello", false, false, NoneLevel},

		{func() { Info("Hello") }, timePattern + "\tinfo\tHello", false, false, NoneLevel},
		{func() { Infof("Hello") }, timePattern + "\tinfo\tHello", false, false, NoneLevel},
		{func() { Infof("%s", "Hello") }, timePattern + "\tinfo\tHello", false, false, NoneLevel},
		{func() { Infoa("Hello") }, timePattern + "\tinfo\tHello", false, false, NoneLevel},

		{func() { Warn("Hello") }, timePattern + "\twarn\tHello", false, false, NoneLevel},
		{func() { Warnf("Hello") }, timePattern + "\twarn\tHello", false, false, NoneLevel},
		{func() { Warnf("%s", "Hello") }, timePattern + "\twarn\tHello", false, false, NoneLevel},
		{func() { Warna("Hello") }, timePattern + "\twarn\tHello", false, false, NoneLevel},

		{func() { Error("Hello") }, timePattern + "\terror\tHello", false, false, NoneLevel},
		{func() { Errorf("Hello") }, timePattern + "\terror\tHello", false, false, NoneLevel},
		{func() { Errorf("%s", "Hello") }, timePattern + "\terror\tHello", false, false, NoneLevel},
		{func() { Errora("Hello") }, timePattern + "\terror\tHello", false, false, NoneLevel},

		{func() { Fatal("Hello") }, timePattern + "\tfatal\tHello", false, false, NoneLevel},
		{func() { Fatalf("Hello") }, timePattern + "\tfatal\tHello", false, false, NoneLevel},
		{func() { Fatalf("%s", "Hello") }, timePattern + "\tfatal\tHello", false, false, NoneLevel},
		{func() { Fatala("Hello") }, timePattern + "\tfatal\tHello", false, false, NoneLevel},

		{func() { Debug("Hello") }, timePattern + "\tdebug\tlog/default_test.go:.*\tHello", false, true, NoneLevel},

		{func() { Debug("Hello") }, "{\"level\":\"debug\",\"time\":\"" + timePattern + "\",\"caller\":\"log/default_test.go:.*\",\"msg\":\"Hello\"," +
			"\"stack\":\".*\"}",
			true, true, DebugLevel},
		{func() { Info("Hello") }, "{\"level\":\"info\",\"time\":\"" + timePattern + "\",\"caller\":\"log/default_test.go:.*\",\"msg\":\"Hello\"," +
			"\"stack\":\".*\"}",
			true, true, DebugLevel},
		{func() { Warn("Hello") }, "{\"level\":\"warn\",\"time\":\"" + timePattern + "\",\"caller\":\"log/default_test.go:.*\",\"msg\":\"Hello\"," +
			"\"stack\":\".*\"}",
			true, true, DebugLevel},
		{func() { Error("Hello") }, "{\"level\":\"error\",\"time\":\"" + timePattern + "\",\"caller\":\"log/default_test.go:.*\",\"msg\":\"Hello\"," +
			"\"stack\":\".*\"}",
			true, true, DebugLevel},
		{func() { Fatal("Hello") }, "{\"level\":\"fatal\",\"time\":\"" + timePattern + "\",\"caller\":\"log/default_test.go:.*\",\"msg\":\"Hello\"," +
			"\"stack\":\".*\"}",
			true, true, DebugLevel},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			lines, err := captureStdout(func() {
				o := DefaultOptions()
				o.JSONEncoding = c.json

				if err := Configure(o); err != nil {
					t.Errorf("Got err '%v', expecting success", err)
				}

				defaultScope.SetOutputLevel(DebugLevel)
				defaultScope.SetStackTraceLevel(c.stackLevel)
				defaultScope.SetLogCallers(c.caller)

				c.f()
				_ = Sync()
			})

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
			o := DefaultOptions()
			o.SetOutputLevel(DefaultScopeName, c.level)
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
