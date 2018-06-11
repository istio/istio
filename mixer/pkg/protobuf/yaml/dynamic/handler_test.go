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
	"testing"
	"istio.io/istio/mixer/pkg/lang/compiled"
	protoyaml "istio.io/istio/mixer/pkg/protobuf/yaml"
	"istio.io/istio/mixer/pkg/adapter"
	"github.com/gogo/protobuf/types"
	"istio.io/istio/mixer/template/metric"
	attributeV1beta1 "istio.io/api/policy/v1beta1"
	"fmt"
	"istio.io/istio/mixer/template/listentry"
	"istio.io/istio/mixer/template/quota"
	spy "istio.io/istio/mixer/test/spybackend"
	"istio.io/api/mixer/adapter/model/v1beta1"
	"strings"
)

func TestEncodeCheckRequest(t *testing.T) {
	fds, err := protoyaml.GetFileDescSet("../../../../template/metric/template_handler_service.descriptor_set")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	compiler := compiled.NewBuilder(StatdardVocabulary())
	res := protoyaml.NewResolver(fds)

	b := NewEncoderBuilder(res, compiler, true)
	var inst *Svc
	adapterConfig := &types.Any{
		TypeUrl: "@abc",
		Value:   []byte("abcd"),
	}

	hh := &adapter.DynamicHandler{
		Adapter: &adapter.Dynamic{},
		AdapterConfig: adapterConfig,
	}

	if inst, err = RemoteAdapterSvc("", res, hh); err != nil {
		t.Fatalf("failed to get service:%v", err)
	}

	var re *RequestEncoder
	re, err = buildRequestEncoder(b, inst.InputType, &adapter.DynamicHandler{
		Adapter: &adapter.Dynamic{

		},
		AdapterConfig: adapterConfig,
	})

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
		DedupId: dedupString,
	}

	got := &metric.HandleMetricRequest{}

	eed0, _ := ed0.Marshal()
	eed1, _ := ed1.Marshal()

	br, err1 := re.encodeRequest(nil, dedupString, eed0, eed1)
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

func loadDynamicInstance(t *testing.T, name string, variety v1beta1.TemplateVariety) *adapter.DynamicInstance {
	t.Helper()
	path := fmt.Sprintf("../../../../template/%s/template_handler_service.descriptor_set", name)
	fds, err := protoyaml.GetFileDescSet(path)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	// ../../template/listentry/template_handler_service.descriptor_set
	return &adapter.DynamicInstance{
		Name: name,
		Template: &adapter.DynamicTemplate{
			Name: name,
			Variety: variety,
			FileDescSet: fds,
		},
	}
}

func validateNoSessionBackend(s *spy.NoSessionServer, t *testing.T) error {
	listentryDi := loadDynamicInstance(t, "listentry", v1beta1.TEMPLATE_VARIETY_CHECK)
	metricDi := loadDynamicInstance(t, "metric", v1beta1.TEMPLATE_VARIETY_REPORT)
	quotaDi := loadDynamicInstance(t, "quota", v1beta1.TEMPLATE_VARIETY_QUOTA)

	adapterConfig := &types.Any{
		TypeUrl: "@abc",
		Value:   []byte("abcd"),
	}
	handlerConfig := &adapter.DynamicHandler{
		Name: "spy",
		Connection: &attributeV1beta1.Connection{Address: s.Addr().String()},
		Adapter: &adapter.Dynamic{
			SessionBased: false,
		},
		AdapterConfig: adapterConfig,
	}

	h, err := BuildHandler(handlerConfig, []*adapter.DynamicInstance{listentryDi, metricDi, quotaDi}, nil)
	if err != nil {
		t.Fatalf("unable to build handler: %v", err)
	}

	defer func() {
		_ = h.Close()
	}()
	// check
	linst := &listentry.InstanceMsg{
		Name: "n1",
		Value: "v1",
	}
	linstBa, _ := linst.Marshal()
	le, err := h.HandleRemoteCheck(context.Background(), linstBa, listentryDi.Name)
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
	if err = h.HandleRemoteReport(context.Background(), [][]byte{minstBa}, metricDi.Name);err != nil {
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
	qe, err := h.HandleRemoteQuota(context.Background(), qinstBa, &adapter.QuotaArgs{}, quotaDi.Name)
	if err != nil {
		t.Fatalf("HandleRemoteCheck returned: %v", err)
	}
	expectEqual(qinst, s.Requests.HandleQuotaRequest[0].Instance, t)
	expectEqual(qe, asAdapterQuotaResult(s.Behavior.HandleQuotaResult, quotaDi.Name), t)

	return nil
}
func asAdapterQuotaResult(qRes *v1beta1.QuotaResult, qname string) *adapter.QuotaResult {
	return &adapter.QuotaResult{
		ValidDuration: qRes.Quotas[qname].ValidDuration,
		Amount:        qRes.Quotas[qname].GrantedAmount,
	}
}

func asAdapterCheckResult(result *v1beta1.CheckResult) *adapter.CheckResult {
	return &adapter.CheckResult{
		Status: result.Status,
		ValidUseCount: result.ValidUseCount,
		ValidDuration: result.ValidDuration,
	}
}

func TestCodecErrors(t *testing.T) {
	c := Codec{}
	t.Run(c.String() +".marshalError", func(t *testing.T) {
		if _, err := c.Marshal("ABC"); err != nil {
			if !strings.Contains(err.Error(), "unable to marshal") {
				t.Errorf("incorrect error: %v", err)
			}
		} else {
			t.Errorf("exepcted marshal to fail")
		}
	})
	t.Run(c.String() +".unMarshalError", func(t *testing.T) {
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

func TestBuildHandler_ConnectError(t *testing.T) {
	handlerConfig := &adapter.DynamicHandler{
		Name: "spy",
		Connection: &attributeV1beta1.Connection{Address: ""},
		Adapter: &adapter.Dynamic{
			SessionBased: false,
		},
	}

	h, err := BuildHandler(handlerConfig, []*adapter.DynamicInstance{}, nil)
	if err != nil {
		t.Fatalf("unable to build handler: %v", err)
	}
	h.Close()
}