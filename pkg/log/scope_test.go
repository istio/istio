// Copyright 2018 Istio Authors
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
	"errors"
	"regexp"
	"strconv"
	"testing"

	"go.uber.org/zap/zapcore"
)

func TestBasicScopes(t *testing.T) {
	s := RegisterScope("testScope", "z", 0)

	cases := []struct {
		f          func()
		pat        string
		json       bool
		caller     bool
		wantExit   bool
		stackLevel Level
	}{
		{
			f:   func() { s.Debug("Hello") },
			pat: timePattern + "\tdebug\ttestScope\tHello",
		},
		{
			f:   func() { s.Debugf("Hello") },
			pat: timePattern + "\tdebug\ttestScope\tHello",
		},
		{
			f:   func() { s.Debugf("%s", "Hello") },
			pat: timePattern + "\tdebug\ttestScope\tHello",
		},
		{
			f:   func() { s.Debuga("Hello") },
			pat: timePattern + "\tdebug\ttestScope\tHello",
		},

		{
			f:   func() { s.Info("Hello") },
			pat: timePattern + "\tinfo\ttestScope\tHello",
		},
		{
			f:   func() { s.Infof("Hello") },
			pat: timePattern + "\tinfo\ttestScope\tHello",
		},
		{
			f:   func() { s.Infof("%s", "Hello") },
			pat: timePattern + "\tinfo\ttestScope\tHello",
		},
		{
			f:   func() { s.Infoa("Hello") },
			pat: timePattern + "\tinfo\ttestScope\tHello",
		},

		{
			f:   func() { s.Warn("Hello") },
			pat: timePattern + "\twarn\ttestScope\tHello",
		},
		{
			f:   func() { s.Warnf("Hello") },
			pat: timePattern + "\twarn\ttestScope\tHello",
		},
		{
			f:   func() { s.Warnf("%s", "Hello") },
			pat: timePattern + "\twarn\ttestScope\tHello",
		},
		{
			f:   func() { s.Warna("Hello") },
			pat: timePattern + "\twarn\ttestScope\tHello",
		},

		{
			f:   func() { s.Error("Hello") },
			pat: timePattern + "\terror\ttestScope\tHello",
		},
		{
			f:   func() { s.Errorf("Hello") },
			pat: timePattern + "\terror\ttestScope\tHello",
		},
		{
			f:   func() { s.Errorf("%s", "Hello") },
			pat: timePattern + "\terror\ttestScope\tHello",
		},
		{
			f:   func() { s.Errora("Hello") },
			pat: timePattern + "\terror\ttestScope\tHello",
		},

		{
			f:        func() { s.Fatal("Hello") },
			pat:      timePattern + "\tfatal\ttestScope\tHello",
			wantExit: true,
		},
		{
			f:        func() { s.Fatalf("Hello") },
			pat:      timePattern + "\tfatal\ttestScope\tHello",
			wantExit: true,
		},
		{
			f:        func() { s.Fatalf("%s", "Hello") },
			pat:      timePattern + "\tfatal\ttestScope\tHello",
			wantExit: true,
		},
		{
			f:        func() { s.Fatala("Hello") },
			pat:      timePattern + "\tfatal\ttestScope\tHello",
			wantExit: true,
		},

		{
			f:      func() { s.Debug("Hello") },
			pat:    timePattern + "\tdebug\ttestScope\tlog/scope_test.go:.*\tHello",
			caller: true,
		},

		{
			f: func() { s.Debug("Hello") },
			pat: "{\"level\":\"debug\",\"time\":\"" + timePattern + "\",\"scope\":\"testScope\",\"caller\":\"log/scope_test.go:.*\",\"msg\":\"Hello\"," +
				"\"stack\":\".*\"}",
			json:       true,
			caller:     true,
			stackLevel: DebugLevel,
		},
		{
			f: func() { s.Info("Hello") },
			pat: "{\"level\":\"info\",\"time\":\"" + timePattern + "\",\"scope\":\"testScope\",\"caller\":\"log/scope_test.go:.*\",\"msg\":\"Hello\"," +
				"\"stack\":\".*\"}",
			json:       true,
			caller:     true,
			stackLevel: DebugLevel,
		},
		{
			f: func() { s.Warn("Hello") },
			pat: "{\"level\":\"warn\",\"time\":\"" + timePattern + "\",\"scope\":\"testScope\",\"caller\":\"log/scope_test.go:.*\",\"msg\":\"Hello\"," +
				"\"stack\":\".*\"}",
			json:       true,
			caller:     true,
			stackLevel: DebugLevel,
		},
		{
			f: func() { s.Error("Hello") },
			pat: "{\"level\":\"error\",\"time\":\"" + timePattern + "\",\"scope\":\"testScope\",\"caller\":\"log/scope_test.go:.*\"," +
				"\"msg\":\"Hello\"," +
				"\"stack\":\".*\"}",
			json:       true,
			caller:     true,
			stackLevel: DebugLevel,
		},
		{
			f: func() { s.Fatal("Hello") },
			pat: "{\"level\":\"fatal\",\"time\":\"" + timePattern + "\",\"scope\":\"testScope\",\"caller\":\"log/scope_test.go:.*\"," +
				"\"msg\":\"Hello\"," +
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

				s.SetOutputLevel(DebugLevel)
				s.SetStackTraceLevel(c.stackLevel)
				s.SetLogCallers(c.caller)

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

func TestScopeEnabled(t *testing.T) {
	const name = "TestEnabled"
	const desc = "Desc"
	s := RegisterScope(name, desc, 0)

	if n := s.Name(); n != name {
		t.Errorf("Got %s, expected %s", n, name)
	}

	if d := s.Description(); d != desc {
		t.Errorf("Got %s, expected %s", d, desc)
	}

	cases := []struct {
		level        Level
		debugEnabled bool
		infoEnabled  bool
		warnEnabled  bool
		errorEnabled bool
		fatalEnabled bool
	}{
		{NoneLevel, false, false, false, false, false},
		{FatalLevel, false, false, false, false, true},
		{ErrorLevel, false, false, false, true, true},
		{WarnLevel, false, false, true, true, true},
		{InfoLevel, false, true, true, true, true},
		{DebugLevel, true, true, true, true, true},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			s.SetOutputLevel(c.level)

			if c.debugEnabled != s.DebugEnabled() {
				t.Errorf("Got %v, expected %v", s.DebugEnabled(), c.debugEnabled)
			}

			if c.infoEnabled != s.InfoEnabled() {
				t.Errorf("Got %v, expected %v", s.InfoEnabled(), c.infoEnabled)
			}

			if c.warnEnabled != s.WarnEnabled() {
				t.Errorf("Got %v, expected %v", s.WarnEnabled(), c.warnEnabled)
			}

			if c.errorEnabled != s.ErrorEnabled() {
				t.Errorf("Got %v, expected %v", s.ErrorEnabled(), c.errorEnabled)
			}

			if c.fatalEnabled != s.FatalEnabled() {
				t.Errorf("Got %v, expected %v", s.FatalEnabled(), c.fatalEnabled)
			}

			if c.level != s.GetOutputLevel() {
				t.Errorf("Got %v, expected %v", s.GetOutputLevel(), c.level)
			}
		})
	}
}

func TestMultipleScopesWithSameName(t *testing.T) {
	z1 := RegisterScope("zzzz", "z", 0)
	z2 := RegisterScope("zzzz", "z", 0)

	if z1 != z2 {
		t.Error("Expecting the same scope objects, got different ones")
	}
}

func TestFind(t *testing.T) {
	if z := FindScope("TestFind"); z != nil {
		t.Error("Found scope, but expected it wouldn't exist")
	}

	_ = RegisterScope("TestFind", "", 0)

	if z := FindScope("TestFind"); z == nil {
		t.Error("Did not find scope, expected to find it")
	}
}

func TestBadNames(t *testing.T) {
	if s := RegisterScope("a:b", "", 0); s != nil {
		t.Error("Expecting to get nil")
	}

	if s := RegisterScope("a,b", "", 0); s != nil {
		t.Error("Expecting to get nil")
	}

	if s := RegisterScope("a.b", "", 0); s != nil {
		t.Error("Expecting to get nil")
	}
}

func TestBadWriter(t *testing.T) {
	o := testOptions()
	if err := Configure(o); err != nil {
		t.Errorf("Got err '%v', expecting success", err)
	}

	writeFn.Store(func(zapcore.Entry, []zapcore.Field) error {
		return errors.New("bad")
	})

	// for now, we just make sure this doesn't crash. To be totally correct, we'd need to capture stderr and
	// inspect it, but it's just not worth it
	defaultScope.Error("TestBadWriter")
}
