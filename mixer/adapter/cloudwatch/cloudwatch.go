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
// WITHOUT WARRANTIES OR CONDITIONS OF ANY Type, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -f mixer/adapter/cloudwatch/config/config.proto

package cloudwatch

import (
	"context"

	"github.com/aws/aws-sdk-go/service/cloudwatch/cloudwatchiface"

	"istio.io/istio/mixer/adapter/cloudwatch/config"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/metric"
)

type (
	builder struct {
		adpCfg      *config.Params
		metricTypes map[string]*metric.Type
	}

	// Handler holds data for the cloudwatch adapter handler
	Handler struct {
		metricTypes map[string]*metric.Type
		env         adapter.Env
		cfg         *config.Params
		cloudwatch  cloudwatchiface.CloudWatchAPI
	}
)

// ensure types implement the requisite interfaces
var _ metric.HandlerBuilder = &builder{}
var _ metric.Handler = &Handler{}

///////////////// Configuration-time Methods ///////////////

// NewHandler initializes a cloudwatch handler
func NewHandler(metricTypes map[string]*metric.Type, env adapter.Env, cfg *config.Params, cloudwatch cloudwatchiface.CloudWatchAPI) adapter.Handler {
	return &Handler{metricTypes: metricTypes, env: env, cfg: cfg, cloudwatch: cloudwatch}
}

// adapter.HandlerBuilder#Build
func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	cloudwatch := NewCloudWatchClient()
	return NewHandler(b.metricTypes, env, b.adpCfg, cloudwatch), nil
}

// adapter.HandlerBuilder#SetAdapterConfig
func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.adpCfg = cfg.(*config.Params)
}

// adapter.HandlerBuilder#Validate
func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	cfg := b.adpCfg

	// validate that instance and handler contain the same metrics
	for _, v := range cfg.GetMetricInfo() {
		if v.GetStorageResolution() != 1 && v.GetStorageResolution() != 60 {
			ce = ce.Appendf("storageResolution", "Storage resolution should be either 1 or 60")
		}

		//TODO: validate cw unit
		if len(v.GetUnit()) == 0 {
			ce = ce.Appendf("unit", "Cloudwatch metric unit must not be null")
		}
	}

	return
}

// metric.HandlerBuilder#SetMetricTypes
func (b *builder) SetMetricTypes(types map[string]*metric.Type) {
	b.metricTypes = types
}

// HandleMetric validates received instances and sends them to cloudwatch
func (h *Handler) HandleMetric(ctx context.Context, insts []*metric.Instance) error {
	validInsts := getValidMetrics(h, insts)
	return handleValidMetrics(h, validInsts)
}

func getValidMetrics(h *Handler, insts []*metric.Instance) []*metric.Instance {
	validInsts := make([]*metric.Instance, 0, len(insts))

	for _, inst := range insts {
		if _, ok := h.metricTypes[inst.Name]; !ok {
			_ = h.env.Logger().Errorf("There is no type found for instance %s", inst.Name)
			continue
		}

		// check that every instance metric has a corresponding cloudwatch handler metric
		if _, ok := h.cfg.GetMetricInfo()[inst.Name]; !ok {
			_ = h.env.Logger().Errorf("There is no corresponding cloudwatch handler metric present for instance %s", inst.Name)
			continue
		}

		validInsts = append(validInsts, inst)
	}

	return validInsts
}

// Close implements client closing functionality if necessary
func (h *Handler) Close() error {
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
