// Copyright 2020 Istio Authors
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

package clog

import (
	"fmt"
	"io"
	"os"

	"istio.io/pkg/log"
)

// Logger provides optional log taps for console and test buffer outputs.
type Logger interface {
	LogAndPrint(v ...interface{})
	LogAndError(v ...interface{})
	LogAndFatal(a ...interface{})
	LogAndPrintf(format string, a ...interface{})
	LogAndErrorf(format string, a ...interface{})
	LogAndFatalf(format string, a ...interface{})
	Print(s string)
	PrintErr(s string)
}

// DefaultLogger is a passthrough to istio.io/pkg/log.
type DefaultLogger struct{}

// NewDefaultLogger creates a new logger and returns a pointer to it.
func NewDefaultLogger() *DefaultLogger {
	return &DefaultLogger{}
}

func (l *DefaultLogger) LogAndPrint(v ...interface{}) {
	if len(v) == 0 {
		return
	}
	s := fmt.Sprint(v...)
	log.Infof(s)
}

func (l *DefaultLogger) LogAndError(v ...interface{}) {
	if len(v) == 0 {
		return
	}
	s := fmt.Sprint(v...)
	log.Infof(s)
}

func (l *DefaultLogger) LogAndFatal(a ...interface{}) {
	l.LogAndError(a...)
	os.Exit(-1)
}

func (l *DefaultLogger) LogAndPrintf(format string, a ...interface{}) {
	s := fmt.Sprintf(format, a...)
	log.Infof(s)
}

func (l *DefaultLogger) LogAndErrorf(format string, a ...interface{}) {
	s := fmt.Sprintf(format, a...)
	log.Infof(s)
}

func (l *DefaultLogger) LogAndFatalf(format string, a ...interface{}) {
	l.LogAndErrorf(format, a...)
	os.Exit(-1)
}

func (l *DefaultLogger) Print(s string) {
}

func (l *DefaultLogger) PrintErr(s string) {
}

//ConsoleLogger is the struct used for mesh command
type ConsoleLogger struct {
	stdOut io.Writer
	stdErr io.Writer
}

// NewConsoleLogger creates a new logger and returns a pointer to it.
// stdOut and stdErr can be used to capture output for testing.
func NewConsoleLogger(stdOut, stdErr io.Writer) *ConsoleLogger {
	return &ConsoleLogger{
		stdOut: stdOut,
		stdErr: stdErr,
	}
}

func (l *ConsoleLogger) LogAndPrint(v ...interface{}) {
	if len(v) == 0 {
		return
	}
	s := fmt.Sprint(v...)
	l.Print(s + "\n")
	log.Infof(s)
}

func (l *ConsoleLogger) LogAndError(v ...interface{}) {
	if len(v) == 0 {
		return
	}
	s := fmt.Sprint(v...)
	l.PrintErr(s + "\n")
	log.Infof(s)
}

func (l *ConsoleLogger) LogAndFatal(a ...interface{}) {
	l.LogAndError(a...)
	os.Exit(-1)
}

func (l *ConsoleLogger) LogAndPrintf(format string, a ...interface{}) {
	s := fmt.Sprintf(format, a...)
	l.Print(s + "\n")
	log.Infof(s)
}

func (l *ConsoleLogger) LogAndErrorf(format string, a ...interface{}) {
	s := fmt.Sprintf(format, a...)
	l.PrintErr(s + "\n")
	log.Infof(s)
}

func (l *ConsoleLogger) LogAndFatalf(format string, a ...interface{}) {
	l.LogAndErrorf(format, a...)
	os.Exit(-1)
}

func (l *ConsoleLogger) Print(s string) {
	_, _ = l.stdOut.Write([]byte(s))
}

func (l *ConsoleLogger) PrintErr(s string) {
	_, _ = l.stdErr.Write([]byte(s))
}
