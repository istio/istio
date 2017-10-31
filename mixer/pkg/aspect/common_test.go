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
	"fmt"
	"reflect"
	"strings"
	"testing"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/istio/mixer/adapter/noopLegacy"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/aspect/test"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/config"
	cfgpb "istio.io/istio/mixer/pkg/config/proto"
	"istio.io/istio/mixer/pkg/expr"
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

func TestValidateLabels(t *testing.T) {
	descriptors := map[string]dpb.ValueType{
		"stringlabel": dpb.STRING,
	}

	tests := []struct {
		name   string
		labels map[string]string
		descs  map[string]dpb.ValueType
		err    string
	}{
		{"no labels", map[string]string{}, map[string]dpb.ValueType{}, ""},
		{"one label", map[string]string{"stringlabel": "string"}, descriptors, ""},
		{"two labels", map[string]string{"stringlabel": "string", "durationlabel": "duration"}, map[string]dpb.ValueType{
			"stringlabel":   dpb.STRING,
			"durationlabel": dpb.DURATION,
		}, ""},
		{"missing label", map[string]string{"missing": "not a label"}, descriptors, "wrong dimensions"},
		{"cardinality mismatch", map[string]string{"stringlabel": "string", "string2": "string"}, descriptors, "wrong dimensions"},
		{"type eval error", map[string]string{"stringlabel": "string |"}, descriptors, "error type checking label"},
		{"type doesn't match desc", map[string]string{"stringlabel": "duration"}, descriptors, "expected type STRING"},
	}
	dfind := test.NewDescriptorFinder(map[string]interface{}{
		"duration": &cfgpb.AttributeManifest_AttributeInfo{ValueType: dpb.DURATION},
		"string":   &cfgpb.AttributeManifest_AttributeInfo{ValueType: dpb.STRING},
		"int64":    &cfgpb.AttributeManifest_AttributeInfo{ValueType: dpb.INT64},
	})
	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			eval, _ := expr.NewCEXLEvaluator(expr.DefaultCacheSize)
			if err := validateLabels(tt.name, tt.labels, tt.descs, eval, dfind); err != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf("validateLabels() = '%s', wanted no err", err.Error())
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%s'", tt.err, err.Error())
				}
			}
		})
	}
}

type testhandler struct{ int }

func (testhandler) Close() error { return nil }

func TestFromHandler(t *testing.T) {
	f := FromHandler(testhandler{37})
	asp, err := f(nil, nil)
	if err != nil {
		t.Fatalf("FromHandler should never return an error")
	}
	if th, ok := asp.(testhandler); !ok {
		t.Fatalf("FromHandler changed type of Handler instance")
	} else if th.int != 37 {
		t.Fatalf("Handler instance was messed with by FromHandler, that should never happen")
	}
}

func TestFromBuilder(t *testing.T) {
	tests := []struct {
		name         string
		kind         config.Kind
		expectedType reflect.Type // we only compare the type of this value against the one returned by the builder
		err          string
		args         []interface{}
	}{
		{"attr gen", config.AttributesKind, reflect.TypeOf((*adapter.AttributesGenerator)(nil)).Elem(), "", []interface{}{}},
		{"quota no args", config.QuotasKind, reflect.TypeOf((*adapter.QuotasAspect)(nil)).Elem(),
			"quota builders must have configuration args",
			[]interface{}{}},
		{"quota", config.QuotasKind, reflect.TypeOf((*adapter.QuotasAspect)(nil)).Elem(),
			"",
			[]interface{}{map[string]*adapter.QuotaDefinition{}}},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.name), func(t *testing.T) {
			f, err := FromBuilder(noopLegacy.Builder{}, tt.kind)
			if err != nil {
				t.Fatalf("failed to construct CreateAspectFunc from builder unexpectedly")
			}
			out, err := f(nil, nil, tt.args...)
			if err != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf("FromBuilder(noop.Builder{})(nil, nil) = '%s', wanted no err", err.Error())
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%s'", tt.err, err.Error())
				}
				return // we can't check out when we get an err.
			}
			if outType := reflect.TypeOf(out); !outType.Implements(tt.expectedType) {
				t.Fatalf("TypeOf(%v) = %v; expected something that implements %v", out, outType, tt.expectedType)
			}
		})
	}

}

type fakeDeny struct{ adapter.Builder }

func (fakeDeny) NewDenialsAspect(adapter.Env, adapter.Config) (adapter.DenialsAspect, error) {
	return nil, nil
}

func TestFromBuilder_Errors(t *testing.T) {
	tests := []struct {
		builder adapter.Builder
		kind    config.Kind
		err     string
	}{
		{&fakeDeny{}, config.AttributesKind, "invalid builder"},
		{&fakeDeny{}, config.QuotasKind, "invalid builder"},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("%d", idx), func(t *testing.T) {
			if _, err := FromBuilder(tt.builder, tt.kind); err != nil || tt.err != "" {
				if tt.err == "" {
					t.Fatalf(" = '%s', wanted no err", err.Error())
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("Expected errors containing the string '%s', actual: '%s'", tt.err, err.Error())
				}
			}
		})
	}

}
