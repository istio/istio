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

// Package glog exposes an API subset of the [glog](https://github.com/golang/glog) package.
// All logging state delivered to this package is shunted to the global [zap logger](https://github.com/uber-go/zap).
//
// Istio is built on top of zap logger. We depend on some downstream components that use glog for logging.
// This package makes it so we can intercept the calls to glog and redirect them to zap and thus produce
// a consistent log for our processes.
package glog

import (
	"fmt"
	"os"

	"go.uber.org/zap"
)

// Level is a shim
type Level int32

// Verbose is a shim
type Verbose bool

// Flush is a shim
func Flush() {
	zap.L().Sync()
}

// V is a shim
func V(level Level) Verbose {
	return Verbose(zap.L().Core().Enabled(zap.DebugLevel))
}

// Info is a shim
func (v Verbose) Info(args ...interface{}) {
	zap.S().Debug(args...)
}

// Infoln is a shim
func (v Verbose) Infoln(args ...interface{}) {
	s := fmt.Sprint(args)
	zap.S().Debug(s, "\n")
}

// Infof is a shim
func (v Verbose) Infof(format string, args ...interface{}) {
	zap.S().Debugf(format, args...)
}

// Info is a shim
func Info(args ...interface{}) {
	zap.S().Info(args...)
}

// InfoDepth is a shim
func InfoDepth(depth int, args ...interface{}) {
	zap.S().Info(args...)
}

// Infoln is a shim
func Infoln(args ...interface{}) {
	s := fmt.Sprint(args)
	zap.S().Info(s, "\n")
}

// Infof is a shim
func Infof(format string, args ...interface{}) {
	zap.S().Infof(format, args...)
}

// Warning is a shim
func Warning(args ...interface{}) {
	zap.S().Warn(args...)
}

// WarningDepth is a shim
func WarningDepth(depth int, args ...interface{}) {
	zap.S().Warn(args...)
}

// Warningln is a shim
func Warningln(args ...interface{}) {
	s := fmt.Sprint(args)
	zap.S().Warn(s, "\n")
}

// Warningf is a shim
func Warningf(format string, args ...interface{}) {
	zap.S().Warnf(format, args...)
}

// Error is a shim
func Error(args ...interface{}) {
	zap.S().Error(args...)
}

// ErrorDepth is a shim
func ErrorDepth(depth int, args ...interface{}) {
	zap.S().Error(args...)
}

// Errorln is a shim
func Errorln(args ...interface{}) {
	s := fmt.Sprint(args)
	zap.S().Error(s, "\n")
}

// Errorf is a shim
func Errorf(format string, args ...interface{}) {
	zap.S().Errorf(format, args...)
}

// Fatal is a shim
func Fatal(args ...interface{}) {
	zap.S().Error(args...)
	os.Exit(255)
}

// FatalDepth is a shim
func FatalDepth(depth int, args ...interface{}) {
	zap.S().Error(args...)
	os.Exit(255)
}

// Fatalln is a shim
func Fatalln(args ...interface{}) {
	s := fmt.Sprint(args)
	zap.S().Error(s, "\n")
	os.Exit(255)
}

// Fatalf is a shim
func Fatalf(format string, args ...interface{}) {
	zap.S().Errorf(format, args...)
	os.Exit(255)
}

// Exit is a shim
func Exit(args ...interface{}) {
	zap.S().Error(args...)
	os.Exit(1)
}

// ExitDepth is a shim
func ExitDepth(depth int, args ...interface{}) {
	zap.S().Error(args...)
	os.Exit(1)
}

// Exitln is a shim
func Exitln(args ...interface{}) {
	s := fmt.Sprint(args)
	zap.S().Error(s, "\n")
	os.Exit(1)
}

// Exitf is a shim
func Exitf(format string, args ...interface{}) {
	zap.S().Errorf(format, args...)
	os.Exit(1)
}
