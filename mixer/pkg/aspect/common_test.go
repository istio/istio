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
	"reflect"
	"strings"
	"testing"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/aspect/test"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"
)

func TestEvalAll(t *testing.T) {
	bag := &test.Bag{Strs: map[string]string{"1": "1", "2": "2"}}
	eval := test.NewFakeEval(func(s string, _ attribute.Bag) (interface{}, error) {
		str, f := bag.String(s)
		if !f {
			return nil, fmt.Errorf("no key %s", s)
		}
		return str, nil
	})

	tests := []struct {
		name  string
		exprs map[string]string
		out   map[string]interface{}
		err   string
	}{
		{"no expressions", map[string]string{}, map[string]interface{}{}, ""},
		{"valid expr", map[string]string{"foo": "1"}, map[string]interface{}{"foo": "1"}, ""},
		{"2 valid expr", map[string]string{"foo": "1", "bar": "2"}, map[string]interface{}{"foo": "1", "bar": "2"}, ""},
		{"1 valid, 1 invalid", map[string]string{"foo": "1", "bar": "does not exist"}, map[string]interface{}{"foo": "1"}, "failed to construct value"},
		{"1 valid, 1 invalid", map[string]string{"foo": "does not exist", "bar": "2"}, map[string]interface{}{"bar": "2"}, "failed to construct value"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			out, err := evalAll(tt.exprs, bag, eval)
			if err != nil {
				if tt.err == "" {
					t.Errorf("ValidateConfig(tt.cfg, tt.v, tt.df) = '%s', wanted no err", err.Error())
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Errorf("Expected errors containing the string '%s', actual: '%s'", tt.err, err.Error())
				}
			}

			if len(out) != len(tt.out) {
				t.Errorf("Expected %d outputs, got %d", len(tt.out), len(out))
			}
			for key, val := range out {
				if expected, found := tt.out[key]; !found {
					t.Errorf("unexpected extra output (%s, %v)", key, val)
				} else if !reflect.DeepEqual(expected, val) {
					t.Errorf("label %s = %v, wanted %v", key, val, expected)
				}
			}
		})
	}
}

func TestFindLabel(t *testing.T) {
	tests := []struct {
		in     string
		out    *dpb.LabelDescriptor
		labels []*dpb.LabelDescriptor
	}{
		{"empty", nil, []*dpb.LabelDescriptor{}},
		{"missing", nil, []*dpb.LabelDescriptor{{Name: "label", ValueType: dpb.BOOL}}},
		{"present", &dpb.LabelDescriptor{Name: "present", ValueType: dpb.INT64}, []*dpb.LabelDescriptor{{Name: "present", ValueType: dpb.INT64}}},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.in), func(t *testing.T) {
			if result := findLabel(tt.in, tt.labels); !reflect.DeepEqual(result, tt.out) {
				t.Fatalf("findLabel(%s, %v) = %v, wanted %v", tt.in, tt.labels, result, tt.out)
			}
		})
	}
}

func TestValidateLabels(t *testing.T) {
	descriptors := []*dpb.LabelDescriptor{
		{Name: "stringlabel", ValueType: dpb.STRING},
	}

	tests := []struct {
		name   string
		labels map[string]string
		descs  []*dpb.LabelDescriptor
		err    string
	}{
		{"no labels", map[string]string{}, []*dpb.LabelDescriptor{}, ""},
		{"one label", map[string]string{"stringlabel": "string"}, descriptors, ""},
		{"two labels", map[string]string{"stringlabel": "string", "durationlabel": "duration"}, []*dpb.LabelDescriptor{
			{Name: "stringlabel", ValueType: dpb.STRING},
			{Name: "durationlabel", ValueType: dpb.DURATION},
		}, ""},
		{"missing label", map[string]string{"missing": "not a label"}, descriptors, "wrong dimensions"},
		{"cardinality mismatch", map[string]string{"stringlabel": "string", "string2": "string"}, descriptors, "wrong dimensions"},
		{"type eval error", map[string]string{"stringlabel": "string |"}, descriptors, "error type checking label"},
		{"type doesn't match desc", map[string]string{"stringlabel": "duration"}, descriptors, "expected type STRING"},
	}
	df := test.NewDescriptorFinder(map[string]interface{}{
		"duration": &dpb.AttributeDescriptor{Name: "duration", ValueType: dpb.DURATION},
		"string":   &dpb.AttributeDescriptor{Name: "string", ValueType: dpb.STRING},
		"int64":    &dpb.AttributeDescriptor{Name: "int64", ValueType: dpb.INT64},
	})
	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			if err := validateLabels(tt.name, tt.labels, tt.descs, expr.NewCEXLEvaluator(), df); err != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf("validateLabels() = '%s', wanted no err", err.Error())
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%s'", tt.err, err.Error())
				}
			}
		})
	}
}

func TestValidateTemplateExpressions(t *testing.T) {
	df := test.NewDescriptorFinder(map[string]interface{}{
		"duration": &dpb.AttributeDescriptor{Name: "duration", ValueType: dpb.DURATION},
		"string":   &dpb.AttributeDescriptor{Name: "string", ValueType: dpb.STRING},
		"int64":    &dpb.AttributeDescriptor{Name: "int64", ValueType: dpb.INT64},
	})

	tests := []struct {
		name  string
		exprs map[string]string
		err   string
	}{
		{"valid", map[string]string{"1": "duration", "2": "string", "3": "int64"}, ""},
		{"missing attr", map[string]string{"not present": "timestamp"}, "failed to parse expression"},
		{"invalid expr", map[string]string{"invalid": "string |"}, "failed to parse expression"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			if err := validateTemplateExpressions(tt.name, tt.exprs, expr.NewCEXLEvaluator(), df); err != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf("validateTemplateExpressions() = '%s', wanted no err", err.Error())
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%s'", tt.err, err.Error())
				}
			}
		})
	}
}
