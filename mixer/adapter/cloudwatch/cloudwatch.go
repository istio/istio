// Copyright 2018 Istio Authors
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
//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -a mixer/adapter/cloudwatch/config/config.proto -x "-n cloudwatch -t metric"

package cloudwatch

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go/service/cloudwatch/cloudwatchiface"

	istio_policy_v1beta1 "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/adapter/cloudwatch/config"
	"istio.io/istio/mixer/pkg/adapter"
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
		adpCfg      *config.Params
		metricTypes map[string]*metric.Type
	}

	// handler holds data for the cloudwatch adapter handler
	handler struct {
		metricTypes map[string]*metric.Type
		env         adapter.Env
		cfg         *config.Params
		cloudwatch  cloudwatchiface.CloudWatchAPI
	}
)

// ensure types implement the requisite interfaces
var _ metric.HandlerBuilder = &builder{}
var _ metric.Handler = &handler{}

///////////////// Configuration-time Methods ///////////////

// newHandler initializes a cloudwatch handler
func newHandler(metricTypes map[string]*metric.Type, env adapter.Env, cfg *config.Params, cloudwatch cloudwatchiface.CloudWatchAPI) adapter.Handler {
	return &handler{metricTypes: metricTypes, env: env, cfg: cfg, cloudwatch: cloudwatch}
}

// adapter.HandlerBuilder#Build
func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	cloudwatch := newCloudWatchClient()
	return newHandler(b.metricTypes, env, b.adpCfg, cloudwatch), nil
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
	return ce
}

// metric.HandlerBuilder#SetMetricTypes
func (b *builder) SetMetricTypes(types map[string]*metric.Type) {
	b.metricTypes = types
}

// HandleMetric sends metrics to cloudwatch
func (h *handler) HandleMetric(ctx context.Context, insts []*metric.Instance) error {
	metricData := h.generateMetricData(insts)
	_, err := h.sendMetricsToCloudWatch(metricData)
	return err
}

// Close implements client closing functionality if necessary
func (h *handler) Close() error {
	return nil
}

// GetInfo returns the adapter.Info specific to this adapter.
func GetInfo() adapter.Info {
	return adapter.Info{
		Name:        "cloudwatch",
		Description: "Sends metrics to cloudwatch",
		SupportedTemplates: []string{
			metric.TemplateName,
		},
		NewBuilder:    func() adapter.HandlerBuilder { return &builder{} },
		DefaultConfig: &config.Params{},
	}
}
