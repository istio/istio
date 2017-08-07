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

// Package stdioLogger provides an implementation of Mixer's logger aspect that
// writes logs (serialized as JSON) to a standard stream (stdout | stderr).
package stdioLogger

import (
	"fmt"

	multierror "github.com/hashicorp/go-multierror"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"istio.io/mixer/adapter/stdioLogger/config"
	"istio.io/mixer/pkg/adapter"
)

type (
	builder struct{ adapter.DefaultBuilder }
	logger  struct {
		omitEmpty   bool
		outputPaths []string
		impl        *zap.Logger
	}
	zapBuilderFn func(outputPaths ...string) (*zap.Logger, error)
)

const (
	stdErr = "stderr"
	stdOut = "stdout"
)

var (
	zapConfig = zapcore.EncoderConfig{
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeTime:     zapcore.EpochTimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
	}
)

// Register records the builders exposed by this adapter.
func Register(r adapter.Registrar) {
	b := builder{adapter.NewDefaultBuilder(
		"stdioLogger",
		"Writes log entries to a standard I/O stream",
		&config.Params{},
	)}

	r.RegisterApplicationLogsBuilder(b)
	r.RegisterAccessLogsBuilder(b)
}

func (builder) NewApplicationLogsAspect(_ adapter.Env, cfg adapter.Config) (adapter.ApplicationLogsAspect, error) {
	return newLogger(cfg, newZapLogger)
}

func (builder) NewAccessLogsAspect(_ adapter.Env, cfg adapter.Config) (adapter.AccessLogsAspect, error) {
	return newLogger(cfg, newZapLogger)
}

func newLogger(cfg adapter.Config, buildZap zapBuilderFn) (*logger, error) {
	c := cfg.(*config.Params)

	outputPath := stdErr
	if c.LogStream == config.STDOUT {
		outputPath = stdOut
	}

	zapLogger, err := buildZap(outputPath)
	if err != nil {
		return nil, fmt.Errorf("could not build logger: %v", err)
	}

	return &logger{omitEmpty: c.OmitEmptyFields, outputPaths: []string{outputPath}, impl: zapLogger}, nil
}

func (l *logger) Log(entries []adapter.LogEntry) error {
	return l.log(entries)
}

func (l *logger) LogAccess(entries []adapter.LogEntry) error {
	return l.log(entries)
}

func (l *logger) log(entries []adapter.LogEntry) error {
	var errors *multierror.Error
	fields := make([]zapcore.Field, 0, 6)
	includeEmpty := !l.omitEmpty
	for _, entry := range entries {
		if includeEmpty || entry.LogName != "" {
			fields = append(fields, zap.String("logName", entry.LogName))
		}
		if includeEmpty || entry.Timestamp != "" {
			fields = append(fields, zap.String("timestamp", entry.Timestamp))
		}
		if includeEmpty || entry.Severity != adapter.Default {
			fields = append(fields, zap.String("severity", entry.Severity.String()))
		}
		if includeEmpty || len(entry.Labels) > 0 {
			fields = append(fields, zap.Any("labels", entry.Labels))
		}
		if includeEmpty || len(entry.TextPayload) > 0 {
			fields = append(fields, zap.String("textPayload", entry.TextPayload))
		}
		if includeEmpty || len(entry.StructPayload) > 0 {
			fields = append(fields, zap.Any("structPayload", entry.StructPayload))
		}
		if err := l.impl.Core().Write(zapcore.Entry{}, fields); err != nil {
			errors = multierror.Append(errors, err)
		}
		fields = fields[:0]
	}
	return errors.ErrorOrNil()
}

func (l *logger) Close() error { return nil }

func newZapLogger(outputPaths ...string) (*zap.Logger, error) {
	prodConfig := zap.NewProductionConfig()
	prodConfig.DisableCaller = true
	prodConfig.DisableStacktrace = true
	prodConfig.EncoderConfig = zapConfig
	prodConfig.OutputPaths = outputPaths
	zapLogger, err := prodConfig.Build()
	return zapLogger, err
}
