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

package stdio // import "istio.io/istio/mixer/adapter/stdio"

import (
	"context"
	"fmt"
	"net"
	"sort"
	"time"

	multierror "github.com/hashicorp/go-multierror"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"istio.io/istio/mixer/adapter/stdio/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/logentry"
	"istio.io/istio/mixer/template/metric"
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
				fields = append(fields, field(varName, value))
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
			fields = append(fields, field(varName, value))
		}

		if err := h.logger.Core().Write(entry, fields); err != nil {
			errors = multierror.Append(errors, err)
		}
		fields = fields[:0]
	}

	return errors.ErrorOrNil()
}

func field(varName string, value interface{}) zapcore.Field {
	// TODO: remove when IP_ADDRESS is properly handled by Mixer
	switch value.(type) {
	case []byte:
		b := value.([]byte)
		if len(b) == net.IPv4len || len(b) == net.IPv6len {
			return zap.Any(varName, net.IP(b))
		}
		return zap.Any(varName, value)
	default:
		return zap.Any(varName, value)
	}
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
func GetInfo() adapter.Info {
	return adapter.Info{
		Name:        "stdio",
		Impl:        "istio.io/istio/mixer/adapter/stdio",
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

		NewBuilder: func() adapter.HandlerBuilder { return &builder{} },
	}
}

type builder struct {
	adapterConfig *config.Params
	logEntryTypes map[string]*logentry.Type
	metricTypes   map[string]*metric.Type
}

func (b *builder) SetLogEntryTypes(types map[string]*logentry.Type) { b.logEntryTypes = types }
func (b *builder) SetMetricTypes(types map[string]*metric.Type)     { b.metricTypes = types }
func (b *builder) SetAdapterConfig(cfg adapter.Config)              { b.adapterConfig = cfg.(*config.Params) }
func (*builder) Validate() (ce *adapter.ConfigErrors)               { return }

func (b *builder) Build(context context.Context, env adapter.Env) (adapter.Handler, error) {
	return b.buildWithZapBuilder(context, env, newZapLogger)
}

func (b *builder) buildWithZapBuilder(_ context.Context, _ adapter.Env, zb zapBuilderFn) (adapter.Handler, error) {
	// We produce sorted tables of the variables we'll receive such that
	// we send output to the zap logger in a consistent order at runtime
	varLists := make(map[string][]string, len(b.logEntryTypes))
	for tn, tv := range b.logEntryTypes {
		l := make([]string, 0, len(tv.Variables))
		for v := range tv.Variables {
			l = append(l, v)
		}

		sort.Strings(l)
		varLists[tn] = l
	}

	// We produce sorted tables of the dimensions we'll receive such that
	// we send output to the zap logger in a consistent order at runtime
	dimLists := make(map[string][]string, len(b.metricTypes))
	for tn, tv := range b.metricTypes {
		l := make([]string, 0, len(tv.Dimensions))
		for v := range tv.Dimensions {
			l = append(l, v)
		}

		sort.Strings(l)
		dimLists[tn] = l
	}

	ac := b.adapterConfig

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
