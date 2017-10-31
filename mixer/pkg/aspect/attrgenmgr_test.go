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
	"net"
	"reflect"
	"testing"

	dpb "istio.io/api/mixer/v1/config/descriptor"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/adapter/test"
	apb "istio.io/mixer/pkg/aspect/config"
	atest "istio.io/mixer/pkg/aspect/test"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	cfgpb "istio.io/mixer/pkg/config/proto"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/status"
)

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

	df = atest.NewDescriptorFinder(map[string]interface{}{
		"request_count":   requestCountDesc,
		"request_latency": requestLatencyDesc,
	})
)

func TestAttributeGeneratorManager(t *testing.T) {
	m := newAttrGenMgr()
	if m.Kind() != config.AttributesKind {
		t.Errorf("m.Kind() = %s; wanted %s", m.Kind(), config.AttributesKindName)
	}
	eval, _ := expr.NewCEXLEvaluator(expr.DefaultCacheSize)
	if err := m.ValidateConfig(m.DefaultConfig(), eval, nil); err != nil {
		t.Errorf("ValidateConfig(DefaultConfig()) produced an error: %v", err)
	}
	if err := m.ValidateConfig(&apb.AttributesGeneratorParams{}, eval, df); err != nil {
		t.Error("ValidateConfig(AttributeGeneratorsParams{}) should not produce an error.")
	}
}

func TestAttrGenMgr_ValidateConfig(t *testing.T) {

	dfind := atest.NewDescriptorFinder(map[string]interface{}{
		"int64":     &cfgpb.AttributeManifest_AttributeInfo{ValueType: dpb.INT64},
		"duration":  &cfgpb.AttributeManifest_AttributeInfo{ValueType: dpb.DURATION},
		"source_ip": &cfgpb.AttributeManifest_AttributeInfo{},
	})

	validExpr := &apb.AttributesGeneratorParams{
		InputExpressions: map[string]string{
			"valid_int":      "42 | int64",
			"valid_duration": "\"42ms\" | duration",
		},
	}

	badExpr := &apb.AttributesGeneratorParams{
		InputExpressions: map[string]string{
			"bad_expr": "int64 | duration",
		},
	}

	foundAttr := &apb.AttributesGeneratorParams{
		AttributeBindings: map[string]string{"source_ip": "srcPodIP"},
	}

	notFoundAttr := &apb.AttributesGeneratorParams{
		AttributeBindings: map[string]string{"not_found": "srcPodIP"},
	}

	tests := []struct {
		name    string
		params  *apb.AttributesGeneratorParams
		wantErr bool
	}{
		{"valid input expressions", validExpr, false},
		{"invalid input expressions", badExpr, true},
		{"valid attribute binding", foundAttr, false},
		{"invalid attribute binding", notFoundAttr, true},
	}

	m := newAttrGenMgr()

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			eval, _ := expr.NewCEXLEvaluator(expr.DefaultCacheSize)
			err := m.ValidateConfig(v.params, eval, dfind)
			if err != nil && !v.wantErr {
				t.Errorf("Unexpected error '%v' for config: %#v", err, v.params)
			}
			if err == nil && v.wantErr {
				t.Errorf("Expected error for config: %#v", v.params)
			}
		})
	}
}

type testAttrGen struct {
	adapter.AttributesGenerator

	out       map[string]interface{}
	closed    bool
	returnErr bool
}

type testAttrGenBuilder struct {
	adapter.DefaultBuilder
	returnErr bool
}

func newTestAttrGenBuilder(returnErr bool) testAttrGenBuilder {
	return testAttrGenBuilder{adapter.NewDefaultBuilder("test", "test", nil), returnErr}
}

func (t testAttrGenBuilder) BuildAttributesGenerator(env adapter.Env, c adapter.Config) (adapter.AttributesGenerator, error) {
	if t.returnErr {
		return nil, errors.New("error")
	}
	return &testAttrGen{}, nil
}

func TestAttributeGeneratorManager_NewPreprocessExecutor(t *testing.T) {
	tests := []struct {
		name    string
		builder adapter.Builder
		wantErr bool
	}{
		{"no error", newTestAttrGenBuilder(false), false},
		{"build error", newTestAttrGenBuilder(true), true},
	}

	m := newAttrGenMgr()
	c := &cfgpb.Combined{
		Builder: &cfgpb.Adapter{Params: &apb.AttributesGeneratorParams{}},
		Aspect: &cfgpb.Aspect{Params: &apb.AttributesGeneratorParams{
			AttributeBindings: map[string]string{"service_found": "found", "source_service": "srcSvc"},
		}},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			f, _ := FromBuilder(v.builder, config.AttributesKind)
			exec, err := m.NewPreprocessExecutor(c, f, test.NewEnv(t), nil)
			if err == nil && v.wantErr {
				t.Error("Expected to receive error")
			}
			if err != nil {
				if !v.wantErr {
					t.Errorf("Unexpected error: %v", err)
				}
				return
			}
			for attrName, valName := range c.Aspect.Params.(*apb.AttributesGeneratorParams).AttributeBindings {
				found := false
				for boundVal, boundAttr := range exec.(*attrGenExec).bindings {
					if boundVal == valName && boundAttr == attrName {
						found = true
						break
					}
				}
				if found {
					continue
				}
				t.Errorf("Bindings map missing binding from %s to %s", valName, attrName)
			}
		})
	}
}

func (t testAttrGen) Generate(map[string]interface{}) (map[string]interface{}, error) {
	if t.returnErr {
		return nil, errors.New("generate error")
	}
	return t.out, nil
}

func TestAttributeGeneratorExecutor_Execute(t *testing.T) {

	genParams := &apb.AttributesGeneratorParams{
		InputExpressions: map[string]string{"pod.ip": "source_ip"},
		AttributeBindings: map[string]string{
			"service_found":  "found",
			"source_service": "srcSvc",
			"destination_ip": "destIP",
			"ip_v6":          "v6IP",
		},
	}

	bMap := map[string]string{"found": "service_found", "srcSvc": "source_service", "destIP": "destination_ip", "v6IP": "ip_v6"}

	inBag := attribute.GetFakeMutableBagForTesting(map[string]interface{}{"source_ip": []byte(net.IP("10.1.1.10").To4())})

	outMap := map[string]interface{}{
		"found":  true,
		"srcSvc": "service1",
		"destIP": net.ParseIP("10.34.23.3"),
		"v6IP":   net.ParseIP("2001:db8::1"),
	}
	wantBag := attribute.GetMutableBag(nil)
	wantBag.Set("service_found", true)
	wantBag.Set("source_service", "service1")
	wantBag.Set("destination_ip", []byte{0xa, 0x22, 0x17, 0x3})
	wantBag.Set("ip_v6", []byte{0x20, 0x1, 0xd, 0xb8, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1})

	extraOutMap := map[string]interface{}{
		"found":            true,
		"srcSvc":           "service1",
		"destIP":           net.ParseIP("10.34.23.3"),
		"v6IP":             net.ParseIP("2001:db8::1"),
		"shouldBeStripped": "never_used",
	}

	tests := []struct {
		name      string
		exec      attrGenExec
		attrs     attribute.Bag
		eval      expr.Evaluator
		wantAttrs attribute.Bag
		wantErr   bool
	}{
		{"no error", attrGenExec{&testAttrGen{out: outMap}, genParams, bMap}, inBag, atest.NewIDEval(), wantBag, false},
		{"strippped attrs", attrGenExec{&testAttrGen{out: extraOutMap}, genParams, bMap}, inBag, atest.NewIDEval(), wantBag, false},
		{"generate error", attrGenExec{&testAttrGen{out: outMap, returnErr: true}, genParams, bMap}, inBag, atest.NewIDEval(), wantBag, true},
		{"eval error", attrGenExec{&testAttrGen{}, genParams, bMap}, inBag, atest.NewErrEval(), wantBag, true},
	}

	for _, v := range tests {
		t.Run(v.name, func(t *testing.T) {
			got, s := v.exec.Execute(v.attrs, v.eval)
			if status.IsOK(s) && v.wantErr {
				t.Fatal("Expected to receive error")
			}
			if !status.IsOK(s) {
				if !v.wantErr {
					t.Fatalf("Unexpected status returned: %v", s)
				}
				return
			}
			for _, n := range v.wantAttrs.Names() {
				wantVal, _ := v.wantAttrs.Get(n)
				gotVal, ok := got.Attrs.Get(n)
				if !ok {
					t.Errorf("Generated attribute.Bag missing attribute %s", n)
				}
				if !reflect.DeepEqual(gotVal, wantVal) {
					t.Errorf("For attribute '%s': got value %v, want %v", n, gotVal, wantVal)
				}
			}
		})
	}
}

func (t *testAttrGen) Close() error {
	t.closed = true
	return nil
}

func TestAttributeGeneratorExecutor_Close(t *testing.T) {
	inner := &testAttrGen{closed: false}
	executor := &attrGenExec{aspect: inner}
	if err := executor.Close(); err != nil {
		t.Errorf("Close() returned an error: %v", err)
	}
	if !inner.closed {
		t.Error("Close() should propagate to wrapped aspect.")
	}
}
