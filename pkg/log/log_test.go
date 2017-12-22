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
	"errors"
	"io/ioutil"
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

func TestBasic(t *testing.T) {
	cases := []struct {
		f          func()
		pat        string
		json       bool
		caller     bool
		stackLevel zapcore.Level
	}{
		{func() { Debug("Hello") }, timePattern + "\tdebug\tHello", false, false, None},
		{func() { Debugf("Hello") }, timePattern + "\tdebug\tHello", false, false, None},
		{func() { Debugw("Hello") }, timePattern + "\tdebug\tHello", false, false, None},
		{func() { Debuga("Hello") }, timePattern + "\tdebug\tHello", false, false, None},

		{func() { Info("Hello") }, timePattern + "\tinfo\tHello", false, false, None},
		{func() { Infof("Hello") }, timePattern + "\tinfo\tHello", false, false, None},
		{func() { Infow("Hello") }, timePattern + "\tinfo\tHello", false, false, None},
		{func() { Infoa("Hello") }, timePattern + "\tinfo\tHello", false, false, None},

		{func() { Warn("Hello") }, timePattern + "\twarn\tHello", false, false, None},
		{func() { Warnf("Hello") }, timePattern + "\twarn\tHello", false, false, None},
		{func() { Warnw("Hello") }, timePattern + "\twarn\tHello", false, false, None},
		{func() { Warna("Hello") }, timePattern + "\twarn\tHello", false, false, None},

		{func() { Error("Hello") }, timePattern + "\terror\tHello", false, false, None},
		{func() { Errorf("Hello") }, timePattern + "\terror\tHello", false, false, None},
		{func() { Errorw("Hello") }, timePattern + "\terror\tHello", false, false, None},
		{func() { Errora("Hello") }, timePattern + "\terror\tHello", false, false, None},

		{func() {
			l := With(zap.String("key", "value"))
			l.Debug("Hello")
		}, timePattern + "\tdebug\tHello\t{\"key\": \"value\"}", false, false, None},

		{func() { Debug("Hello") }, timePattern + "\tdebug\tlog/log_test.go:.*\tHello", false, true, None},

		{func() { Debug("Hello") }, "{\"level\":\"debug\",\"time\":\"" + timePattern + "\",\"caller\":\"log/log_test.go:.*\",\"msg\":\"Hello\"," +
			"\"stack\":\".*\"}",
			true, true, zapcore.DebugLevel},
		{func() { Info("Hello") }, "{\"level\":\"info\",\"time\":\"" + timePattern + "\",\"caller\":\"log/log_test.go:.*\",\"msg\":\"Hello\"," +
			"\"stack\":\".*\"}",
			true, true, zapcore.DebugLevel},
		{func() { Warn("Hello") }, "{\"level\":\"warn\",\"time\":\"" + timePattern + "\",\"caller\":\"log/log_test.go:.*\",\"msg\":\"Hello\"," +
			"\"stack\":\".*\"}",
			true, true, zapcore.DebugLevel},
		{func() { Error("Hello") }, "{\"level\":\"error\",\"time\":\"" + timePattern + "\",\"caller\":\"log/log_test.go:.*\",\"msg\":\"Hello\"," +
			"\"stack\":\".*\"}",
			true, true, zapcore.DebugLevel},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			lines, err := captureStdout(func() {
				o := NewOptions()

				o.JSONEncoding = c.json
				o.IncludeCallerSourceLocation = c.caller

				_ = o.SetOutputLevel(zapcore.DebugLevel)
				_ = o.SetStackTraceLevel(c.stackLevel)
				if err := Configure(o); err != nil {
					t.Errorf("Got err '%v', expecting success", err)
				}

				c.f()
				Sync()
			})

			if err != nil {
				t.Errorf("Got error '%v', expected success", err)
			}

			if match, _ := regexp.MatchString(c.pat, lines[0]); !match {
				t.Errorf("Got '%v', expected a match with '%v'", lines[0], c.pat)
			}
		})
	}

	// sadly, only testing whether we crash or not...
	l := With(zap.String("Key", "Value"))
	l.Debug("Hello")
	l.Sync()
}

func TestEnabled(t *testing.T) {
	cases := []struct {
		level        zapcore.Level
		debugEnabled bool
		infoEnabled  bool
		warnEnabled  bool
		errorEnabled bool
	}{
		{zapcore.DebugLevel, true, true, true, true},
		{zapcore.InfoLevel, false, true, true, true},
		{zapcore.WarnLevel, false, false, true, true},
		{zapcore.ErrorLevel, false, false, false, true},
		{None, false, false, false, false},
	}

	for i, c := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			o := NewOptions()
			_ = o.SetOutputLevel(c.level)
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
		})
	}
}

func TestOddballs(t *testing.T) {
	o := NewOptions()
	_ = Configure(o)

	o = NewOptions()
	o.outputLevel = "foobar"
	err := Configure(o)
	if err == nil {
		t.Errorf("Got success, expected failure")
	}

	o = NewOptions()
	o.stackTraceLevel = "foobar"
	err = Configure(o)
	if err == nil {
		t.Errorf("Got success, expected failure")
	}

	o = NewOptions()
	err = configure(o, func(c *zap.Config) (*zap.Logger, error) { return nil, errors.New("BAD") })
	if err == nil {
		t.Errorf("Got success, expecting error")
	}
}

func TestCapture(t *testing.T) {
	lines, _ := captureStdout(func() {
		o := NewOptions()
		o.IncludeCallerSourceLocation = true
		_ = Configure(o)

		// output to the plain golang "log" package
		log.Println("Hello")

		// output to the gRPC logging package
		grpclog.Info("There")

		// output directly to zap
		zap.L().Info("Goodbye")
	})

	patterns := []string{
		timePattern + "\tinfo\tlog/log_test.go:.*\tHello",
		timePattern + "\tinfo\tlog/log_test.go:.*\tThere",
		timePattern + "\tinfo\tlog/log_test.go:.*\tGoodbye",
	}

	for i, pat := range patterns {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			match, _ := regexp.MatchString(pat, lines[i])
			if !match {
				t.Errorf("Got '%s', expecting to match '%s'", lines[i], pat)
			}
		})
	}
}

// Runs the given function while capturing everything sent to stdout
func captureStdout(f func()) ([]string, error) {
	tf, err := ioutil.TempFile("", "log_test")
	if err != nil {
		return nil, err
	}

	old := os.Stdout
	os.Stdout = tf

	f()

	os.Stdout = old
	path := tf.Name()
	tf.Sync()
	tf.Close()

	content, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	_ = os.Remove(path)

	return strings.Split(string(content), "\n"), nil
}
