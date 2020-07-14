// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copyAttrs of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sample

import (
	"bytes"
	"context"
	"errors"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/lang/compiled"
	"istio.io/istio/mixer/pkg/template"
	sample_apa "istio.io/istio/mixer/template/sample/apa"
	sample_check "istio.io/istio/mixer/template/sample/check"
	sample_quota "istio.io/istio/mixer/template/sample/quota"
	sample_report "istio.io/istio/mixer/template/sample/report"
	"istio.io/pkg/attribute"
)

func TestDispatchReport_Success(t *testing.T) {
	h := &mockHandler{}
	err := executeDispatchReport(t, h)
	if err != nil {
		t.Fatalf("Unexpected error found: '%v'", err)
	}
	if len(h.instances) != 1 {
		t.Fatalf("Unexpected number of instances: %d", len(h.instances))
	}
	if instance, ok := h.instances[0].(*sample_report.Instance); !ok || instance.Name != "instance1" {
		t.Fatalf("Unexpected instance: %+v", h.instances[0])
	}
}

func TestDispatchReport_Failure(t *testing.T) {
	h := &mockHandler{err: errors.New("handler is apathetic to your news")}

	err := executeDispatchReport(t, h)
	if err == nil {
		t.Fatal("expected error not found")
	}
}

func TestDispatchCheck_Success(t *testing.T) {
	h := &mockHandler{checkResult: adapter.CheckResult{
		ValidUseCount: 23,
	}}

	r, err := executeDispatchCheck(t, h)
	if err != nil {
		t.Fatalf("Unexpected error found: '%v'", err)
	}
	if len(h.instances) != 1 {
		t.Fatalf("Unexpected number of instances: %d", len(h.instances))
	}
	if instance, ok := h.instances[0].(*sample_check.Instance); !ok || instance.Name != "instance1" {
		t.Fatalf("Unexpected instance: %+v", h.instances[0])
	}

	if !reflect.DeepEqual(h.checkResult, r) {
		t.Fatalf("The check result was not propagated correctly.")
	}
}

func TestDispatchCheck_Failure(t *testing.T) {
	h := &mockHandler{err: errors.New("you shall not pass")}

	_, err := executeDispatchCheck(t, h)
	if err == nil {
		t.Fatal("expected error not found")
	}
}

func TestDispatchQuota_Success(t *testing.T) {
	h := &mockHandler{quotaResult: adapter.QuotaResult{
		Amount:        64,
		ValidDuration: 25 * time.Second,
	}}

	a := adapter.QuotaArgs{BestEffort: true, DeduplicationID: "dedupe", QuotaAmount: 54}

	r, err := executeDispatchQuota(t, h, a)
	if err != nil {
		t.Fatalf("Unexpected error found: '%v'", err)
	}
	if len(h.instances) != 1 {
		t.Fatalf("Unexpected number of instances: %d", len(h.instances))
	}
	if instance, ok := h.instances[0].(*sample_quota.Instance); !ok || instance.Name != "instance1" {
		t.Fatalf("Unexpected instance: %+v", h.instances[0])
	}
	if h.quotaArgs != a {
		t.Fatalf("The quota args was not propagated correctly")
	}

	if !reflect.DeepEqual(h.quotaResult, r) {
		t.Fatalf("The quota result was not propagated correctly.")
	}
}

func TestDispatchQuota_Failure(t *testing.T) {
	h := &mockHandler{err: errors.New("handler is apathetic to your demands")}

	a := adapter.QuotaArgs{BestEffort: true, DeduplicationID: "dedupe", QuotaAmount: 54}

	_, err := executeDispatchQuota(t, h, a)
	if err == nil {
		t.Fatal("expected error not found")
	}
}

var sampleApaInstanceParam = sample_apa.InstanceParam{
	StringPrimitive:                "as",
	Int64Primitive:                 "ai",
	TimeStamp:                      "ats",
	DoublePrimitive:                "ad",
	BoolPrimitive:                  "ab",
	Duration:                       "adr",
	Email:                          "`email@email`",
	OptionalIP:                     `ip("0.0.0.0")`,
	DimensionsFixedInt64ValueDType: map[string]string{"ai2": "ai2"},
	Res3Map: map[string]*sample_apa.Resource3InstanceParam{
		"r3": {
			StringPrimitive:                "as2",
			Duration:                       "adr",
			TimeStamp:                      "ats",
			BoolPrimitive:                  "ab",
			DoublePrimitive:                "ad",
			Int64Primitive:                 "ai3",
			DimensionsFixedInt64ValueDType: map[string]string{"ai4": "ai4"},
		},
	},
	AttributeBindings: map[string]string{
		"generated.ai": "$out.int64Primitive",
		"generated.as": "$out.stringPrimitive",
		"generated.ip": "$out.out_ip",
	},
}

func TestDispatchGenAttrs_Success(t *testing.T) {

	out := sample_apa.NewOutput()
	out.SetStringPrimitive("This is an output")
	out.SetInt64Primitive(defaultApaAttributes["ai"].(int64))
	out.SetOutIp(net.ParseIP("2.3.4.5"))

	h := &mockHandler{
		apaOutput: *out,
	}

	bag := attribute.GetMutableBagForTesting(defaultApaAttributes)
	f := attribute.NewFinder(combineManifests(defaultAttributeInfos, SupportedTmplInfo[sample_apa.TemplateName].AttributeManifests...))
	builder := compiled.NewBuilder(f)
	expressions, err := SupportedTmplInfo[sample_apa.TemplateName].CreateOutputExpressions(&sampleApaInstanceParam, f, builder)
	if err != nil {
		t.Fatalf("Unexpected CreateOutputExpressions error: %v", err)
	}

	mapper := template.NewOutputMapperFn(expressions)

	outBag, err := executeDispatchGenAttrs(t, h, bag, mapper)
	if err != nil {
		t.Fatalf("Unexpected error found: '%v'", err)
	}
	if len(h.instances) != 1 {
		t.Fatalf("Unexpected number of instances: %d", len(h.instances))
	}
	if instance, ok := h.instances[0].(*sample_apa.Instance); !ok || instance.Name != "instance1" {
		t.Fatalf("Unexpected instance: %+v", h.instances[0])
	}
	if ai, ok := outBag.Get("generated.ai"); !ok || ai != defaultApaAttributes["ai"] {
		t.Fatalf("Expected attribute not found or different than expected: %v != %v", ai, defaultApaAttributes["ai"])
	}

	if ai, ok := outBag.Get("generated.ip"); !ok || !bytes.Equal(ai.([]byte), []byte(net.ParseIP("2.3.4.5"))) {
		t.Fatalf("Expected attribute not found or different than expected: %v != %v", ai, "2.3.4.5")
	}
}

func TestDispatchGenAttrs_Failure(t *testing.T) {
	h := &mockHandler{err: errors.New("handler is not interested in generating attributes")}

	bag := attribute.GetMutableBagForTesting(defaultApaAttributes)
	mapper := template.NewOutputMapperFn(map[string]compiled.Expression{})

	_, err := executeDispatchGenAttrs(t, h, bag, mapper)
	if err == nil {
		t.Fatal("expected error not found")
	}
}

func executeDispatchReport(t *testing.T, h adapter.Handler) error {
	instance := createInstance(t, sample_report.TemplateName, &defaultReportInstanceParam, defaultReportAttributes)
	return SupportedTmplInfo[sample_report.TemplateName].DispatchReport(context.TODO(), h, []interface{}{instance})
}

func executeDispatchCheck(t *testing.T, h adapter.Handler) (adapter.CheckResult, error) {
	instance := createInstance(t, sample_check.TemplateName, &defaultCheckInstanceParam, defaultCheckAttributes)
	return SupportedTmplInfo[sample_check.TemplateName].DispatchCheck(context.TODO(), h, instance, nil, "")
}

func executeDispatchQuota(t *testing.T, h adapter.Handler, a adapter.QuotaArgs) (adapter.QuotaResult, error) {
	instance := createInstance(t, sample_quota.TemplateName, &defaultQuotaInstanceParam, defaultQuotaAttributes)
	return SupportedTmplInfo[sample_quota.TemplateName].DispatchQuota(context.TODO(), h, instance, a)
}

func executeDispatchGenAttrs(t *testing.T, h adapter.Handler, bag attribute.Bag, mapper template.OutputMapperFn) (*attribute.MutableBag, error) {
	instance := createInstance(t, sample_apa.TemplateName, &defaultApaInstanceParam, defaultApaAttributes)
	return SupportedTmplInfo[sample_apa.TemplateName].DispatchGenAttrs(context.TODO(), h, instance, bag, mapper)
}

func createInstance(t *testing.T, template string, instanceParam proto.Message, attrs map[string]interface{}) interface{} {
	expb := compiled.NewBuilder(attribute.NewFinder(defaultAttributeInfos))
	builder, e := SupportedTmplInfo[template].CreateInstanceBuilder(
		"instance1", instanceParam, expb)
	if e != nil {
		t.Fail()
	}

	bag := attribute.GetMutableBagForTesting(attrs)
	instance, e2 := builder(bag)
	if e2 != nil {
		t.Fail()
	}

	return instance
}

type mockHandler struct {
	instances   []interface{}
	quotaArgs   adapter.QuotaArgs
	quotaResult adapter.QuotaResult
	checkResult adapter.CheckResult
	apaOutput   sample_apa.Output
	err         error
}

var _ adapter.Handler = &mockHandler{}

func (h *mockHandler) Close() error {
	return nil
}
func (h *mockHandler) HandleReport(ctx context.Context, instances []*sample_report.Instance) error {
	for _, instance := range instances {
		h.instances = append(h.instances, instance)
	}

	return h.err
}
func (h *mockHandler) HandleQuota(ctx context.Context, instance *sample_quota.Instance, qra adapter.QuotaArgs) (adapter.QuotaResult, error) {
	h.instances = []interface{}{instance}
	h.quotaArgs = qra
	return h.quotaResult, h.err
}
func (h *mockHandler) HandleCheck(ctx context.Context, instance *sample_check.Instance) (adapter.CheckResult, error) {
	h.instances = []interface{}{instance}
	return h.checkResult, h.err
}
func (h *mockHandler) GenerateMyApaAttributes(ctx context.Context, instance *sample_apa.Instance) (
	*sample_apa.Output, error) {

	h.instances = []interface{}{instance}
	return &h.apaOutput, h.err
}
