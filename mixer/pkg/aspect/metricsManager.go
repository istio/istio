// Copyright 2017 Istio Authors.
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
	"errors"
	"fmt"
	"time"

	"github.com/golang/glog"
	rpc "github.com/googleapis/googleapis/google/rpc"
	multierror "github.com/hashicorp/go-multierror"

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
	metricsManager struct{}

	metricInfo struct {
		definition *adapter.MetricDefinition
		value      string
		labels     map[string]string
	}

	metricsExecutor struct {
		name     string
		aspect   adapter.MetricsAspect
		metadata map[string]*metricInfo // metric name -> info
	}
)

// newMetricsManager returns a manager for the metric aspect.
func newMetricsManager() ReportManager {
	return &metricsManager{}
}

func (m *metricsManager) NewReportExecutor(c *cpb.Combined, createAspect CreateAspectFunc, env adapter.Env,
	df descriptor.Finder, _ string) (ReportExecutor, error) {
	params := c.Aspect.Params.(*aconfig.MetricsParams)

	metadata := make(map[string]*metricInfo)
	defs := make(map[string]*adapter.MetricDefinition, len(params.Metrics))
	for _, metric := range params.Metrics {
		// we ignore the error as config validation confirms both that the metric exists and that it can
		// be converted safely into its definition
		def, _ := metricDefinitionFromProto(df.GetMetric(metric.DescriptorName))
		defs[def.Name] = def
		metadata[def.Name] = &metricInfo{
			definition: def,
			value:      metric.Value,
			labels:     metric.Labels,
		}
	}
	out, err := createAspect(env, c.Builder.Params.(adapter.Config), defs)
	if err != nil {
		return nil, fmt.Errorf("failed to construct metrics aspect with config '%v': %v", c, err)
	}
	asp, ok := out.(adapter.MetricsAspect)
	if !ok {
		return nil, fmt.Errorf("wrong aspect type returned after creation; expected MetricsAspect: %#v", out)
	}
	return &metricsExecutor{c.Builder.Name, asp, metadata}, nil
}

func (*metricsManager) Kind() config.Kind                  { return config.MetricsKind }
func (*metricsManager) DefaultConfig() config.AspectParams { return &aconfig.MetricsParams{} }

func (*metricsManager) ValidateConfig(c config.AspectParams, tc expr.TypeChecker, df descriptor.Finder) (ce *adapter.ConfigErrors) {
	cfg := c.(*aconfig.MetricsParams)
	for _, metric := range cfg.Metrics {
		desc := df.GetMetric(metric.DescriptorName)
		if desc == nil {
			ce = ce.Appendf("metrics", "could not find a descriptor for the metric '%s'", metric.DescriptorName)
			continue // we can't do any other validation without the descriptor
		}

		if err := tc.AssertType(metric.Value, df, desc.Value); err != nil {
			ce = ce.Appendf(fmt.Sprintf("metrics[%s].value", metric.DescriptorName), "error type checking label %s: %v", err)
		}
		ce = ce.Extend(validateLabels(fmt.Sprintf("metrics[%s].labels", desc.Name), metric.Labels, desc.Labels, tc, df))

		// TODO: this doesn't feel like quite the right spot to do this check, but it's the best we have ¯\_(ツ)_/¯
		if _, err := metricDefinitionFromProto(desc); err != nil {
			ce = ce.Appendf(fmt.Sprintf("descriptor[%s]", desc.Name), "failed to marshal descriptor into its adapter representation: %v", err)
		}
	}
	return
}

func (w *metricsExecutor) Execute(attrs attribute.Bag, mapper expr.Evaluator) rpc.Status {
	result := &multierror.Error{}
	var values []adapter.Value

	for name, md := range w.metadata {
		metricValue, err := mapper.Eval(md.value, attrs)
		if err != nil {
			result = multierror.Append(result, fmt.Errorf("failed to eval metric value for metric '%s': %v", name, err))
			continue
		}
		labels, err := evalAll(md.labels, attrs, mapper)
		if err != nil {
			result = multierror.Append(result, fmt.Errorf("failed to eval labels for metric '%s': %v", name, err))
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
		result = multierror.Append(result, fmt.Errorf("failed to record all values: %v", err))
	}

	if glog.V(4) {
		glog.V(4).Infof("completed execution of metric adapter '%s' for %d values", w.name, len(values))
	}

	err := result.ErrorOrNil()
	if err != nil {
		return status.WithError(err)
	}

	return status.OK
}

func (w *metricsExecutor) Close() error {
	return w.aspect.Close()
}

func metricDefinitionFromProto(desc *dpb.MetricDescriptor) (*adapter.MetricDefinition, error) {
	labels := make(map[string]adapter.LabelType, len(desc.Labels))
	for name, labelType := range desc.Labels {
		l, err := valueTypeToLabelType(labelType)
		if err != nil {
			return nil, fmt.Errorf("descriptor '%s' label '%v' failed to convert label type value '%v' from proto: %v",
				desc.Name, name, labelType, err)
		}
		labels[name] = l
	}
	value, err := valueTypeToLabelType(desc.Value)
	if err != nil {
		return nil, fmt.Errorf("descriptor '%s' failed to convert metric value '%v' from proto: %v", desc.Name, desc.Value, err)
	}

	kind, err := metricKindFromProto(desc.Kind)
	if err != nil {
		return nil, fmt.Errorf("descriptor '%s' failed to convert metric kind value '%v' from proto: %v", desc.Name, desc.Kind, err)
	} else if kind == adapter.Distribution && desc.Buckets == nil {
		return nil, fmt.Errorf("invalid descriptor '%s': metrics with metric kind of distribution must define buckets", desc.Name)
	}

	def := &adapter.MetricDefinition{
		Name:        desc.Name,
		DisplayName: desc.DisplayName,
		Description: desc.Description,
		Value:       value,
		Kind:        kind,
		Labels:      labels,
	}

	if desc.Buckets != nil {
		b, err := bucketDefinitionFromProto(desc.Buckets)
		if err != nil {
			return nil, fmt.Errorf("invalid descriptor '%s': could not extract bucket definitions: %v", desc.Name, err)
		}
		def.Buckets = b
	}
	return def, nil
}

func bucketDefinitionFromProto(buckets *dpb.MetricDescriptor_BucketsDefinition) (adapter.BucketDefinition, error) {
	switch buckets.Definition.(type) {
	case *dpb.MetricDescriptor_BucketsDefinition_LinearBuckets:
		lb := buckets.Definition.(*dpb.MetricDescriptor_BucketsDefinition_LinearBuckets)
		return &adapter.LinearBuckets{
			Count:  lb.LinearBuckets.NumFiniteBuckets,
			Width:  lb.LinearBuckets.Width,
			Offset: lb.LinearBuckets.Offset,
		}, nil
	case *dpb.MetricDescriptor_BucketsDefinition_ExponentialBuckets:
		eb := buckets.Definition.(*dpb.MetricDescriptor_BucketsDefinition_ExponentialBuckets)
		return &adapter.ExponentialBuckets{
			Count:        eb.ExponentialBuckets.NumFiniteBuckets,
			GrowthFactor: eb.ExponentialBuckets.GrowthFactor,
			Scale:        eb.ExponentialBuckets.Scale,
		}, nil
	case *dpb.MetricDescriptor_BucketsDefinition_ExplicitBuckets:
		ex := buckets.Definition.(*dpb.MetricDescriptor_BucketsDefinition_ExplicitBuckets)
		return &adapter.ExplicitBuckets{
			Bounds: ex.ExplicitBuckets.Bounds,
		}, nil
	}
	return nil, errors.New("could not build bucket definitions from proto")
}
