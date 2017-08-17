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

package fortio

import (
	"flag"
	"fmt"
	"log"
	"runtime"
	"strings"
)

// LogLevel is the level of logging (0 Debug -> 6 Fatal).
type LogLevel int

// Log levels. Go can't have variable and function of the same name so we keep
// medium length (Dbg,Info,Warn,Err,Crit,Fatal) names for the functions.
const (
	Debug LogLevel = iota
	Verbose
	Info
	Warning
	Error
	Critical
	Fatal
)

var (
	level          = Info // default is Info and up
	levelToStrA    []string
	levelToStrM    map[string]LogLevel
	logPrefix      = flag.String("logprefix", "> ", "Prefix to log lines before logged messages")
	logFileAndLine = flag.Bool("logcaller", true, "Logs filename and line number of callers to log")
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
	levelToStrM = make(map[string]LogLevel, 2*len(levelToStrA))
	for l, name := range levelToStrA {
		// Allow both -loglevel Verbose and -loglevel verbose ...
		levelToStrM[name] = LogLevel(l)
		levelToStrM[strings.ToLower(name)] = LogLevel(l)
	}
	flag.Var(&level, "loglevel", fmt.Sprintf("loglevel, one of %v", levelToStrA))
	log.SetFlags(log.Ltime)
}

// String returns the string representation of the level.
// Needed for flag Var interface.
func (l *LogLevel) String() string {
	return (*l).ToString()
}

// ToString returns the string representation of the level.
// (this can't be the same name as the pointer receiver version)
func (l LogLevel) ToString() string {
	return levelToStrA[l]
}

// Set is called by the flags.
func (l *LogLevel) Set(str string) error {
	var lvl LogLevel
	var ok bool
	if lvl, ok = levelToStrM[str]; !ok {
		// flag processing already logs the value
		return fmt.Errorf("should be one of %v", levelToStrA)
	}
	SetLogLevel(lvl)
	return nil
}

// SetLogLevel sets the log level and returns the previous one.
func SetLogLevel(lvl LogLevel) LogLevel {
	prev := level
	if lvl < Debug {
		log.Printf("SetLogLevel called with level %d lower than Debug!", lvl)
		return -1
	}
	if lvl > Critical {
		log.Printf("SetLogLevel called with level %d higher than Critical!", lvl)
		return -1
	}
	logPrintf(Info, "Log level is now %d %s (was %d %s)\n", lvl, lvl.ToString(), prev, prev.ToString())
	level = lvl
	return prev
}

// GetLogLevel returns the currently configured LogLevel.
func GetLogLevel() LogLevel {
	return level
}

// Log returns true if a given level is currently logged.
func Log(lvl LogLevel) bool {
	return lvl >= level
}

// LogLevelByName returns the LogLevel by its name.
func LogLevelByName(str string) LogLevel {
	return levelToStrM[str]
}

// Logf logs with format at the given level.
// 2 level of calls so it's always same depth for extracting caller file/line
func Logf(lvl LogLevel, format string, rest ...interface{}) {
	logPrintf(lvl, format, rest...)
}

func logPrintf(lvl LogLevel, format string, rest ...interface{}) {
	if !Log(lvl) {
		return
	}
	if *logFileAndLine {
		_, file, line, _ := runtime.Caller(2)
		file = file[strings.LastIndex(file, "/")+1:]
		log.Print(levelToStrA[lvl][0:1], " ", file, ":", line, *logPrefix, fmt.Sprintf(format, rest...))
	} else {
		log.Print(levelToStrA[lvl][0:1], " ", *logPrefix, fmt.Sprintf(format, rest...))
	}
	if lvl == Fatal {
		panic("aborting...")
	}
}

// -- would be nice to be able to create those in a loop instead of copypasta:

// Debugf logs if Debug level is on.
func Debugf(format string, rest ...interface{}) {
	logPrintf(Debug, format, rest...)
}

// LogVf logs if Verbose level is on.
func LogVf(format string, rest ...interface{}) {
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
func LogDebug() bool {
	return Log(Debug)
}

// LogVerbose shortcut for fortio.Log(fortio.Verbose)
func LogVerbose() bool {
	return Log(Verbose)
}
