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

package descriptor

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	dpb "istio.io/api/mixer/v1/config/descriptor"
)

func TestValidateLogEntry(t *testing.T) {
	tests := []struct {
		name string
		in   *dpb.LogEntryDescriptor
		err  string
	}{
		{"valid", &dpb.LogEntryDescriptor{
			Name:          "valid",
			PayloadFormat: dpb.TEXT,
			LogTemplate:   "{{.foo}}",
			Labels:        map[string]dpb.ValueType{},
		}, ""},
		{"invalid payload", &dpb.LogEntryDescriptor{
			Name:          "invalid_payload",
			PayloadFormat: dpb.PAYLOAD_FORMAT_UNSPECIFIED,
			LogTemplate:   "{{.foo}}",
			Labels:        map[string]dpb.ValueType{},
		}, "payloadFormat"},
		{"invalid template", &dpb.LogEntryDescriptor{
			Name:          "invalid_template",
			PayloadFormat: dpb.JSON,
			LogTemplate:   "",
			Labels:        map[string]dpb.ValueType{},
		}, "logTemplate"},
		{"invalid labels", &dpb.LogEntryDescriptor{
			Name:          "invalid_labels",
			PayloadFormat: dpb.TEXT,
			LogTemplate:   "{{.foo}}",
			Labels: map[string]dpb.ValueType{
				"find": dpb.STRING,
				"oops": dpb.VALUE_TYPE_UNSPECIFIED,
			},
		}, "labels"},
		{"invalid name", &dpb.LogEntryDescriptor{
			Name:          "",
			PayloadFormat: dpb.JSON,
			LogTemplate:   "",
			Labels:        map[string]dpb.ValueType{},
		}, "name"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			if err := ValidateLogEntry(tt.in); err != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf("ValidateLogEntry() = '%v', wanted no err", err)
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%v'", tt.err, err)
				}
			}
		})
	}
}

func TestValidateMetric(t *testing.T) {
	tests := []struct {
		name string
		in   *dpb.MetricDescriptor
		err  string
	}{
		{"valid", &dpb.MetricDescriptor{
			Name:   "valid",
			Kind:   dpb.COUNTER,
			Value:  dpb.INT64,
			Labels: map[string]dpb.ValueType{},
		}, ""},
		{"invalid kind", &dpb.MetricDescriptor{
			Name:   "invalid_kind",
			Kind:   dpb.METRIC_KIND_UNSPECIFIED,
			Value:  dpb.INT64,
			Labels: map[string]dpb.ValueType{},
		}, "kind"},
		{"invalid value", &dpb.MetricDescriptor{
			Name:   "invalid_value",
			Kind:   dpb.COUNTER,
			Value:  dpb.VALUE_TYPE_UNSPECIFIED,
			Labels: map[string]dpb.ValueType{},
		}, "value"},
		{"invalid labels", &dpb.MetricDescriptor{
			Name:  "invalid_labels",
			Kind:  dpb.COUNTER,
			Value: dpb.INT64,
			Labels: map[string]dpb.ValueType{
				"find": dpb.STRING,
				"oops": dpb.VALUE_TYPE_UNSPECIFIED,
			},
		}, "labels"},
		{"invalid name", &dpb.MetricDescriptor{
			Name:   "",
			Kind:   dpb.COUNTER,
			Value:  dpb.INT64,
			Labels: map[string]dpb.ValueType{},
		}, "name"},
		{"valid buckets", &dpb.MetricDescriptor{
			Name:   "bucketz",
			Kind:   dpb.DISTRIBUTION,
			Value:  dpb.INT64,
			Labels: map[string]dpb.ValueType{},
			Buckets: &dpb.MetricDescriptor_BucketsDefinition{
				Definition: &dpb.MetricDescriptor_BucketsDefinition_LinearBuckets{
					LinearBuckets: &dpb.MetricDescriptor_BucketsDefinition_Linear{
						NumFiniteBuckets: 1,
						Width:            1,
						Offset:           0,
					},
				},
			},
		}, ""},
		{"nil bickets", &dpb.MetricDescriptor{
			Name:   "",
			Kind:   dpb.DISTRIBUTION,
			Value:  dpb.INT64,
			Labels: map[string]dpb.ValueType{},
		}, "buckets"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			if err := ValidateMetric(tt.in); err != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf("ValidateMetric() = '%s', wanted no err", err.Error())
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%s'", tt.err, err.Error())
				}
			}
		})
	}
}

func TestValidateBucket(t *testing.T) {
	tests := []struct {
		name string
		in   *dpb.MetricDescriptor_BucketsDefinition
		err  string
	}{
		{"valid linear", &dpb.MetricDescriptor_BucketsDefinition{
			Definition: &dpb.MetricDescriptor_BucketsDefinition_LinearBuckets{
				LinearBuckets: &dpb.MetricDescriptor_BucketsDefinition_Linear{
					NumFiniteBuckets: 1,
					Width:            1,
					Offset:           0,
				},
			},
		}, ""},
		{"invalid linear - num", &dpb.MetricDescriptor_BucketsDefinition{
			Definition: &dpb.MetricDescriptor_BucketsDefinition_LinearBuckets{
				LinearBuckets: &dpb.MetricDescriptor_BucketsDefinition_Linear{
					NumFiniteBuckets: 0,
					Width:            1,
					Offset:           0,
				},
			},
		}, "linearBuckets.numFiniteBuckets"},
		{"invalid linear - width", &dpb.MetricDescriptor_BucketsDefinition{
			Definition: &dpb.MetricDescriptor_BucketsDefinition_LinearBuckets{
				LinearBuckets: &dpb.MetricDescriptor_BucketsDefinition_Linear{
					NumFiniteBuckets: 10,
					Width:            0,
					Offset:           0,
				},
			},
		}, "linearBuckets.width"},
		{"valid exponential", &dpb.MetricDescriptor_BucketsDefinition{
			Definition: &dpb.MetricDescriptor_BucketsDefinition_ExponentialBuckets{
				ExponentialBuckets: &dpb.MetricDescriptor_BucketsDefinition_Exponential{
					NumFiniteBuckets: 1,
					GrowthFactor:     2,
					Scale:            1,
				},
			},
		}, ""},
		{"invalid exponential - num", &dpb.MetricDescriptor_BucketsDefinition{
			Definition: &dpb.MetricDescriptor_BucketsDefinition_ExponentialBuckets{
				ExponentialBuckets: &dpb.MetricDescriptor_BucketsDefinition_Exponential{
					NumFiniteBuckets: -1,
					GrowthFactor:     2,
					Scale:            1,
				},
			},
		}, "exponentialBuckets.numFiniteBuckets"},
		{"invalid exponential - growth", &dpb.MetricDescriptor_BucketsDefinition{
			Definition: &dpb.MetricDescriptor_BucketsDefinition_ExponentialBuckets{
				ExponentialBuckets: &dpb.MetricDescriptor_BucketsDefinition_Exponential{
					NumFiniteBuckets: 10,
					GrowthFactor:     0.5,
					Scale:            1,
				},
			},
		}, "exponentialBuckets.growthFactor"},
		{"invalid exponential - scale", &dpb.MetricDescriptor_BucketsDefinition{
			Definition: &dpb.MetricDescriptor_BucketsDefinition_ExponentialBuckets{
				ExponentialBuckets: &dpb.MetricDescriptor_BucketsDefinition_Exponential{
					NumFiniteBuckets: 10,
					GrowthFactor:     2,
					Scale:            -1,
				},
			},
		}, "exponentialBuckets.scale"},
		{"valid explicit", &dpb.MetricDescriptor_BucketsDefinition{
			Definition: &dpb.MetricDescriptor_BucketsDefinition_ExplicitBuckets{
				ExplicitBuckets: &dpb.MetricDescriptor_BucketsDefinition_Explicit{
					Bounds: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
				},
			},
		}, ""},
		{"invalid explicit", &dpb.MetricDescriptor_BucketsDefinition{
			Definition: &dpb.MetricDescriptor_BucketsDefinition_ExplicitBuckets{
				ExplicitBuckets: &dpb.MetricDescriptor_BucketsDefinition_Explicit{
					Bounds: []float64{4, 3, 2, 1, 0},
				},
			},
		}, "explicitBuckets.bounds"},
		{"invalid explicit", &dpb.MetricDescriptor_BucketsDefinition{
			Definition: &dpb.MetricDescriptor_BucketsDefinition_ExplicitBuckets{
				ExplicitBuckets: &dpb.MetricDescriptor_BucketsDefinition_Explicit{
					Bounds: []float64{0, 1, 1, 2},
				},
			},
		}, "explicitBuckets.bounds"},
		{"invalid explicit", &dpb.MetricDescriptor_BucketsDefinition{
			Definition: &dpb.MetricDescriptor_BucketsDefinition_ExplicitBuckets{
				ExplicitBuckets: &dpb.MetricDescriptor_BucketsDefinition_Explicit{
					Bounds: []float64{-1, -2, -3},
				},
			},
		}, "explicitBuckets.bounds"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			if err := validateBucket(tt.in); err != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf("validateBucket() = '%s', wanted no err", err.Error())
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%s'", tt.err, err.Error())
				}
			}
		})
	}
}

func TestValidateQuota(t *testing.T) {
	tests := []struct {
		name string
		in   *dpb.QuotaDescriptor
		err  string
	}{
		{"valid", &dpb.QuotaDescriptor{
			Name:   "valid",
			Labels: map[string]dpb.ValueType{},
		}, ""},
		{"invalid labels", &dpb.QuotaDescriptor{
			Name: "invalid_labels",
			Labels: map[string]dpb.ValueType{
				"find": dpb.STRING,
				"oops": dpb.VALUE_TYPE_UNSPECIFIED,
			},
		}, "labels"},
		{"invalid name", &dpb.QuotaDescriptor{
			Name:   "",
			Labels: map[string]dpb.ValueType{},
		}, "name"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			if err := ValidateQuota(tt.in); err != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf("ValidateQuota() = '%s', wanted no err", err.Error())
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%s'", tt.err, err.Error())
				}
			}
		})
	}
}

func TestValidateMonitoredResource(t *testing.T) {
	tests := []struct {
		name string
		in   *dpb.MonitoredResourceDescriptor
		err  string
	}{
		{"valid", &dpb.MonitoredResourceDescriptor{
			Name:   "valid",
			Labels: map[string]dpb.ValueType{},
		}, ""},
		{"invalid labels", &dpb.MonitoredResourceDescriptor{
			Name: "invalid_labels",
			Labels: map[string]dpb.ValueType{
				"find": dpb.STRING,
				"oops": dpb.VALUE_TYPE_UNSPECIFIED,
			},
		}, "labels"},
		{"invalid name", &dpb.MonitoredResourceDescriptor{
			Name:   "",
			Labels: map[string]dpb.ValueType{},
		}, "name"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			if err := ValidateMonitoredResource(tt.in); err != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf("ValidateMonitoredResource() = '%s', wanted no err", err.Error())
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%s'", tt.err, err.Error())
				}
			}
		})
	}
}

func TestValidatePrincipal(t *testing.T) {
	tests := []struct {
		name string
		in   *dpb.PrincipalDescriptor
		err  string
	}{
		{"valid", &dpb.PrincipalDescriptor{
			Name:   "valid",
			Labels: map[string]dpb.ValueType{},
		}, ""},
		{"invalid labels", &dpb.PrincipalDescriptor{
			Name: "invalid_labels",
			Labels: map[string]dpb.ValueType{
				"find": dpb.STRING,
				"oops": dpb.VALUE_TYPE_UNSPECIFIED,
			},
		}, "labels"},
		{"invalid name", &dpb.PrincipalDescriptor{
			Name:   "",
			Labels: map[string]dpb.ValueType{},
		}, "name"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			if err := ValidatePrincipal(tt.in); err != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf("ValidatePrincipal() = '%s', wanted no err", err.Error())
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%s'", tt.err, err.Error())
				}
			}
		})
	}
}

func TestValidateLabels(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]dpb.ValueType
		err  string
	}{
		{"valid", map[string]dpb.ValueType{"one": dpb.STRING, "two": dpb.INT64}, ""},
		{"invalid", map[string]dpb.ValueType{"one": dpb.STRING, "two": dpb.VALUE_TYPE_UNSPECIFIED}, "labels[two]"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			if err := validateLabels(tt.in); err != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf("validateLabels() = '%s', wanted no err", err.Error())
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%s'", tt.err, err.Error())
				}
			}
		})
	}
}

func TestValidateDescriptorName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		err  string
	}{
		{"valid", "somename", ""},
		{"invalid", "$invalid", ""},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			if err := validateDescriptorName(tt.in); err != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf("validateDescriptorName() = '%s', wanted no err", err.Error())
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%s'", tt.err, err.Error())
				}
			}
		})
	}
}

func TestAlmostEq(t *testing.T) {
	// Move to the nth next representable f64 value
	nthNextFloat := func(a float64, n uint64) float64 {
		return math.Float64frombits(math.Float64bits(a) + n)
	}

	tests := []struct {
		a     float64
		b     float64
		steps int64
		out   bool
	}{
		{1, 1, 0, true},
		{+0, -0, 0, true},
		{float64(1) / 3, float64(1) / 3.000001, 10, false},
		{1, 2, 1000, false},
		{123.0, nthNextFloat(123.0, 2), 1, false},
		{123.0, nthNextFloat(123.0, 2), 2, true},
		{123.0, nthNextFloat(123.0, 2), 3, true},
		{math.Inf(1), math.Inf(1), 0, true},
		{math.Inf(-1), math.Inf(-1), 0, true},
		// the following are not eq no matter how many steps.
		{math.Inf(1), math.Inf(-1), math.MaxInt64, false},
		{math.Inf(-1), math.Inf(1), math.MaxInt64, false},
		{math.NaN(), math.NaN(), math.MaxInt64, false},
		{0, math.NaN(), math.MaxInt64, false},
		{math.NaN(), 0, math.MaxInt64, false},
	}

	for idx, tt := range tests {
		t.Run(strconv.Itoa(idx), func(t *testing.T) {
			if actual := almostEq(tt.a, tt.b, tt.steps); actual != tt.out {
				t.Fatalf("almostEq(%f, %f, %d) = %t, wanted %t", tt.a, tt.b, tt.steps, actual, tt.out)
			}
		})
	}
}
