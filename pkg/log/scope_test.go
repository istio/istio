// Copyright Istio Authors
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
	"time"

	"go.uber.org/zap/zapcore"
	"k8s.io/klog/v2"
)

func runTest(t *testing.T, f func()) []string {
	lines, err := captureStdout(func() {
		Configure(DefaultOptions())
		f()
		_ = Sync()
	})
	if err != nil {
		t.Fatalf("Got error '%v', expected success", err)
	}
	if lines[len(lines)-1] == "" {
		return lines[:len(lines)-1]
	}
	return lines
}

func TestKlog(t *testing.T) {
	cases := []struct {
		log      func()
		expected string
	}{
		{
			func() { klog.Error("foo") },
			"error\tklog\tfoo$",
		},
		{
			func() { klog.Error("foo") },
			"foo$",
		},
		{
			func() { klog.Errorf("fmt %v", "item") },
			"fmt item$",
		},
		{
			func() { klog.Errorf("fmt %v", "item") },
			"fmt item$",
		},
		{
			func() { klog.ErrorS(errors.New("my error"), "info") },
			"error\tklog\tmy error: info",
		},
		{
			func() { klog.Info("a", "b") },
			"info\tklog\tab",
		},
		{
			func() { klog.InfoS("msg", "key", 1) },
			"info\tklog\tmsg\tkey=1",
		},
		{
			func() { klog.ErrorS(errors.New("my error"), "info", "key", 1) },
			"error\tklog\tmy error: info\tkey=1",
		},
	}
	for _, tt := range cases {
		t.Run(tt.expected, func(t *testing.T) {
			lines := runTest(t, tt.log)
			mustMatchLength(t, 1, lines)
			mustRegexMatchString(t, lines[0], tt.expected)
		})
	}
}

func TestBasicScopes(t *testing.T) {
	s := RegisterScope("testScope", "z")

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
			f:        func() { s.Fatal("Hello") },
			pat:      timePattern + "\tfatal\ttestScope\tHello",
			wantExit: true,
		},
		{
			f:        func() { s.Fatal("Hello") },
			pat:      timePattern + "\tfatal\ttestScope\tHello",
			wantExit: true,
		},
		{
			f:        func() { s.Fatalf("%s", "Hello") },
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

				if err := Configure(o); err != nil {
					t.Errorf("Got err '%v', expecting success", err)
				}

				pt := funcs.Load().(patchTable)
				pt.exitProcess = func(_ int) {
					exitCalled = true
				}
				funcs.Store(pt)

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
				t.Errorf("Got '%v',\nexpected a match with '%v'", lines[0], c.pat)
			}
		})
	}
}

func TestScopeWithLabel(t *testing.T) {
	const name = "TestScope"
	const desc = "Desc"
	s := RegisterScope(name, desc)
	s.SetOutputLevel(DebugLevel)

	lines, err := captureStdout(func() {
		Configure(DefaultOptions())
		funcs.Store(funcs.Load().(patchTable))
		s2 := s.WithLabels("foo", "bar").WithLabels("baz", 123, "qux", 0.123, "dur", time.Millisecond*101).WithLabels("foo", "override")
		s2.Debug("Hello")
		// s should be unmodified.
		s.Debug("Hello")

		_ = Sync()
	})
	if err != nil {
		t.Errorf("Got error '%v', expected success", err)
	}

	mustRegexMatchString(t, lines[0], `Hello	foo=override baz=123 qux=0.123 dur=101ms`)
	mustRegexMatchString(t, lines[1], "Hello$")
}

func TestScopeJSON(t *testing.T) {
	const name = "TestScope"
	const desc = "Desc"
	s := RegisterScope(name, desc)
	s.SetOutputLevel(DebugLevel)

	lines, err := captureStdout(func() {
		o := DefaultOptions()
		o.JSONEncoding = true
		Configure(o)
		funcs.Store(funcs.Load().(patchTable))
		s.WithLabels("foo", "bar", "baz", 123, "dur", time.Millisecond*101).Debug("Hello")

		_ = Sync()
	})
	if err != nil {
		t.Errorf("Got error '%v', expected success", err)
	}

	mustRegexMatchString(t, lines[0], `{.*"msg":"Hello","foo":"bar","baz":123,"dur":"101ms"}`)
}

func mustRegexMatchString(t *testing.T, got, want string) {
	t.Helper()
	match, _ := regexp.MatchString(want, got)

	if !match {
		t.Fatalf("Got '%v', expected a match with '%v'", got, want)
	}
}

func TestScopeEnabled(t *testing.T) {
	const name = "TestEnabled"
	const desc = "Desc"
	s := RegisterScope(name, desc)

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
	z1 := RegisterScope("zzzz", "z")
	z2 := RegisterScope("zzzz", "z")

	if z1 != z2 {
		t.Error("Expecting the same scope objects, got different ones")
	}
}

func TestFind(t *testing.T) {
	if z := FindScope("TestFind"); z != nil {
		t.Error("Found scope, but expected it wouldn't exist")
	}

	_ = RegisterScope("TestFind", "")

	if z := FindScope("TestFind"); z == nil {
		t.Error("Did not find scope, expected to find it")
	}
}

func TestBadNames(t *testing.T) {
	badNames := []string{
		"a:b",
		"a,b",
		"a.b",

		":ab",
		",ab",
		".ab",

		"ab:",
		"ab,",
		"ab.",
	}

	for _, name := range badNames {
		tryBadName(t, name)
	}
}

func tryBadName(t *testing.T, name string) {
	defer func() {
		if r := recover(); r != nil {
			return
		}
		t.Errorf("Expecting to panic when using bad scope name %s, but didn't", name)
	}()

	_ = RegisterScope(name, "A poorly named scope")
}

func TestBadWriter(t *testing.T) {
	o := testOptions()
	if err := Configure(o); err != nil {
		t.Errorf("Got err '%v', expecting success", err)
	}

	pt := funcs.Load().(patchTable)
	pt.write = func(zapcore.Entry, []zapcore.Field) error {
		return errors.New("bad")
	}
	funcs.Store(pt)

	// for now, we just make sure this doesn't crash. To be totally correct, we'd need to capture stderr and
	// inspect it, but it's just not worth it
	defaultScope.Error("TestBadWriter")
}

// Regression test for a race condition
func TestUpdateScope(t *testing.T) {
	o := testOptions()
	if err := Configure(o); err != nil {
		t.Errorf("Got err '%v', expecting success", err)
	}

	go func() {
		for range 100 {
			defaultScope.SetOutputLevel(DebugLevel)
		}
	}()
	go func() {
		for range 100 {
			defaultScope.WithLabels("foo", "bar")
		}
	}()
}

func BenchmarkLog(b *testing.B) {
	runOpts := func(name string, opts func(*Options), f func()) {
		b.Run(name, func(b *testing.B) {
			o := testOptions()
			o.OutputPaths = []string{"/dev/null"}
			opts(o)
			if err := Configure(o); err != nil {
				b.Fatalf("Got err '%v', expecting success", err)
			}
			for n := 0; n < b.N; n++ {
				f()
			}
		})
	}
	run := func(name string, f func()) {
		runOpts(name, func(options *Options) {}, f)
	}
	run("default", func() {
		Info("some message")
	})
	run("formatted", func() {
		Infof("some %s", "message")
	})
	scope := WithLabels("some", "value", "foo", 123)
	run("precreated labels", func() {
		scope.Infof("some %s", "message")
	})
	run("labels", func() {
		WithLabels("some", "value", "foo", 123).Infof("some %s", "message")
	})
	run("precreated and dynamic labels", func() {
		scope.WithLabels("fruit", "apply").Infof("some %s", "message")
	})
	runOpts("json", func(options *Options) {
		options.JSONEncoding = true
	}, func() {
		scope.Infof("some %s", "message")
	})
}
