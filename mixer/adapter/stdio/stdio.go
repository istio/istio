// Copyright Istio Authors
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

// nolint: lll
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -a mixer/adapter/stdio/config/config.proto -x "-n stdio -t logentry -t metric -d example"

// Package stdio provides an adapter that implements the logEntry and metrics
// templates to serialize generated logs and metrics to stdout, stderr, or files.
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

	istio_policy_v1beta1 "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/adapter/metadata"
	"istio.io/istio/mixer/adapter/stdio/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/logentry"
	"istio.io/istio/mixer/template/metric"
)

type (
	zapBuilderFn func(options *config.Params) (zapcore.Core, func(), error)

	handler struct {
		closer         func()
		severityLevels map[string]zapcore.Level
		metricLevel    zapcore.Level
		getTime        func() time.Time
		write          func(entry zapcore.Entry, fields []zapcore.Field) error
		sync           func() error
		logEntryVars   map[string][]string
		metricDims     map[string][]string
		logEntryTypes  map[string]*logentry.Type
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

		var logEntryTypes map[string]istio_policy_v1beta1.ValueType
		if typeInfo, found := h.logEntryTypes[instance.Name]; found {
			logEntryTypes = typeInfo.Variables
		}
		for _, varName := range h.logEntryVars[instance.Name] {
			if value, ok := instance.Variables[varName]; ok {
				fields = append(fields, zap.Any(varName, convertValueTypes(value, varName, logEntryTypes)))
			}
		}

		if err := h.write(entry, fields); err != nil {
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

		if err := h.write(entry, fields); err != nil {
			errors = multierror.Append(errors, err)
		}
		fields = fields[:0]
	}

	return errors.ErrorOrNil()
}

func (h *handler) Close() error {
	_ = h.sync()
	h.closer()
	return nil
}

func (h *handler) mapSeverityLevel(severity string) zapcore.Level {
	level, ok := h.severityLevels[severity]
	if !ok {
		level = zap.InfoLevel
	}

	return level
}

func convertValueTypes(value interface{}, varName string, logEntryTypes map[string]istio_policy_v1beta1.ValueType) interface{} {
	if logEntryTypes[varName] == istio_policy_v1beta1.IP_ADDRESS {
		if byteArr, ok := value.([]byte); ok {
			return interface{}(net.IP(byteArr).String())
		}
	}

	return value
}

////////////////// Config //////////////////////////

// GetInfo returns the Info associated with this adapter implementation.
func GetInfo() adapter.Info {
	info := metadata.GetInfo("stdio")
	info.NewBuilder = func() adapter.HandlerBuilder { return &builder{} }
	return info
}

type builder struct {
	adapterConfig *config.Params
	logEntryTypes map[string]*logentry.Type
	metricTypes   map[string]*metric.Type
}

func (b *builder) SetLogEntryTypes(types map[string]*logentry.Type) { b.logEntryTypes = types }
func (b *builder) SetMetricTypes(types map[string]*metric.Type)     { b.metricTypes = types }
func (b *builder) SetAdapterConfig(cfg adapter.Config)              { b.adapterConfig = cfg.(*config.Params) }

func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	if b.adapterConfig.LogStream == config.STDERR || b.adapterConfig.LogStream == config.STDOUT {
		if b.adapterConfig.OutputPath != "" {
			ce = ce.Appendf("outputPath", "cannot specify an output path when using a STDOUT or STDERR log stream")
		}
	} else {
		if b.adapterConfig.OutputPath == "" {
			ce = ce.Appendf("outputPath", "need a valid output path when using a FILE or ROTATED_FILE log stream")
		}
	}

	return
}

func (b *builder) Build(context context.Context, env adapter.Env) (adapter.Handler, error) {
	return b.buildWithZapBuilder(context, env, newZapCore)
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

	core, closer, err := zb(b.adapterConfig)
	if err != nil {
		return nil, fmt.Errorf("could not build logger: %v", err)
	}

	sl := make(map[string]zapcore.Level)
	for k, v := range b.adapterConfig.SeverityLevels {
		sl[k] = levelToZap[v]
	}

	return &handler{
		severityLevels: sl,
		metricLevel:    levelToZap[b.adapterConfig.MetricLevel],
		closer:         closer,
		getTime:        time.Now,
		sync:           core.Sync,
		write:          core.Write,
		logEntryVars:   varLists,
		metricDims:     dimLists,
		logEntryTypes:  b.logEntryTypes,
	}, nil
}
