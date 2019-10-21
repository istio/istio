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

// Package mesh contains types and functions that are used across the full
// set of mixer commands.
package mesh

import (
	"fmt"
	"io"
	"os"

	"istio.io/pkg/log"
)

const (
	// logToFile controls whether to log to logFilePath
	logToFile   = false
	logFilePath = "./.mesh-cli.log"
)

var (
	// HubValueFromBuild can be set to override the hub value in the base profile. It's applied after reading base
	// profile from compiled in or file-based profiles, but before the user overlay or --set flag.
	HubValueFromBuild = ""
	// TagValueFromBuild can be set to override the tag value in the base profile. It's applied after reading base
	// profile from compiled in or file-based profiles, but before the user overlay or --set flag.
	TagValueFromBuild = ""
)

func initLogsOrExit(args *rootArgs) {
	if logToFile {
		// Only the logs for the last command are of interest.
		// Remove any previous log to avoid indefinite accumulation.
		_ = os.Remove(logFilePath)
	}
	if err := configLogs(args.logToStdErr); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Could not configure logs: %s", err)
		os.Exit(1)
	}
}

func configLogs(logToStdErr bool) error {
	opt := log.DefaultOptions()
	if !logToStdErr && logToFile {
		opt.ErrorOutputPaths = []string{logFilePath}
		opt.OutputPaths = []string{logFilePath}
	}
	return log.Configure(opt)
}

type logger struct {
	logToStdErr bool
	stdOut      io.Writer
	stdErr      io.Writer
}

func newLogger(logToStdErr bool, stdOut, stdErr io.Writer) *logger {
	return &logger{
		logToStdErr: logToStdErr,
		stdOut:      stdOut,
		stdErr:      stdErr,
	}
}

// TODO: this really doesn't belong here. Figure out if it's generally needed and possibly move to istio.io/pkg/log.
func (l *logger) logAndPrint(v ...interface{}) {
	if len(v) == 0 {
		return
	}
	s := fmt.Sprint(v...)
	if !l.logToStdErr {
		l.print(s)
		l.print("\n")
	}
	log.Infof(s)
}

func (l *logger) logAndFatal(v ...interface{}) {
	l.logAndPrint(v...)
	os.Exit(-1)
}

func (l *logger) logAndPrintf(format string, a ...interface{}) {
	s := fmt.Sprintf(format, a...)
	if !l.logToStdErr {
		l.print(s)
		l.print("\n")
	}
	log.Infof(s)
}

func (l *logger) logAndFatalf(format string, a ...interface{}) {
	l.logAndPrintf(format, a...)
	os.Exit(-1)
}

func (l *logger) print(s string) {
	_, _ = l.stdOut.Write([]byte(s))
}

func refreshGoldenFiles() bool {
	return os.Getenv("REFRESH_GOLDEN") == "true"
}
