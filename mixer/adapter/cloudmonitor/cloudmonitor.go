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
// WITHOUT WARRANTIES OR CONDITIONS OF ANY Type, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// nolint: lll
//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -a mixer/adapter/cloudmonitor/config/config.proto -x "-n cloudmonitor -t metric"

package cloudmonitor

import (
	"context"

	"fmt"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/cms"

	"istio.io/istio/mixer/adapter/cloudmonitor/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/metric"
)

const (
	// CloudMonitor enforced limit
	// https://help.aliyun.com/document_detail/63275.html
	dimensionLimit = 10
)

var supportedValueTypes = map[string]bool{
	"STRING":   true,
	"INT64":    true,
	"DOUBLE":   true,
	"DURATION": true,
}

var supportedDurationUnits = map[string]bool{
	"Seconds": true,
}

type (
	builder struct {
		adpCfg      *config.Params
		metricTypes map[string]*metric.Type
	}
	// handler holds data for the cloudmonitor adapter handler
	handler struct {
		metricTypes map[string]*metric.Type
		env         adapter.Env
		cfg         *config.Params
		client      *cms.Client
	}
)

// ensure types implement the requisite interfaces
var _ metric.HandlerBuilder = &builder{}
var _ metric.Handler = &handler{}

///////////////// Configuration-time Methods ///////////////

// newHandler initializes a cloudmonitor handler
func newHandler(metricTypes map[string]*metric.Type, env adapter.Env, cfg *config.Params, client *cms.Client) adapter.Handler {
	return &handler{metricTypes: metricTypes, env: env, cfg: cfg, client: client}
}

// adapter.HandlerBuilder#Build
func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	client, err := cms.NewClientWithAccessKey(b.adpCfg.RegiondId, b.adpCfg.AccessKeyId, b.adpCfg.AccessKeySecret)
	if err != nil {
		panic(err)
	}
	return newHandler(b.metricTypes, env, b.adpCfg, client), nil
}

// adapter.HandlerBuilder#SetAdapterConfig
func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.adpCfg = cfg.(*config.Params)
}

// adapter.HandlerBuilder#Validate
func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	if len(b.adpCfg.GetMetricInfo()) != len(b.metricTypes) {
		ce = ce.Append("metricInfo", fmt.Errorf("metricInfo and instance config must contain the same metrics"))
	}
	if len(b.adpCfg.GetRegiondId()) == 0 {
		ce = ce.Append("RegionId", fmt.Errorf("RegionId should not be empty"))
	}
	if len(b.adpCfg.GetAccessKeyId()) == 0 {
		ce = ce.Append("AccessKeyId", fmt.Errorf("AccessKeyId should not be empty"))
	}
	if len(b.adpCfg.GetAccessKeySecret()) == 0 {
		ce = ce.Append("AccessKeySecret", fmt.Errorf("AccessKeySecret should not be empty"))
	}
	if b.adpCfg.GetGroupId() == 0 {
		ce = ce.Append("GroupId", fmt.Errorf("GroupId should be non-empty and greater than zero"))
	}
	for k, v := range b.metricTypes {
		// validate handler config contains required metric config
		if _, ok := b.adpCfg.GetMetricInfo()[k]; !ok {
			ce = ce.Append("metricInfo", fmt.Errorf("metricInfo and instance config must contain the same metrics but missing %v", k))
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

////////////////// Request-time Methods //////////////////////////
// metric.Handler#HandleMetric
func (h *handler) HandleMetric(ctx context.Context, insts []*metric.Instance) error {
	metricData := h.generateMetricData(insts)
	_, err := h.sendMetricsToCloudMonitor(metricData)
	return err
}

// adapter.Handler#Close
func (h *handler) Close() error { return nil }

////////////////// Bootstrap //////////////////////////
// GetInfo returns the adapter.Info specific to this adapter.
func GetInfo() adapter.Info {
	return adapter.Info{
		Name:        "cloudmonitor",
		Description: "Sends metrics to AliCloud CloudMonitor",
		SupportedTemplates: []string{
			metric.TemplateName,
		},
		NewBuilder:    func() adapter.HandlerBuilder { return &builder{} },
		DefaultConfig: &config.Params{},
	}
}
