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

package log // import "fortio.org/fortio/log"

import (
	"flag"
	"fmt"
	"io"
	"log"
	"runtime"
	"strings"
)

// Level is the level of logging (0 Debug -> 6 Fatal).
type Level int

// Log levels. Go can't have variable and function of the same name so we keep
// medium length (Dbg,Info,Warn,Err,Crit,Fatal) names for the functions.
const (
	Debug Level = iota
	Verbose
	Info
	Warning
	Error
	Critical
	Fatal
)

var (
	level       = Info // default is Info and up
	levelToStrA []string
	levelToStrM map[string]Level
	// LogPrefix is a prefix to include in each log line.
	LogPrefix = flag.String("logprefix", "> ", "Prefix to log lines before logged messages")
	// LogFileAndLine determines if the log lines will contain caller file name and line number.
	LogFileAndLine = flag.Bool("logcaller", true, "Logs filename and line number of callers to log")
)

func init() {
	levelToStrA = []string{
		"Debug",
		"Verbose",
		"Info",
		"Warning",
		"Error",
		"Critical",
		"Fatal",
	}
	levelToStrM = make(map[string]Level, 2*len(levelToStrA))
	for l, name := range levelToStrA {
		// Allow both -loglevel Verbose and -loglevel verbose ...
		levelToStrM[name] = Level(l)
		levelToStrM[strings.ToLower(name)] = Level(l)
	}
	flag.Var(&level, "loglevel", fmt.Sprintf("loglevel, one of %v", levelToStrA))
	log.SetFlags(log.Ltime)
}

// String returns the string representation of the level.
// Needed for flag Var interface.
func (l *Level) String() string {
	return (*l).ToString()
}

// ToString returns the string representation of the level.
// (this can't be the same name as the pointer receiver version)
func (l Level) ToString() string {
	return levelToStrA[l]
}

// Set is called by the flags.
func (l *Level) Set(str string) error {
	var lvl Level
	var ok bool
	if lvl, ok = levelToStrM[str]; !ok {
		// flag processing already logs the value
		return fmt.Errorf("should be one of %v", levelToStrA)
	}
	SetLogLevel(lvl)
	return nil
}

// SetLogLevel sets the log level and returns the previous one.
func SetLogLevel(lvl Level) Level {
	return setLogLevel(lvl, true)
}

// SetLogLevelQuiet sets the log level and returns the previous one but does
// not log the change of level itself.
func SetLogLevelQuiet(lvl Level) Level {
	return setLogLevel(lvl, false)
}

// setLogLevel sets the log level and returns the previous one.
// if logChange is true the level change is logged.
func setLogLevel(lvl Level, logChange bool) Level {
	prev := level
	if lvl < Debug {
		log.Printf("SetLogLevel called with level %d lower than Debug!", lvl)
		return -1
	}
	if lvl > Critical {
		log.Printf("SetLogLevel called with level %d higher than Critical!", lvl)
		return -1
	}
	if lvl != prev {
		if logChange {
			logPrintf(Info, "Log level is now %d %s (was %d %s)\n", lvl, lvl.ToString(), prev, prev.ToString())
		}
		level = lvl
	}
	return prev
}

// GetLogLevel returns the currently configured LogLevel.
func GetLogLevel() Level {
	return level
}

// Log returns true if a given level is currently logged.
func Log(lvl Level) bool {
	return lvl >= level
}

// LevelByName returns the LogLevel by its name.
func LevelByName(str string) Level {
	return levelToStrM[str]
}

// Logf logs with format at the given level.
// 2 level of calls so it's always same depth for extracting caller file/line
func Logf(lvl Level, format string, rest ...interface{}) {
	logPrintf(lvl, format, rest...)
}

func logPrintf(lvl Level, format string, rest ...interface{}) {
	if !Log(lvl) {
		return
	}
	if *LogFileAndLine {
		_, file, line, _ := runtime.Caller(2)
		file = file[strings.LastIndex(file, "/")+1:]
		log.Print(levelToStrA[lvl][0:1], " ", file, ":", line, *LogPrefix, fmt.Sprintf(format, rest...))
	} else {
		log.Print(levelToStrA[lvl][0:1], " ", *LogPrefix, fmt.Sprintf(format, rest...))
	}
	if lvl == Fatal {
		panic("aborting...")
	}
}

// SetOutput sets the output to a different writer (forwards to system logger).
func SetOutput(w io.Writer) {
	log.SetOutput(w)
}

// SetFlags forwards flags to the system logger.
func SetFlags(f int) {
	log.SetFlags(f)
}

// -- would be nice to be able to create those in a loop instead of copypasta:

// Debugf logs if Debug level is on.
func Debugf(format string, rest ...interface{}) {
	logPrintf(Debug, format, rest...)
}

// LogVf logs if Verbose level is on.
func LogVf(format string, rest ...interface{}) { //nolint: golint
	logPrintf(Verbose, format, rest...)
}

// Infof logs if Info level is on.
func Infof(format string, rest ...interface{}) {
	logPrintf(Info, format, rest...)
}

// Warnf logs if Warning level is on.
func Warnf(format string, rest ...interface{}) {
	logPrintf(Warning, format, rest...)
}

// Errf logs if Warning level is on.
func Errf(format string, rest ...interface{}) {
	logPrintf(Error, format, rest...)
}

// Critf logs if Warning level is on.
func Critf(format string, rest ...interface{}) {
	logPrintf(Critical, format, rest...)
}

// Fatalf logs if Warning level is on.
func Fatalf(format string, rest ...interface{}) {
	logPrintf(Fatal, format, rest...)
}

// LogDebug shortcut for fortio.Log(fortio.Debug)
func LogDebug() bool { //nolint: golint
	return Log(Debug)
}

// LogVerbose shortcut for fortio.Log(fortio.Verbose)
func LogVerbose() bool { //nolint: golint
	return Log(Verbose)
}
