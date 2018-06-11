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
	"bytes"
	"context"
	"fmt"
	"testing"

	"google.golang.org/grpc"

	"istio.io/api/mixer/adapter/model/v1beta1"
	attributeV1beta1 "istio.io/api/policy/v1beta1"
	adapter_integration "istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/pkg/lang/ast"
	"istio.io/istio/mixer/pkg/lang/compiled"
	protoyaml "istio.io/istio/mixer/pkg/protobuf/yaml"
	"istio.io/istio/mixer/pkg/protobuf/yaml/dynamic"
	"istio.io/istio/mixer/template/listentry"
	"istio.io/istio/mixer/template/metric"
	"istio.io/istio/mixer/template/quota"
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

func validateNoSessionBackend(ctx interface{}, t *testing.T) error {
	s := ctx.(*noSessionServer)
	req := s.requests
	// Connect the client to Mixer
	codec := grpc.CallCustomCodec(ByteCodec{})
	conn, err := grpc.Dial(s.Addr().String(), grpc.WithInsecure(), grpc.WithDefaultCallOptions(codec))
	if err != nil {
		t.Fatalf("Unable to connect to gRPC server: %v", err)
	}
	defer closeHelper(conn)

	fds, err := protoyaml.GetFileDescSet("../../template/metric/template_handler_service_proto.descriptor_set")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	compiler := compiled.NewBuilder(StatdardVocabulary())
	res := protoyaml.NewResolver(fds)

	var ra *RemoteAdapter
	if ra, err = RemoteAdapterSvc("", res); err != nil {
		t.Fatalf("unable to get service: %v", err)
	}


	t.Logf("ra = %s", ra)

	builder := dynamic.NewEncoderBuilder(res, compiler, false)
	var enc dynamic.Encoder
	var ba []byte

	enc, err = builder.Build(".metric.InstanceMsg", map[string]interface{}{
		"name":  "'abc'",
		"value": 2000,
	})
	t.Logf("enc: %v, err:%v ba:%v", enc, err, ba)

	vv := attributeV1beta1.Value_StringValue{}
	vv.StringValue = "abc"
	ba, err = enc.Encode(nil, ba)

	t.Logf("%v: %v %v", req, res, compiler)
	/*
		return validateHandleCalls(
			metric.NewHandleMetricServiceClient(conn),
			listentry.NewHandleListEntryServiceClient(conn),
			quota.NewHandleQuotaServiceClient(conn),
			req)
	*/
	return nil
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



// StatdardVocabulary returns Istio standard vocabulary
func StatdardVocabulary() ast.AttributeDescriptorFinder {
	attrs := map[string]*attributeV1beta1.AttributeManifest_AttributeInfo{
		"api.operation":                   {ValueType: attributeV1beta1.STRING},
		"api.protocol":                    {ValueType: attributeV1beta1.STRING},
		"api.service":                     {ValueType: attributeV1beta1.STRING},
		"api.version":                     {ValueType: attributeV1beta1.STRING},
		"connection.duration":             {ValueType: attributeV1beta1.DURATION},
		"connection.id":                   {ValueType: attributeV1beta1.STRING},
		"connection.received.bytes":       {ValueType: attributeV1beta1.INT64},
		"connection.received.bytes_total": {ValueType: attributeV1beta1.INT64},
		"connection.sent.bytes":           {ValueType: attributeV1beta1.INT64},
		"connection.sent.bytes_total":     {ValueType: attributeV1beta1.INT64},
		"context.protocol":                {ValueType: attributeV1beta1.STRING},
		"context.time":                    {ValueType: attributeV1beta1.TIMESTAMP},
		"context.timestamp":               {ValueType: attributeV1beta1.TIMESTAMP},
		"destination.ip":                  {ValueType: attributeV1beta1.IP_ADDRESS},
		"destination.labels":              {ValueType: attributeV1beta1.STRING_MAP},
		"destination.name":                {ValueType: attributeV1beta1.STRING},
		"destination.namespace":           {ValueType: attributeV1beta1.STRING},
		"destination.service":             {ValueType: attributeV1beta1.STRING},
		"destination.serviceAccount":      {ValueType: attributeV1beta1.STRING},
		"destination.uid":                 {ValueType: attributeV1beta1.STRING},
		"origin.ip":                       {ValueType: attributeV1beta1.IP_ADDRESS},
		"origin.uid":                      {ValueType: attributeV1beta1.STRING},
		"origin.user":                     {ValueType: attributeV1beta1.STRING},
		"request.api_key":                 {ValueType: attributeV1beta1.STRING},
		"request.auth.audiences":          {ValueType: attributeV1beta1.STRING},
		"request.auth.presenter":          {ValueType: attributeV1beta1.STRING},
		"request.auth.principal":          {ValueType: attributeV1beta1.STRING},
		"request.auth.claims":             {ValueType: attributeV1beta1.STRING_MAP},
		"request.headers":                 {ValueType: attributeV1beta1.STRING_MAP},
		"request.host":                    {ValueType: attributeV1beta1.STRING},
		"request.id":                      {ValueType: attributeV1beta1.STRING},
		"request.method":                  {ValueType: attributeV1beta1.STRING},
		"request.path":                    {ValueType: attributeV1beta1.STRING},
		"request.reason":                  {ValueType: attributeV1beta1.STRING},
		"request.referer":                 {ValueType: attributeV1beta1.STRING},
		"request.scheme":                  {ValueType: attributeV1beta1.STRING},
		"request.size":                    {ValueType: attributeV1beta1.INT64},
		"request.time":                    {ValueType: attributeV1beta1.TIMESTAMP},
		"request.useragent":               {ValueType: attributeV1beta1.STRING},
		"response.code":                   {ValueType: attributeV1beta1.INT64},
		"response.duration":               {ValueType: attributeV1beta1.DURATION},
		"response.headers":                {ValueType: attributeV1beta1.STRING_MAP},
		"response.size":                   {ValueType: attributeV1beta1.INT64},
		"response.time":                   {ValueType: attributeV1beta1.TIMESTAMP},
		"source.ip":                       {ValueType: attributeV1beta1.IP_ADDRESS},
		"source.labels":                   {ValueType: attributeV1beta1.STRING_MAP},
		"source.name":                     {ValueType: attributeV1beta1.STRING},
		"source.namespace":                {ValueType: attributeV1beta1.STRING},
		"source.service":                  {ValueType: attributeV1beta1.STRING},
		"source.serviceAccount":           {ValueType: attributeV1beta1.STRING},
		"source.uid":                      {ValueType: attributeV1beta1.STRING},
		"source.user":                     {ValueType: attributeV1beta1.STRING},
		"test.bool":                       {ValueType: attributeV1beta1.BOOL},
		"test.double":                     {ValueType: attributeV1beta1.DOUBLE},
		"test.i32":                        {ValueType: attributeV1beta1.INT64},
		"test.i64":                        {ValueType: attributeV1beta1.INT64},
		"test.float":                      {ValueType: attributeV1beta1.DOUBLE},
	}

	return ast.NewFinder(attrs)
}
