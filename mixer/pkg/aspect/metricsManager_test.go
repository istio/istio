/// Copyright 2017 Istio Authors.
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
	"flag"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/adapter"
	atest "istio.io/mixer/pkg/adapter/test"
	aconfig "istio.io/mixer/pkg/aspect/config"
	"istio.io/mixer/pkg/aspect/test"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/config/descriptor"
	cfgpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
)

type fakeaspect struct {
	adapter.Aspect
	closed bool
	body   func([]adapter.Value) error
}

func (a *fakeaspect) Close() error {
	a.closed = true
	return nil
}

func (a *fakeaspect) Record(v []adapter.Value) error {
	return a.body(v)
}

type fakeBuilder struct {
	adapter.Builder
	name string

	body func() (adapter.MetricsAspect, error)
}

func (b *fakeBuilder) Name() string {
	return b.name
}

func (b *fakeBuilder) NewMetricsAspect(env adapter.Env, config adapter.Config,
	metrics map[string]*adapter.MetricDefinition) (adapter.MetricsAspect, error) {
	return b.body()
}

var (
	requestCountDesc = &dpb.MetricDescriptor{
		Name:        "request_count",
		Kind:        dpb.COUNTER,
		Value:       dpb.INT64,
		Description: "request count by source, target, service, and code",
		Labels: map[string]dpb.ValueType{
			"source":        dpb.STRING,
			"target":        dpb.STRING,
			"service":       dpb.STRING,
			"method":        dpb.STRING,
			"response_code": dpb.INT64,
		},
	}

	requestLatencyDesc = &dpb.MetricDescriptor{
		Name:        "request_latency",
		Kind:        dpb.COUNTER,
		Value:       dpb.DURATION,
		Description: "request latency by source, target, and service",
		Labels: map[string]dpb.ValueType{
			"source":        dpb.STRING,
			"target":        dpb.STRING,
			"service":       dpb.STRING,
			"method":        dpb.STRING,
			"response_code": dpb.INT64,
		},
	}

	df = test.NewDescriptorFinder(map[string]interface{}{
		"request_count":   requestCountDesc,
		"request_latency": requestLatencyDesc,
	})
)

func TestNewMetricsManager(t *testing.T) {
	m := newMetricsManager()
	if m.Kind() != config.MetricsKind {
		t.Errorf("m.Kind() = %s wanted %s", m.Kind(), config.MetricsKind)
	}
	if err := m.ValidateConfig(m.DefaultConfig(), nil, nil); err != nil {
		t.Errorf("m.ValidateConfig(m.DefaultConfig()) = %v; wanted no err", err)
	}
}

func TestMetricsManager_NewAspect(t *testing.T) {
	conf := &cfgpb.Combined{
		Aspect: &cfgpb.Aspect{
			Params: &aconfig.MetricsParams{
				Metrics: []*aconfig.MetricsParams_Metric{
					{
						DescriptorName: "request_count",
						Value:          "",
						Labels:         map[string]string{"source": "", "target": ""},
					},
				},
			},
		},
		// the params we use here don't matter because we're faking the aspect
		Builder: &cfgpb.Adapter{Params: &aconfig.MetricsParams{}},
	}
	builder := &fakeBuilder{name: "test", body: func() (adapter.MetricsAspect, error) {
		return &fakeaspect{body: func([]adapter.Value) error { return nil }}, nil
	}}
	f, _ := FromBuilder(builder, config.MetricsKind)
	if _, err := newMetricsManager().NewReportExecutor(conf, f, atest.NewEnv(t), df, ""); err != nil {
		t.Errorf("NewExecutor(conf, builder, test.NewEnv(t)) = _, %v; wanted no err", err)
	}
}

func TestMetricsManager_Validation(t *testing.T) {
	descs := map[string]interface{}{
		requestCountDesc.Name:   requestCountDesc,
		requestLatencyDesc.Name: requestLatencyDesc,
		"invalid desc": &dpb.MetricDescriptor{
			Name:   "invalid desc",
			Kind:   dpb.METRIC_KIND_UNSPECIFIED,
			Value:  dpb.INT64,
			Labels: map[string]dpb.ValueType{},
		},
		// our attributes
		"duration": &cfgpb.AttributeManifest_AttributeInfo{ValueType: dpb.DURATION},
		"string":   &cfgpb.AttributeManifest_AttributeInfo{ValueType: dpb.STRING},
		"int64":    &cfgpb.AttributeManifest_AttributeInfo{ValueType: dpb.INT64},
	}
	f := test.NewDescriptorFinder(descs)
	v, _ := expr.NewCEXLEvaluator(expr.DefaultCacheSize)

	// matches request count desc
	validParam := aconfig.MetricsParams_Metric{
		DescriptorName: requestCountDesc.Name,
		Value:          "int64",
		Labels: map[string]string{
			"source":        "string",
			"target":        "string",
			"service":       "string",
			"method":        "string",
			"response_code": "int64",
		},
	}

	missingDesc := validParam
	missingDesc.DescriptorName = "not in the descriptor finder"

	// annoyingly, even though we copy force a copy of the struct the copy points at the same map instance, so we need a new one
	invalidExpr := validParam
	invalidExpr.Labels = map[string]string{
		"source":        "string |", // invalid expr
		"target":        "string",
		"service":       "string",
		"method":        "string",
		"response_code": "int64",
	}

	wrongLabelType := validParam
	wrongLabelType.Labels = map[string]string{
		"source":        "string",
		"target":        "string",
		"service":       "int64", // should be string
		"method":        "string",
		"response_code": "int64",
	}

	extraLabel := validParam
	extraLabel.Labels = map[string]string{
		"source":        "string",
		"target":        "string",
		"service":       "string",
		"method":        "string",
		"response_code": "int64",
		"extra":         "string", // wrong dimensions
	}

	wrongValueType := validParam
	wrongValueType.Value = "duration" // should be int64

	invalidValueExpr := validParam
	invalidValueExpr.Value = "int64 |" // invalid expr

	badDesc := validParam
	badDesc.DescriptorName = "invalid desc"
	badDesc.Labels = make(map[string]string)

	tests := []struct {
		name string
		cfg  *aconfig.MetricsParams
		tc   expr.TypeChecker
		df   descriptor.Finder
		err  string
	}{
		{"empty config", &aconfig.MetricsParams{}, v, f, ""},
		{"valid", &aconfig.MetricsParams{Metrics: []*aconfig.MetricsParams_Metric{&validParam}}, v, f, ""},
		{"missing descriptor", &aconfig.MetricsParams{Metrics: []*aconfig.MetricsParams_Metric{&missingDesc}}, v, f, "could not find a descriptor"},
		{"failed type checking (bad expr)", &aconfig.MetricsParams{Metrics: []*aconfig.MetricsParams_Metric{&invalidExpr}}, v, f, "failed to parse expression"},
		{"eval'd type doesn't match desc", &aconfig.MetricsParams{Metrics: []*aconfig.MetricsParams_Metric{&wrongValueType}}, v, f, "expected type INT64"},
		{"invalid value expr", &aconfig.MetricsParams{Metrics: []*aconfig.MetricsParams_Metric{&invalidValueExpr}}, v, f, "failed to parse expression"},
		{"label eval'd type doesn't match desc", &aconfig.MetricsParams{Metrics: []*aconfig.MetricsParams_Metric{&wrongLabelType}}, v, f, "expected type STRING"},
		{"wrong dimensions for metric", &aconfig.MetricsParams{Metrics: []*aconfig.MetricsParams_Metric{&extraLabel}}, v, f, "wrong dimensions"},
		{"can't convert proto to adapter rep", &aconfig.MetricsParams{Metrics: []*aconfig.MetricsParams_Metric{&badDesc}}, v, f, "invalid desc"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			errs := (&metricsManager{}).ValidateConfig(tt.cfg, tt.tc, tt.df)
			if errs != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf("ValidateConfig(tt.cfg, tt.v, tt.df) = '%s', wanted no err", errs.Error())
				} else if !strings.Contains(errs.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%s'", tt.err, errs.Error())
				}
			}
		})
	}
}

func TestMetricsManager_NewAspect_PropagatesError(t *testing.T) {
	conf := &cfgpb.Combined{
		Aspect: &cfgpb.Aspect{Params: &aconfig.MetricsParams{}},
		// the params we use here don't matter because we're faking the aspect
		Builder: &cfgpb.Adapter{Params: &aconfig.MetricsParams{}},
	}
	errString := "expected"
	builder := &fakeBuilder{
		body: func() (adapter.MetricsAspect, error) {
			return nil, errors.New(errString)
		}}
	f, _ := FromBuilder(builder, config.MetricsKind)
	_, err := newMetricsManager().NewReportExecutor(conf, f, atest.NewEnv(t), df, "")
	if err == nil {
		t.Error("newMetricsManager().NewReportExecutor(conf, builder, test.NewEnv(t)) = _, nil; wanted err")
	}
	if !strings.Contains(err.Error(), errString) {
		t.Errorf("NewExecutor(conf, builder, test.NewEnv(t)) = _, %v; wanted err %s", err, errString)
	}
}

func TestMetricsExecutor_Execute(t *testing.T) {
	// TODO: all of these test values are hardcoded to match the metric definitions hardcoded in metricsManager
	// (since things have to line up for us to test them), they can be made dynamic when we get the ability to set the definitions
	goodEval := test.NewFakeEval(func(exp string, _ attribute.Bag) (interface{}, error) {
		switch exp {
		case "value":
			return 1, nil
		case "source":
			return "me", nil
		case "target":
			return "you", nil
		case "service":
			return "echo", nil
		default:
			return nil, fmt.Errorf("default case for exp = %s", exp)
		}
	})
	errEval := test.NewFakeEval(func(_ string, _ attribute.Bag) (interface{}, error) {
		return nil, errors.New("expected")
	})
	labelErrEval := test.NewFakeEval(func(exp string, _ attribute.Bag) (interface{}, error) {
		switch exp {
		case "value":
			return 1, nil
		default:
			return nil, errors.New("expected")
		}
	})

	goodMd := map[string]*metricInfo{
		"request_count": {
			definition: &adapter.MetricDefinition{Kind: adapter.Counter, Name: "request_count"},
			value:      "value",
			labels: map[string]string{
				"source":  "source",
				"target":  "target",
				"service": "service",
			},
		},
	}
	badGoodMd := map[string]*metricInfo{
		"bad": {
			definition: &adapter.MetricDefinition{Kind: adapter.Counter, Name: "bad"},
			value:      "bad",
			labels: map[string]string{
				"bad": "bad",
			},
		},
		"request_count": {
			definition: &adapter.MetricDefinition{Kind: adapter.Counter, Name: "request_count"},
			value:      "value",
			labels: map[string]string{
				"source":  "source",
				"target":  "target",
				"service": "service",
			},
		},
	}

	type o struct {
		value  interface{}
		labels []string
	}
	cases := []struct {
		mdin      map[string]*metricInfo
		recordErr error
		eval      expr.Evaluator
		out       map[string]o
		errString string
	}{
		{make(map[string]*metricInfo), nil, test.NewIDEval(), make(map[string]o), ""},
		{goodMd, nil, errEval, make(map[string]o), "expected"},
		{goodMd, nil, labelErrEval, make(map[string]o), "expected"},
		{goodMd, nil, goodEval, map[string]o{"request_count": {1, []string{"source", "target"}}}, ""},
		{goodMd, errors.New("record"), goodEval, map[string]o{"request_count": {1, []string{"source", "target"}}}, "record"},
		{badGoodMd, nil, goodEval, map[string]o{"request_count": {1, []string{"source", "target"}}}, "default case"},
	}
	for idx, c := range cases {
		t.Run(strconv.Itoa(idx), func(t *testing.T) {
			var receivedValues []adapter.Value
			executor := &metricsExecutor{
				aspect: &fakeaspect{body: func(v []adapter.Value) error {
					receivedValues = v
					return c.recordErr
				}},
				metadata: c.mdin,
			}
			out := executor.Execute(test.NewBag(), c.eval)

			errString := out.Message
			if !strings.Contains(errString, c.errString) {
				t.Errorf("executor.Execute(&fakeBag{}, eval) = _, %v; wanted error containing %s", out.Message, c.errString)
			}

			if len(receivedValues) != len(c.out) {
				t.Errorf("executor.Execute(&fakeBag{}, eval) got vals %v, wanted at least %d", receivedValues, len(c.out))
			}
			for _, v := range receivedValues {
				o, found := c.out[v.Definition.Name]
				if !found {
					t.Errorf("Got unexpected value %v, wanted only %v", v, c.out)
				}
				if v.MetricValue != o.value {
					t.Errorf("v.MetricValue = %v; wanted %v", v.MetricValue, o.value)
				}
				for _, l := range o.labels {
					if _, found := v.Labels[l]; !found {
						t.Errorf("value.Labels = %v; wanted label named %s", v.Labels, l)
					}
				}
			}
		})
	}
}

func TestMetricsExecutor_Close(t *testing.T) {
	inner := &fakeaspect{closed: false}
	executor := &metricsExecutor{aspect: inner}
	if err := executor.Close(); err != nil {
		t.Errorf("executor.Close() = %v; wanted no err", err)
	}
	if !inner.closed {
		t.Error("metricsExecutor.Close() didn't close the aspect inside")
	}
}

func TestMetrics_DescToDef(t *testing.T) {
	cases := []struct {
		in        *dpb.MetricDescriptor
		out       *adapter.MetricDefinition
		errString string
	}{
		{&dpb.MetricDescriptor{}, nil, "VALUE_TYPE_UNSPECIFIED"},
		{
			&dpb.MetricDescriptor{
				Name:   "bad label",
				Value:  dpb.STRING,
				Labels: map[string]dpb.ValueType{"invalid": dpb.VALUE_TYPE_UNSPECIFIED},
			},
			nil,
			"VALUE_TYPE_UNSPECIFIED",
		},
		{
			&dpb.MetricDescriptor{
				Name:   "bad metric kind",
				Kind:   dpb.METRIC_KIND_UNSPECIFIED,
				Value:  dpb.INT64,
				Labels: map[string]dpb.ValueType{"string": dpb.STRING},
			},
			nil,
			"METRIC_KIND_UNSPECIFIED",
		},
		{
			&dpb.MetricDescriptor{
				Name:   "without buckets",
				Kind:   dpb.DISTRIBUTION,
				Value:  dpb.DNS_NAME,
				Labels: map[string]dpb.ValueType{"string": dpb.STRING},
			},
			nil,
			"distribution",
		},
		{
			&dpb.MetricDescriptor{
				Name:    "bad buckets",
				Kind:    dpb.DISTRIBUTION,
				Value:   dpb.URI,
				Labels:  map[string]dpb.ValueType{"string": dpb.STRING},
				Buckets: &dpb.MetricDescriptor_BucketsDefinition{},
			},
			nil,
			"definitions",
		},
		{
			in: &dpb.MetricDescriptor{
				Name:   "explicit buckets",
				Kind:   dpb.DISTRIBUTION,
				Value:  dpb.INT64,
				Labels: map[string]dpb.ValueType{"string": dpb.STRING},
				Buckets: &dpb.MetricDescriptor_BucketsDefinition{
					Definition: &dpb.MetricDescriptor_BucketsDefinition_ExplicitBuckets{
						ExplicitBuckets: &dpb.MetricDescriptor_BucketsDefinition_Explicit{
							Bounds: []float64{3, 5, 9, 234},
						},
					},
				},
			},
			out: &adapter.MetricDefinition{
				Name:   "explicit buckets",
				Kind:   adapter.Distribution,
				Value:  adapter.Int64,
				Labels: map[string]adapter.LabelType{"string": adapter.String},
				Buckets: &adapter.ExplicitBuckets{
					Bounds: []float64{3, 5, 9, 234},
				},
			},
		},
		{
			in: &dpb.MetricDescriptor{
				Name:   "exponential buckets",
				Kind:   dpb.DISTRIBUTION,
				Value:  dpb.BOOL,
				Labels: map[string]dpb.ValueType{"string": dpb.STRING},
				Buckets: &dpb.MetricDescriptor_BucketsDefinition{
					Definition: &dpb.MetricDescriptor_BucketsDefinition_ExponentialBuckets{
						ExponentialBuckets: &dpb.MetricDescriptor_BucketsDefinition_Exponential{
							NumFiniteBuckets: 3,
							GrowthFactor:     10,
							Scale:            0.1,
						},
					},
				},
			},
			out: &adapter.MetricDefinition{
				Name:   "exponential buckets",
				Kind:   adapter.Distribution,
				Value:  adapter.Bool,
				Labels: map[string]adapter.LabelType{"string": adapter.String},
				Buckets: &adapter.ExponentialBuckets{
					Count:        3,
					GrowthFactor: 10,
					Scale:        0.1,
				},
			},
		},
		{
			in: &dpb.MetricDescriptor{
				Name:   "linear buckets",
				Kind:   dpb.DISTRIBUTION,
				Value:  dpb.STRING,
				Labels: map[string]dpb.ValueType{"string": dpb.STRING},
				Buckets: &dpb.MetricDescriptor_BucketsDefinition{
					Definition: &dpb.MetricDescriptor_BucketsDefinition_LinearBuckets{
						LinearBuckets: &dpb.MetricDescriptor_BucketsDefinition_Linear{
							NumFiniteBuckets: 3,
							Width:            40,
							Offset:           0.0001,
						},
					},
				},
			},
			out: &adapter.MetricDefinition{
				Name:   "linear buckets",
				Kind:   adapter.Distribution,
				Value:  adapter.String,
				Labels: map[string]adapter.LabelType{"string": adapter.String},
				Buckets: &adapter.LinearBuckets{
					Count:  3,
					Width:  40,
					Offset: 0.0001,
				},
			},
		},
		{
			&dpb.MetricDescriptor{
				Name:   "good",
				Kind:   dpb.COUNTER,
				Value:  dpb.EMAIL_ADDRESS,
				Labels: map[string]dpb.ValueType{"string": dpb.STRING},
			},
			&adapter.MetricDefinition{
				Name:   "good",
				Kind:   adapter.Counter,
				Value:  adapter.EmailAddress,
				Labels: map[string]adapter.LabelType{"string": adapter.String},
			},
			""},
	}
	for idx, c := range cases {
		t.Run(strconv.Itoa(idx), func(t *testing.T) {
			result, err := metricDefinitionFromProto(c.in)

			errString := ""
			if err != nil {
				errString = err.Error()
			}
			if !strings.Contains(errString, c.errString) {
				t.Errorf("metricsDescToDef(%v) = _, %v; wanted err containing %s", c.in, err, c.errString)
			}
			if !reflect.DeepEqual(result, c.out) {
				t.Errorf("metricsDescToDef(%v) = %v, %v; wanted %v", c.in, result, err, c.out)
			}
		})
	}
}

func init() {
	// bump up the log level so log-only logic runs during the tests, for correctness and coverage.
	_ = flag.Lookup("v").Value.Set("99")
}
