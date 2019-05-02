// Copyright 2019 The Operator-SDK Authors
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

package zap

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	zapFlagSet *pflag.FlagSet

	development bool
	encoderVal  encoderValue
	levelVal    levelValue
	sampleVal   sampleValue
)

func init() {
	zapFlagSet = pflag.NewFlagSet("zap", pflag.ExitOnError)
	zapFlagSet.BoolVar(&development, "zap-devel", false, "Enable zap development mode (changes defaults to console encoder, debug log level, and disables sampling)")
	zapFlagSet.Var(&encoderVal, "zap-encoder", "Zap log encoding ('json' or 'console')")
	zapFlagSet.Var(&levelVal, "zap-level", "Zap log level (one of 'debug', 'info', 'error' or any integer value > 0)")
	zapFlagSet.Var(&sampleVal, "zap-sample", "Enable zap log sampling. Sampling will be disabled for integer log levels > 1")
}

func FlagSet() *pflag.FlagSet {
	return zapFlagSet
}

type encoderValue struct {
	set     bool
	encoder zapcore.Encoder
	str     string
}

func (v *encoderValue) Set(e string) error {
	v.set = true
	switch e {
	case "json":
		v.encoder = jsonEncoder()
	case "console":
		v.encoder = consoleEncoder()
	default:
		return fmt.Errorf("unknown encoder \"%s\"", e)
	}
	v.str = e
	return nil
}

func (v encoderValue) String() string {
	return v.str
}

func (v encoderValue) Type() string {
	return "encoder"
}

func jsonEncoder() zapcore.Encoder {
	encoderConfig := zap.NewProductionEncoderConfig()
	return zapcore.NewJSONEncoder(encoderConfig)
}

func consoleEncoder() zapcore.Encoder {
	encoderConfig := zap.NewDevelopmentEncoderConfig()
	return zapcore.NewConsoleEncoder(encoderConfig)
}

type levelValue struct {
	set   bool
	level zapcore.Level
}

func (v *levelValue) Set(l string) error {
	v.set = true
	lower := strings.ToLower(l)
	var lvl int
	switch lower {
	case "debug":
		lvl = -1
	case "info":
		lvl = 0
	case "error":
		lvl = 2
	default:
		i, err := strconv.Atoi(lower)
		if err != nil {
			return fmt.Errorf("invalid log level \"%s\"", l)
		}

		if i > 0 {
			lvl = -1 * i
		} else {
			return fmt.Errorf("invalid log level \"%s\"", l)
		}
	}
	v.level = zapcore.Level(int8(lvl))
	return nil
}

func (v levelValue) String() string {
	return v.level.String()
}

func (v levelValue) Type() string {
	return "level"
}

type sampleValue struct {
	set    bool
	sample bool
}

func (v *sampleValue) Set(s string) error {
	var err error
	v.set = true
	v.sample, err = strconv.ParseBool(s)
	return err
}

func (v sampleValue) String() string {
	return strconv.FormatBool(v.sample)
}

func (v sampleValue) IsBoolFlag() bool {
	return true
}

func (v sampleValue) Type() string {
	return "sample"
}
