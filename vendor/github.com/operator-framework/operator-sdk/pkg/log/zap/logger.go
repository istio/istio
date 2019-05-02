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
	"io"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

func Logger() logr.Logger {
	return LoggerTo(os.Stderr)
}

func LoggerTo(destWriter io.Writer) logr.Logger {
	syncer := zapcore.AddSync(destWriter)
	conf := getConfig()

	conf.encoder = &logf.KubeAwareEncoder{Encoder: conf.encoder, Verbose: conf.level.Level() < 0}
	if conf.sample {
		conf.opts = append(conf.opts, zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			return zapcore.NewSampler(core, time.Second, 100, 100)
		}))
	}
	conf.opts = append(conf.opts, zap.AddCallerSkip(1), zap.ErrorOutput(syncer))
	log := zap.New(zapcore.NewCore(conf.encoder, syncer, conf.level))
	log = log.WithOptions(conf.opts...)
	return zapr.NewLogger(log)
}

type config struct {
	encoder zapcore.Encoder
	level   zap.AtomicLevel
	sample  bool
	opts    []zap.Option
}

func getConfig() config {
	var c config

	// Set the defaults depending on the log mode (development vs. production)
	if development {
		c.encoder = consoleEncoder()
		c.level = zap.NewAtomicLevelAt(zap.DebugLevel)
		c.opts = append(c.opts, zap.Development(), zap.AddStacktrace(zap.ErrorLevel))
		c.sample = false
	} else {
		c.encoder = jsonEncoder()
		c.level = zap.NewAtomicLevelAt(zap.InfoLevel)
		c.opts = append(c.opts, zap.AddStacktrace(zap.WarnLevel))
		c.sample = true
	}

	// Override the defaults if the flags were set explicitly on the command line
	if encoderVal.set {
		c.encoder = encoderVal.encoder
	}
	if levelVal.set {
		c.level = zap.NewAtomicLevelAt(levelVal.level)
	}
	if sampleVal.set {
		c.sample = sampleVal.sample
	}

	// Disable sampling when we are in debug mode. Otherwise, this will
	// cause index out of bounds errors in the sampling code.
	if c.level.Level() < -1 {
		c.sample = false
	}
	return c
}
