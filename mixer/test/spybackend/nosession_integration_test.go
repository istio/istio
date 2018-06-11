// Copyright 2018 Istio Authors
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

package spybackend

import (
	"context"
	"fmt"
	"testing"
	diff "gopkg.in/d4l3k/messagediff.v1"

	"istio.io/api/mixer/adapter/model/v1beta1"
	attributeV1beta1 "istio.io/api/policy/v1beta1"
	adapter_integration "istio.io/istio/mixer/pkg/adapter/test"
	protoyaml "istio.io/istio/mixer/pkg/protobuf/yaml"
	"istio.io/istio/mixer/pkg/protobuf/yaml/dynamic"
	"istio.io/istio/mixer/template/listentry"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/quota"
	"istio.io/istio/mixer/pkg/adapter"
	"github.com/gogo/protobuf/types"
)

// This test for now just validates the backend can be started and tested against. This is will be used to verify
// the OOP adapter work. As various features start lighting up, this test will grow.

func TestNoSessionBackend(t *testing.T) {
	adapter_integration.RunTest(
		t,
		nil,
		adapter_integration.Scenario{
			Setup: func() (interface{}, error) {
				args := defaultArgs()
				args.behavior.handleMetricResult = &v1beta1.ReportResult{}
				args.behavior.handleListEntryResult = &v1beta1.CheckResult{ValidUseCount: 31}
				args.behavior.handleQuotaResult = &v1beta1.QuotaResult{Quotas: map[string]v1beta1.QuotaResult_Result{"key1": {GrantedAmount: 32}}}

				var s server
				var err error
				if s, err = newNoSessionServer(args); err != nil {
					return nil, err
				}
				s.Run()
				t.Logf("Started server at: %v", s.Addr())
				return s, nil
			},
			Teardown: func(ctx interface{}) {
				_ = ctx.(server).Close()
			},
			GetState: func(ctx interface{}) (interface{}, error) {
				return nil, validateNoSessionBackend(ctx, t)
			},
			ParallelCalls: []adapter_integration.Call{},
			Configs:       []string{},
			Want: `{
              "AdapterState": null,
		      "Returns": []
            }`,
		},
	)
}

func loadDynamicInstance(t *testing.T, name string, variety v1beta1.TemplateVariety) *adapter.DynamicInstance {
	t.Helper()
	path := fmt.Sprintf("../../template/%s/template_handler_service.descriptor_set", name)
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

func validateNoSessionBackend(ctx interface{}, t *testing.T) error {
	s := ctx.(*noSessionServer)
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

	h, err := dynamic.BuildHandler(handlerConfig, []*adapter.DynamicInstance{listentryDi, metricDi, quotaDi}, nil)
	if err != nil {
		t.Fatalf("unable to build handler: %v", err)
	}

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

	expectEqual(linst, s.requests.handleListEntryRequest[0].Instance, t)
	expectEqual(le, asAdapterCheckResult(s.behavior.handleListEntryResult), t)

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
	expectEqual(minst, s.requests.handleMetricRequest[0].Instances[0], t)



	return nil
}

func asAdapterCheckResult(result *v1beta1.CheckResult) *adapter.CheckResult {
	return &adapter.CheckResult{
		Status: result.Status,
		ValidUseCount: result.ValidUseCount,
		ValidDuration: result.ValidDuration,
	}
}

func validateHandleCalls(metricClt metric.HandleMetricServiceClient,
	listentryClt listentry.HandleListEntryServiceClient, quotaClt quota.HandleQuotaServiceClient, req *requests) error {
	if _, err := metricClt.HandleMetric(context.Background(), &metric.HandleMetricRequest{}); err != nil {
		return err
	}
	if le, err := listentryClt.HandleListEntry(context.Background(), &listentry.HandleListEntryRequest{}); err != nil {
		return err
	} else if le.ValidUseCount != 31 {
		return fmt.Errorf("got listentry.ValidUseCount %v; want %v", le.ValidUseCount, 31)
	}
	if qr, err := quotaClt.HandleQuota(context.Background(), &quota.HandleQuotaRequest{}); err != nil {
		return err
	} else if qr.Quotas["key1"].GrantedAmount != 32 {
		return fmt.Errorf("got quota.GrantedAmount %v; want %v", qr.Quotas["key1"].GrantedAmount, 31)
	}

	if len(req.handleQuotaRequest) != 1 {
		return fmt.Errorf("got quota calls %d; want %d", len(req.handleQuotaRequest), 1)
	}
	if len(req.handleMetricRequest) != 1 {
		return fmt.Errorf("got metric calls %d; want %d", len(req.handleMetricRequest), 1)
	}
	if len(req.handleListEntryRequest) != 1 {
		return fmt.Errorf("got listentry calls %d; want %d", len(req.handleListEntryRequest), 1)
	}
	return nil
}

func expectEqual(got interface{}, want interface{}, t *testing.T) {
	t.Helper()
	s, equal:= diff.PrettyDiff(got, want)
	if equal {
		return
	}

	t.Logf("difference: %s", s)
	t.Fatalf("\n got: %v\nwant: %v", got, want)
}
