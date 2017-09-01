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

package stdio // import "istio.io/mixer/adapter/stdio"

import (
	"context"
	"fmt"
	"sort"
	"time"

	multierror "github.com/hashicorp/go-multierror"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"istio.io/mixer/adapter/stdio/config"
	"istio.io/mixer/pkg/adapter"
	pkgHndlr "istio.io/mixer/pkg/handler"
	"istio.io/mixer/template/logentry"
	"istio.io/mixer/template/metric"
)

type (
	zapBuilderFn func(outputPath string, encoding string) (*zap.Logger, error)
	getTimeFn    func() time.Time

	handler struct {
		logger         *zap.Logger
		severityLevels map[string]zapcore.Level
		metricLevel    zapcore.Level
		getTime        getTimeFn
		logEntryVars   map[string][]string
		metricDims     map[string][]string
	}
)

func (h *handler) HandleLogEntry(_ context.Context, instances []*logentry.Instance) error {
	var errors *multierror.Error

	fields := make([]zapcore.Field, 0, 6)
	for _, instance := range instances {
		entry := zapcore.Entry{
			LoggerName: instance.Name,
			Level:      h.mapSeverityLevel(instance.Severity),
			Time:       instance.Timestamp,
		}

		for _, varName := range h.logEntryVars[instance.Name] {
			if value, ok := instance.Variables[varName]; ok {
				fields = append(fields, zap.Any(varName, value))
			}
		}

		if err := h.logger.Core().Write(entry, fields); err != nil {
			errors = multierror.Append(errors, err)
		}
		fields = fields[:0]
	}

	return errors.ErrorOrNil()
}

func (h *handler) HandleMetric(_ context.Context, instances []*metric.Instance) error {
	var errors *multierror.Error

	fields := make([]zapcore.Field, 0, 6)
	for _, instance := range instances {
		entry := zapcore.Entry{
			LoggerName: instance.Name,
			Level:      h.metricLevel,
			Time:       h.getTime(),
		}

		fields = append(fields, zap.Any("value", instance.Value))
		for _, varName := range h.metricDims[instance.Name] {
			value := instance.Dimensions[varName]
			fields = append(fields, zap.Any(varName, value))
		}

		if err := h.logger.Core().Write(entry, fields); err != nil {
			errors = multierror.Append(errors, err)
		}
		fields = fields[:0]
	}

	return errors.ErrorOrNil()
}

func (h *handler) Close() error { return nil }

func (h *handler) mapSeverityLevel(severity string) zapcore.Level {
	level, ok := h.severityLevels[severity]
	if !ok {
		level = zap.InfoLevel
	}

	return level
}

////////////////// Config //////////////////////////

// GetInfo returns the Info associated with this adapter implementation.
func GetInfo() pkgHndlr.Info {
	return pkgHndlr.Info{
		Name:        "istio.io/mixer/adapter/stdio",
		Description: "Writes logs and metrics to a standard I/O stream",
		SupportedTemplates: []string{
			logentry.TemplateName,
			metric.TemplateName,
		},
		DefaultConfig: &config.Params{
			LogStream:    config.STDOUT,
			MetricLevel:  config.INFO,
			OutputAsJson: false,
		},

		// TO BE DELETED
		CreateHandlerBuilder: func() adapter.HandlerBuilder { return &builder{} },
		ValidateConfig: func(cfg adapter.Config) *adapter.ConfigErrors {
			return validateConfig(&pkgHndlr.HandlerConfig{AdapterConfig: cfg})
		},

		ValidateConfig2: validateConfig,
		NewHandler:      newHandler,
	}
}

func validateConfig(*pkgHndlr.HandlerConfig) (ce *adapter.ConfigErrors) {
	return
}

func newHandler(ctx context.Context, env adapter.Env, hc *pkgHndlr.HandlerConfig) (adapter.Handler, error) {
	return newHandlerWithZapBuilder(ctx, env, hc, newZapLogger)
}

func newHandlerWithZapBuilder(_ context.Context, _ adapter.Env, hc *pkgHndlr.HandlerConfig, zb zapBuilderFn) (adapter.Handler, error) {
	// We produce sorted tables of the variables we'll receive such that
	// we send output to the zap logger in a consistent order at runtime
	varLists := make(map[string][]string, len(hc.LogEntryTypes))
	for tn, tv := range hc.LogEntryTypes {
		l := make([]string, 0, len(tv.Variables))
		for v := range tv.Variables {
			l = append(l, v)
		}

		sort.Strings(l)
		varLists[tn] = l
	}

	// We produce sorted tables of the dimensions we'll receive such that
	// we send output to the zap logger in a consistent order at runtime
	dimLists := make(map[string][]string, len(hc.MetricEntryTypes))
	for tn, tv := range hc.MetricEntryTypes {
		l := make([]string, 0, len(tv.Dimensions))
		for v := range tv.Dimensions {
			l = append(l, v)
		}

		sort.Strings(l)
		dimLists[tn] = l
	}

	ac := hc.AdapterConfig.(*config.Params)

	outputPath := "stdout"
	if ac.LogStream == config.STDERR {
		outputPath = "stderr"
	}

	encoding := "console"
	if ac.OutputAsJson {
		encoding = "json"
	}

	zapLogger, err := zb(outputPath, encoding)
	if err != nil {
		return nil, fmt.Errorf("could not build logger: %v", err)
	}

	sl := make(map[string]zapcore.Level)
	for k, v := range ac.SeverityLevels {
		sl[k] = mapConfigLevel(v)
	}

	return &handler{
		severityLevels: sl,
		metricLevel:    mapConfigLevel(ac.MetricLevel),
		logger:         zapLogger,
		getTime:        time.Now,
		logEntryVars:   varLists,
		metricDims:     dimLists,
	}, nil
}

func mapConfigLevel(l config.Params_Level) zapcore.Level {
	if l == config.WARNING {
		return zapcore.WarnLevel
	} else if l == config.ERROR {
		return zapcore.ErrorLevel
	}
	return zapcore.InfoLevel
}

func newZapEncoderConfig() zapcore.EncoderConfig {
	encConfig := zap.NewProductionEncoderConfig()
	encConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encConfig.EncodeDuration = zapcore.StringDurationEncoder
	encConfig.MessageKey = ""
	encConfig.NameKey = "instance"

	return encConfig
}

func newZapLogger(outputPath string, encoding string) (*zap.Logger, error) {
	zapConfig := zap.NewProductionConfig()
	zapConfig.DisableCaller = true
	zapConfig.DisableStacktrace = true
	zapConfig.OutputPaths = []string{outputPath}
	zapConfig.EncoderConfig = newZapEncoderConfig()
	zapConfig.Encoding = encoding

	return zapConfig.Build()
}

// EVERYTHING BELOW IS TO BE DELETED

type builder struct {
	MetricTypes   map[string]*metric.Type
	LogEntryTypes map[string]*logentry.Type
}

// Build is to be deleted
func (b *builder) Build(cfg adapter.Config, env adapter.Env) (adapter.Handler, error) {
	hc := &pkgHndlr.HandlerConfig{
		AdapterConfig: cfg,
	}
	return newHandler(context.Background(), env, hc)
}

// ConfigureLogEntryHandler is to be deleted
func (b *builder) ConfigureLogEntryHandler(types map[string]*logentry.Type) error {
	b.LogEntryTypes = types
	return nil
}

// ConfigureMetricHandler is to be deleted
func (b *builder) ConfigureMetricHandler(types map[string]*metric.Type) error {
	b.MetricTypes = types
	return nil
}
