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
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -a mixer/adapter/cloudwatch/config/config.proto -x "-n cloudwatch -t metric -d example"

package cloudwatch

import (
	"context"
	"fmt"
	"html/template"
	"strings"

	"github.com/aws/aws-sdk-go/service/cloudwatch/cloudwatchiface"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs/cloudwatchlogsiface"

	istio_policy_v1beta1 "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/adapter/cloudwatch/config"
	"istio.io/istio/mixer/adapter/metadata"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/logentry"
	"istio.io/istio/mixer/template/metric"
)

const (
	// CloudWatch enforced limit
	// https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/cloudwatch_limits.html
	dimensionLimit = 10
)

var supportedValueTypes = map[string]bool{
	"STRING":   true,
	"INT64":    true,
	"DOUBLE":   true,
	"DURATION": true,
}

var supportedDurationUnits = map[config.Params_MetricDatum_Unit]bool{
	config.Seconds:      true,
	config.Microseconds: true,
	config.Milliseconds: true,
}

type (
	builder struct {
		adpCfg        *config.Params
		metricTypes   map[string]*metric.Type
		logEntryTypes map[string]*logentry.Type
	}

	// handler holds data for the cloudwatch adapter handler
	handler struct {
		metricTypes       map[string]*metric.Type
		logEntryTypes     map[string]*logentry.Type
		logEntryTemplates map[string]*template.Template
		env               adapter.Env
		cfg               *config.Params
		cloudwatch        cloudwatchiface.CloudWatchAPI
		cloudwatchlogs    cloudwatchlogsiface.CloudWatchLogsAPI
	}
)

// ensure types implement the requisite interfaces
var (
	_ metric.HandlerBuilder   = &builder{}
	_ metric.Handler          = &handler{}
	_ logentry.HandlerBuilder = &builder{}
	_ logentry.Handler        = &handler{}
)

///////////////// Configuration-time Methods ///////////////

// newHandler initializes both cloudwatch and cloudwatchlogs handler
func newHandler(metricTypes map[string]*metric.Type, logEntryTypes map[string]*logentry.Type, logEntryTemplates map[string]*template.Template,
	env adapter.Env, cfg *config.Params, cloudwatch cloudwatchiface.CloudWatchAPI, cloudwatchlogs cloudwatchlogsiface.CloudWatchLogsAPI) adapter.Handler {
	return &handler{metricTypes: metricTypes, logEntryTypes: logEntryTypes, logEntryTemplates: logEntryTemplates, env: env, cfg: cfg,
		cloudwatch: cloudwatch, cloudwatchlogs: cloudwatchlogs}
}

// adapter.HandlerBuilder#Build
func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	cloudwatch := newCloudWatchClient()
	cloudwatchlogs := newCloudWatchLogsClient()
	templates := make(map[string]*template.Template)
	for name, l := range b.adpCfg.GetLogs() {
		if strings.TrimSpace(l.PayloadTemplate) == "" {
			l.PayloadTemplate = defaultTemplate
		}
		tmpl, err := template.New(name).Parse(l.PayloadTemplate)
		if err == nil {
			templates[name] = tmpl
		}
	}
	return newHandler(b.metricTypes, b.logEntryTypes, templates, env, b.adpCfg, cloudwatch, cloudwatchlogs), nil
}

// adapter.HandlerBuilder#SetAdapterConfig
func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.adpCfg = cfg.(*config.Params)
}

// adapter.HandlerBuilder#Validate
func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	if len(b.adpCfg.GetNamespace()) == 0 {
		ce = ce.Append("namespace", fmt.Errorf("namespace should not be empty"))
	}

	if len(b.adpCfg.GetMetricInfo()) != len(b.metricTypes) {
		ce = ce.Append("metricInfo", fmt.Errorf("metricInfo and instance config must contain the same metrics"))
	}

	for k, v := range b.metricTypes {
		// validate handler config contains required metric config
		if _, ok := b.adpCfg.GetMetricInfo()[k]; !ok {
			ce = ce.Append("metricInfo", fmt.Errorf("metricInfo and instance config must contain the same metrics but missing %v", k))
		}

		// validate that value type can be handled by the CloudWatch handler
		if _, ok := supportedValueTypes[v.Value.String()]; !ok {
			ce = ce.Append("value type", fmt.Errorf("value of type %v is not supported", v.Value))
		}

		// validate that if the value is a duration it has the correct cloudwatch unit
		if v.Value == istio_policy_v1beta1.DURATION {
			unit := b.adpCfg.GetMetricInfo()[k].GetUnit()
			if _, ok := supportedDurationUnits[unit]; !ok {
				ce = ce.Append("duration metric unit", fmt.Errorf("value of type duration should have a unit of seconds, milliseconds or microseconds"))
			}
		}

		// validate that metrics have less than 10 dimensions which is a cloudwatch limit
		if len(v.Dimensions) > dimensionLimit {
			ce = ce.Append("dimensions", fmt.Errorf("metrics can only contain %v dimensions", dimensionLimit))
		}
	}

	// LogGroupName should not be empty
	if len(b.adpCfg.GetLogGroupName()) == 0 {
		ce = ce.Append("log_group_name", fmt.Errorf("log_group_name should not be empty"))
	}
	// LogStreamName should not be empty
	if len(b.adpCfg.GetLogStreamName()) == 0 {
		ce = ce.Append("log_stream_name", fmt.Errorf("log_stream_name should not be empty"))
	}
	// Logs info should not be nil
	if b.adpCfg.GetLogs() == nil {
		ce = ce.Append("logs", fmt.Errorf("logs info should not be nil"))
	}
	// variables in the attributes should not be empty
	for _, v := range b.logEntryTypes {
		if len(v.Variables) == 0 {
			ce = ce.Append("instancevariables", fmt.Errorf("instance variables should not be empty"))
		}
	}
	return ce
}

// metric.HandlerBuilder#SetMetricTypes
func (b *builder) SetMetricTypes(types map[string]*metric.Type) {
	b.metricTypes = types
}

// logentry.HandlerBuilder#SetLogEntryTypes
func (b *builder) SetLogEntryTypes(types map[string]*logentry.Type) {
	b.logEntryTypes = types
}

// HandleMetric sends metrics to cloudwatch
func (h *handler) HandleMetric(ctx context.Context, insts []*metric.Instance) error {
	metricData := h.generateMetricData(insts)
	_, err := h.sendMetricsToCloudWatch(metricData)
	return err
}

// HandleLogEntry sends logentries to cloudwatchlogs
func (h *handler) HandleLogEntry(ctx context.Context, insts []*logentry.Instance) error {
	logentryData := h.generateLogEntryData(insts)
	_, err := h.sendLogEntriesToCloudWatch(logentryData)
	return err
}

// Close implements client closing functionality if necessary
func (h *handler) Close() error {
	return nil
}

// GetInfo returns the adapter.Info specific to this adapter.
func GetInfo() adapter.Info {
	info := metadata.GetInfo("cloudwatch")
	info.NewBuilder = func() adapter.HandlerBuilder { return &builder{} }
	return info
}
