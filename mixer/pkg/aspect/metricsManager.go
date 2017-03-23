// Copyright 2017 the Istio Authors.
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

package aspect

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/hashicorp/go-multierror"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/adapter"
	aconfig "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/config/descriptor"
	cpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/status"
)

type (
	metricsManager struct {
	}

	metricInfo struct {
		definition *adapter.MetricDefinition
		value      string
		labels     map[string]string
	}

	metricsWrapper struct {
		name     string
		aspect   adapter.MetricsAspect
		metadata map[string]*metricInfo // metric name -> info
	}
)

// newMetricsManager returns a manager for the metric aspect.
func newMetricsManager() Manager {
	return &metricsManager{}
}

// NewAspect creates a metric aspect.
func (m *metricsManager) NewAspect(c *cpb.Combined, a adapter.Builder, env adapter.Env, df descriptor.Finder) (Wrapper, error) {
	params := c.Aspect.Params.(*aconfig.MetricsParams)

	// TODO: get descriptors from config
	// TODO: sync these schemas with the new standardized metric schemas.
	desc := []*dpb.MetricDescriptor{
		{
			Name:        "request_count",
			Kind:        dpb.COUNTER,
			Value:       dpb.INT64,
			Description: "request count by source, target, service, and code",
			Labels: []*dpb.LabelDescriptor{
				{Name: "source", ValueType: dpb.STRING},
				{Name: "target", ValueType: dpb.STRING},
				{Name: "service", ValueType: dpb.STRING},
				{Name: "method", ValueType: dpb.STRING},
				{Name: "response_code", ValueType: dpb.INT64},
			},
		},
		{
			Name:        "request_latency",
			Kind:        dpb.COUNTER, // TODO: nail this down; as is we'll have to do post-processing
			Value:       dpb.DURATION,
			Description: "request latency by source, target, and service",
			Labels: []*dpb.LabelDescriptor{
				{Name: "source", ValueType: dpb.STRING},
				{Name: "target", ValueType: dpb.STRING},
				{Name: "service", ValueType: dpb.STRING},
				{Name: "method", ValueType: dpb.STRING},
				{Name: "response_code", ValueType: dpb.INT64},
			},
		},
	}

	metadata := make(map[string]*metricInfo)
	defs := make(map[string]*adapter.MetricDefinition, len(desc))
	for _, d := range desc {
		metric, found := findMetric(params.Metrics, d.Name)
		if !found {
			env.Logger().Warningf("No metric found for descriptor %s, skipping it", d.Name)
			continue
		}

		// TODO: once we plumb descriptors into the validation, remove this err: no descriptor should make it through validation
		// if it cannot be converted into a MetricDefinition, so we should never have to handle the error case.
		def, err := metricDefinitionFromProto(d)
		if err != nil {
			_ = env.Logger().Errorf("Failed to convert metric descriptor '%s' to definition with err: %s; skipping it.", d.Name, err)
			continue
		}

		defs[def.Name] = def
		metadata[def.Name] = &metricInfo{
			definition: def,
			value:      metric.Value,
			labels:     metric.Labels,
		}
	}
	b := a.(adapter.MetricsBuilder)
	asp, err := b.NewMetricsAspect(env, c.Builder.Params.(adapter.Config), defs)
	if err != nil {
		return nil, fmt.Errorf("failed to construct metrics aspect with config '%v' and err: %s", c, err)
	}
	return &metricsWrapper{b.Name(), asp, metadata}, nil
}

func (*metricsManager) Kind() Kind                         { return MetricsKind }
func (*metricsManager) DefaultConfig() config.AspectParams { return &aconfig.MetricsParams{} }

func (*metricsManager) ValidateConfig(config.AspectParams, expr.Validator, descriptor.Finder) (ce *adapter.ConfigErrors) {
	// TODO: we need to be provided the metric descriptors in addition to the metrics themselves here, so we can do type assertions.
	// We also need some way to assert the type of the result of evaluating an expression, but we don't have any attributes or an
	// evaluator on hand.

	// TODO: verify all descriptors can be marshalled into istio structs (DefinitionFromProto)

	return
}

func (w *metricsWrapper) Execute(attrs attribute.Bag, mapper expr.Evaluator, ma APIMethodArgs) Output {
	result := &multierror.Error{}
	var values []adapter.Value

	for name, md := range w.metadata {
		metricValue, err := mapper.Eval(md.value, attrs)
		if err != nil {
			result = multierror.Append(result, fmt.Errorf("failed to eval metric value for metric '%s' with err: %s", name, err))
			continue
		}
		labels, err := evalAll(md.labels, attrs, mapper)
		if err != nil {
			result = multierror.Append(result, fmt.Errorf("failed to eval labels for metric '%s' with err: %s", name, err))
			continue
		}

		// TODO: investigate either pooling these, or keeping a set around that has only its field's values updated.
		// we could keep a map[metric name]value, iterate over the it updating only the fields in each value
		values = append(values, adapter.Value{
			Definition: md.definition,
			Labels:     labels,
			// TODO: extract standard timestamp attributes for start/end once we det'm what they are
			StartTime:   time.Now(),
			EndTime:     time.Now(),
			MetricValue: metricValue,
		})
	}

	if err := w.aspect.Record(values); err != nil {
		result = multierror.Append(result, fmt.Errorf("failed to record all values with err: %s", err))
	}

	if glog.V(4) {
		glog.V(4).Infof("completed execution of metric adapter '%s' for %d values", w.name, len(values))
	}

	err := result.ErrorOrNil()
	if err != nil {
		return Output{Status: status.WithError(err)}
	}

	return Output{Status: status.OK}
}

func (w *metricsWrapper) Close() error {
	return w.aspect.Close()
}

func evalAll(expressions map[string]string, attrs attribute.Bag, eval expr.Evaluator) (map[string]interface{}, error) {
	result := &multierror.Error{}
	labels := make(map[string]interface{}, len(expressions))
	for label, texpr := range expressions {
		val, err := eval.Eval(texpr, attrs)
		if err != nil {
			result = multierror.Append(result, fmt.Errorf("failed to construct value for label '%s' with err: %s", label, err))
			continue
		}
		labels[label] = val
	}
	return labels, result.ErrorOrNil()
}

func metricDefinitionFromProto(desc *dpb.MetricDescriptor) (*adapter.MetricDefinition, error) {
	labels := make(map[string]adapter.LabelType, len(desc.Labels))
	for _, label := range desc.Labels {
		l, err := valueTypeToLabelType(label.ValueType)
		if err != nil {
			return nil, fmt.Errorf("descriptor '%s' label '%s' failed to convert label type value '%v' from proto with err: %s",
				desc.Name, label.Name, label.ValueType, err)
		}
		labels[label.Name] = l
	}
	kind, err := metricKindFromProto(desc.Kind)
	if err != nil {
		return nil, fmt.Errorf("descriptor '%s' failed to convert metric kind value '%v' from proto with err: %s",
			desc.Name, desc.Kind, err)
	}
	return &adapter.MetricDefinition{
		Name:        desc.Name,
		DisplayName: desc.DisplayName,
		Description: desc.Description,
		Kind:        kind,
		Labels:      labels,
	}, nil
}

func findMetric(defs []*aconfig.MetricsParams_Metric, name string) (*aconfig.MetricsParams_Metric, bool) {
	for _, def := range defs {
		if def.DescriptorName == name {
			return def, true
		}
	}
	return nil, false
}
