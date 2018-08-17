// Copyright 2018 Istio Authors.
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

package dynamic

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/gogo/protobuf/types"

	"istio.io/api/mixer/adapter/model/v1beta1"
	attributeV1beta1 "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	protoyaml "istio.io/istio/mixer/pkg/protobuf/yaml"
	"istio.io/istio/mixer/template/listentry"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/quota"
	spy "istio.io/istio/mixer/test/spybackend"
)

func TestEncodeReportRequest(t *testing.T) {
	var err error
	metricDi := loadInstance(t, "metric", v1beta1.TEMPLATE_VARIETY_REPORT)
	res := protoyaml.NewResolver(metricDi.FileDescSet)

	b := NewEncoderBuilder(res, nil, true)
	var inst *Svc
	adapterConfig := &types.Any{
		TypeUrl: "@abc",
		Value:   []byte("abcd"),
	}

	if inst, err = RemoteAdapterSvc("", res, false, adapterConfig, "tmpl"); err != nil {
		t.Fatalf("failed to get service:%v", err)
	}

	var me *messageEncoder
	me, err = buildRequestEncoder(b, inst.InputType, false, adapterConfig)

	if err != nil {
		t.Fatalf("unable build request encoder: %v", err)
	}

	dedupString := "dedupString"

	ed0 := &metric.InstanceMsg{
		Name: "inst0",
		Value: &attributeV1beta1.Value{
			Value: &attributeV1beta1.Value_StringValue{
				StringValue: "aaaaaaaaaaaaaaaa",
			},
		},
	}
	ed1 := &metric.InstanceMsg{
		Name: "inst1",
		Value: &attributeV1beta1.Value{
			Value: &attributeV1beta1.Value_DoubleValue{
				DoubleValue: float64(1.1111),
			},
		},
	}

	want := &metric.HandleMetricRequest{
		Instances: []*metric.InstanceMsg{
			ed0,
			ed1,
		},
		AdapterConfig: adapterConfig,
		DedupId:       dedupString,
	}

	got := &metric.HandleMetricRequest{}

	eed0, _ := ed0.Marshal()
	eed1, _ := ed1.Marshal()

	svc := &Svc{encoder: me}

	br, err1 := svc.encodeRequest(nil, dedupString, &adapter.EncodedInstance{Name: ed0.Name, Data: eed0},
		&adapter.EncodedInstance{Name: ed1.Name, Data: eed1})
	if err1 != nil {
		t.Fatalf("unable to encode request: %v", err1)
	}

	if err := got.Unmarshal(br); err != nil {
		wantba, _ := want.Marshal()
		t.Logf("\n got(%d):%v\nwant(%d):%v", len(br), br, len(wantba), wantba)
		t.Logf("\need0:%v\need1:%v", eed0, eed1)
		t.Fatalf("unable to unmarshal: %v", err)
	}

	expectEqual(got, want, t)
}

func TestNoSessionBackend(t *testing.T) {
	args := spy.DefaultArgs()
	args.Behavior.HandleMetricResult = &v1beta1.ReportResult{}
	args.Behavior.HandleListEntryResult = &v1beta1.CheckResult{ValidUseCount: 31}
	args.Behavior.HandleQuotaResult = &v1beta1.QuotaResult{Quotas: map[string]v1beta1.QuotaResult_Result{"quota": {GrantedAmount: 32}}}

	var s spy.Server
	var err error
	if s, err = spy.NewNoSessionServer(args); err != nil {
		t.Fatalf("unable to start Spy")
	}
	s.Run()
	defer func() {
		_ = s.Close()
	}()

	t.Logf("Started server at: %v", s.Addr())

	validateNoSessionBackend(s.(*spy.NoSessionServer), t)
}

func loadInstance(t *testing.T, name string, variety v1beta1.TemplateVariety) *TemplateConfig {
	t.Helper()
	path := fmt.Sprintf("../../../../template/%s/template_handler_service.descriptor_set", name)
	fds, err := protoyaml.GetFileDescSet(path)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	return &TemplateConfig{
		Name:         name,
		TemplateName: name,
		FileDescSet:  fds,
		Variety:      variety,
	}
}

func validateNoSessionBackend(s *spy.NoSessionServer, t *testing.T) {
	listentryDi := loadInstance(t, "listentry", v1beta1.TEMPLATE_VARIETY_CHECK)
	metricDi := loadInstance(t, "metric", v1beta1.TEMPLATE_VARIETY_REPORT)
	quotaDi := loadInstance(t, "quota", v1beta1.TEMPLATE_VARIETY_QUOTA)
	unknownQuota := &TemplateConfig{
		Name:         "unknownQuota",
		TemplateName: quotaDi.TemplateName,
		FileDescSet:  quotaDi.FileDescSet,
		Variety:      quotaDi.Variety,
	}

	adapterConfig := &types.Any{
		TypeUrl: "@abc",
		Value:   []byte("abcd"),
	}

	h, err := BuildHandler("spy",
		&attributeV1beta1.Connection{Address: s.Addr().String()}, false, adapterConfig,
		[]*TemplateConfig{listentryDi, metricDi, quotaDi, unknownQuota})

	if err != nil {
		t.Fatalf("unable to build handler: %v", err)
	}

	defer func() {
		_ = h.Close()
	}()
	// check
	linst := &listentry.InstanceMsg{
		Name:  "n1",
		Value: "v1",
	}
	linstBa, _ := linst.Marshal()
	le, err := h.HandleRemoteCheck(context.Background(), &adapter.EncodedInstance{Name: listentryDi.Name, Data: linstBa})
	if err != nil {
		t.Fatalf("HandleRemoteCheck returned: %v", err)
	}

	expectEqual(linst, s.Requests.HandleListEntryRequest[0].Instance, t)
	expectEqual(le, asAdapterCheckResult(s.Behavior.HandleListEntryResult), t)

	// report
	minst := &metric.InstanceMsg{
		Name: metricDi.Name,
		Value: &attributeV1beta1.Value{
			Value: &attributeV1beta1.Value_StringValue{
				StringValue: "aaaaaaaaaaaaaaaa",
			},
		},
	}
	minstBa, _ := minst.Marshal()
	mi := &adapter.EncodedInstance{
		Name: metricDi.Name,
		Data: minstBa,
	}
	if err = h.HandleRemoteReport(context.Background(), []*adapter.EncodedInstance{mi}); err != nil {
		t.Fatalf("HandleRemoteCheck returned: %v", err)
	}
	expectEqual(minst, s.Requests.HandleMetricRequest[0].Instances[0], t)

	// quota
	qinst := &quota.InstanceMsg{
		Name: quotaDi.Name,
		Dimensions: map[string]*attributeV1beta1.Value{
			"dim1": {
				Value: &attributeV1beta1.Value_StringValue{
					StringValue: "aaaaaaaaaaaaaaaa",
				},
			},
		},
	}
	qinstBa, _ := qinst.Marshal()
	qi := &adapter.EncodedInstance{
		Name: quotaDi.Name,
		Data: qinstBa,
	}
	qe, err := h.HandleRemoteQuota(context.Background(), qi, &adapter.QuotaArgs{})
	if err != nil {
		t.Fatalf("HandleRemoteCheck returned: %v", err)
	}
	expectEqual(qinst, s.Requests.HandleQuotaRequest[0].Instance, t)
	expectEqual(qe, asAdapterQuotaResult(s.Behavior.HandleQuotaResult, quotaDi.Name), t)

	unknownQi := &adapter.EncodedInstance{
		Name: unknownQuota.Name,
		Data: qinstBa,
	}
	_, err = h.HandleRemoteQuota(context.Background(), unknownQi, &adapter.QuotaArgs{})
	if err == nil || !strings.Contains(err.Error(), "did not respond with the requested quota") {
		t.Fatalf("HandleRemoteCheck unexpected error: got %v, want: no quota", err)
	}
}

func asAdapterQuotaResult(qRes *v1beta1.QuotaResult, qname string) *adapter.QuotaResult {
	return &adapter.QuotaResult{
		ValidDuration: qRes.Quotas[qname].ValidDuration,
		Amount:        qRes.Quotas[qname].GrantedAmount,
	}
}

func asAdapterCheckResult(result *v1beta1.CheckResult) *adapter.CheckResult {
	return &adapter.CheckResult{
		Status:        result.Status,
		ValidUseCount: result.ValidUseCount,
		ValidDuration: result.ValidDuration,
	}
}

func TestCodecErrors(t *testing.T) {
	c := Codec{}
	t.Run(c.String()+".marshalError", func(t *testing.T) {
		if _, err := c.Marshal("ABC"); err != nil {
			if !strings.Contains(err.Error(), "unable to marshal") {
				t.Errorf("incorrect error: %v", err)
			}
		} else {
			t.Errorf("exepcted marshal to fail")
		}
	})
	t.Run(c.String()+".unMarshalError", func(t *testing.T) {
		var ba []byte
		if err := c.Unmarshal(ba, "ABC"); err != nil {
			if !strings.Contains(err.Error(), "unable to unmarshal") {
				t.Errorf("incorrect error: %v ", err)
			}
		} else {
			t.Errorf("exepcted marshal to fail")
		}
	})
}

func TestStaticBag(t *testing.T) {
	b := &staticBag{
		v: map[string]interface{}{
			"attr1": "value",
		},
	}

	t.Run(b.String()+".Get", func(t *testing.T) {
		if v, _ := b.Get("attr1"); v == nil || v != "value" {
			t.Errorf("Get error got:value want:%v", v)
		}
	})

	t.Run(b.String()+".Names", func(t *testing.T) {
		if v := b.Names(); reflect.DeepEqual(v, []string{"value"}) {
			t.Errorf("Get error got:value want:%v", v)
		}
	})
	b.Done()
}

func TestBuildHandler_ConnectError(t *testing.T) {
	/*
		h, err := BuildHandler("spy", &attributeV1beta1.Connection{Address: ""}, false, []*adapter.DynamicInstance{}, false,)
		if err != nil {
			t.Fatalf("unable to build handler: %v", err)
		}
		h.Close()*/
}
