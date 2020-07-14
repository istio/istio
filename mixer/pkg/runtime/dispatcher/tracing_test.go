// Copyright Istio Authors. All Rights Reserved.
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

package dispatcher

import (
	"fmt"
	"testing"

	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/status"
)

type testBase struct {
	name                                   string
	templateName, handlerName, adapterName string
	err                                    error
	expectedFields                         map[string]interface{}
}

var checkResultTests = []struct {
	testBase
	checkResult adapter.CheckResult
}{
	{
		testBase: testBase{
			name:         "Status OK and nil error",
			templateName: "TN0",
			handlerName:  "HN0",
			adapterName:  "AN0",
			err:          nil,
			expectedFields: map[string]interface{}{
				meshFunction: "TN0",
				handlerName:  "HN0",
				adapterName:  "AN0",
				responseCode: "OK",
				responseMsg:  "",
				errorStr:     false,
			},
		},
	},
	{
		testBase: testBase{
			name:         "Status Not Found and nil error",
			templateName: "TN1",
			handlerName:  "HN1",
			adapterName:  "AN1",
			err:          nil,
			expectedFields: map[string]interface{}{
				meshFunction: "TN1",
				handlerName:  "HN1",
				adapterName:  "AN1",
				responseCode: "NOT_FOUND",
				responseMsg:  "42",
				errorStr:     false,
			},
		},
		checkResult: adapter.CheckResult{Status: status.WithMessage(rpc.NOT_FOUND, "42")},
	},
	{
		testBase: testBase{
			name:         "Status Not Found and error",
			templateName: "TN2",
			handlerName:  "HN2",
			adapterName:  "AN2",
			err:          fmt.Errorf("failed"),
			expectedFields: map[string]interface{}{
				meshFunction: "TN2",
				handlerName:  "HN2",
				adapterName:  "AN2",
				responseCode: "INTERNAL",
				responseMsg:  "failed",
				errorStr:     true,
			},
		},
		checkResult: adapter.CheckResult{Status: status.WithMessage(rpc.NOT_FOUND, "42")},
	},
}

func TestLogCheckResultToDispatchSpan(t *testing.T) {
	for _, test := range checkResultTests {
		t.Run(test.name, func(t *testing.T) {
			span := &testSpan{t: t}
			logCheckResultToDispatchSpan(span, test.templateName, test.handlerName, test.adapterName, test.checkResult, test.err)
			for k, v := range test.expectedFields {
				expect(t, span.fields, k, v)
			}
		})
	}
}

var quotaResultTests = []struct {
	testBase
	quotaResult adapter.QuotaResult
}{
	{
		testBase: testBase{
			name:         "Status OK and nil error",
			templateName: "TN0",
			handlerName:  "HN0",
			adapterName:  "AN0",
			err:          nil,
			expectedFields: map[string]interface{}{
				meshFunction: "TN0",
				handlerName:  "HN0",
				adapterName:  "AN0",
				responseCode: "OK",
				responseMsg:  "",
				errorStr:     false,
			},
		},
	},
	{
		testBase: testBase{
			name:         "Status Cancelled and nil error",
			templateName: "TN1",
			handlerName:  "HN1",
			adapterName:  "AN1",
			err:          nil,
			expectedFields: map[string]interface{}{
				meshFunction: "TN1",
				handlerName:  "HN1",
				adapterName:  "AN1",
				responseCode: "CANCELLED",
				responseMsg:  "hquota1.aquota.istio-system:cancelled details",
				errorStr:     false,
			},
		},
		quotaResult: adapter.QuotaResult{
			Status: rpc.Status{
				Code:    int32(rpc.CANCELLED),
				Message: "hquota1.aquota.istio-system:cancelled details",
			},
		},
	},
	{
		testBase: testBase{
			name:         "Status Not Found and error",
			templateName: "TN2",
			handlerName:  "HN2",
			adapterName:  "AN2",
			err:          fmt.Errorf("failed"),
			expectedFields: map[string]interface{}{
				meshFunction: "TN2",
				handlerName:  "HN2",
				adapterName:  "AN2",
				responseCode: "INTERNAL",
				responseMsg:  "failed",
				errorStr:     true,
			},
		},
		quotaResult: adapter.QuotaResult{
			Status: rpc.Status{
				Code:    int32(rpc.CANCELLED),
				Message: "hquota1.aquota.istio-system:cancelled details",
			},
		},
	},
}

func TestLogQuotaResultToDispatchSpan(t *testing.T) {
	for _, test := range quotaResultTests {
		t.Run(test.name, func(t *testing.T) {
			span := &testSpan{t: t}
			logQuotaResultToDispatchSpan(span, test.templateName, test.handlerName, test.adapterName, test.quotaResult, test.err)
			for k, v := range test.expectedFields {
				expect(t, span.fields, k, v)
			}
		})
	}
}

var errorTests = []testBase{
	{
		name:         "No error",
		templateName: "TN0",
		handlerName:  "HN0",
		adapterName:  "AN0",
		err:          nil,
		expectedFields: map[string]interface{}{
			meshFunction: "TN0",
			handlerName:  "HN0",
			adapterName:  "AN0",
			responseCode: "OK",
			responseMsg:  "",
			errorStr:     false,
		},
	},
	{
		name:         "With error",
		templateName: "TN2",
		handlerName:  "HN2",
		adapterName:  "AN2",
		err:          fmt.Errorf("failed"),
		expectedFields: map[string]interface{}{
			meshFunction: "TN2",
			handlerName:  "HN2",
			adapterName:  "AN2",
			responseCode: "INTERNAL",
			responseMsg:  "failed",
			errorStr:     true,
		},
	},
}

func TestLogErrorToDispatchSpan(t *testing.T) {
	for _, test := range errorTests {
		t.Run(test.name, func(t *testing.T) {
			span := &testSpan{t: t}
			logErrorToDispatchSpan(span, test.templateName, test.handlerName, test.adapterName, test.err)
			for k, v := range test.expectedFields {
				expect(t, span.fields, k, v)
			}
		})
	}
}

func expect(t *testing.T, fields []log.Field, key string, value interface{}) {
	for _, field := range fields {
		if field.Key() == key {
			if field.Value() != value {
				t.Errorf("Expected logged field %v to be %v, but found %v", key, value, field.Value())
			}
			return
		}
	}
	t.Errorf("Could not find logged field %v", key)
}

type testSpan struct {
	t *testing.T

	fields []log.Field
}

var _ opentracing.Span = &testSpan{}

func (span *testSpan) Finish() {
}

func (span *testSpan) FinishWithOptions(opts opentracing.FinishOptions) {
	span.t.Fatalf("operation not supported")
}

func (span *testSpan) Context() opentracing.SpanContext {
	span.t.Fatalf("operation not supported")
	return nil
}

func (span *testSpan) SetOperationName(operationName string) opentracing.Span {
	span.t.Fatalf("operation not supported")
	return nil
}

func (span *testSpan) SetTag(key string, value interface{}) opentracing.Span {
	span.t.Fatalf("operation not supported")
	return nil
}

func (span *testSpan) LogFields(fields ...log.Field) {
	span.fields = append(span.fields, fields...)
}

func (span *testSpan) LogKV(alternatingKeyValues ...interface{}) {
	span.t.Fatalf("operation not supported")
}

func (span *testSpan) SetBaggageItem(restrictedKey, value string) opentracing.Span {
	span.t.Fatalf("operation not supported")
	return nil
}

func (span *testSpan) BaggageItem(restrictedKey string) string {
	span.t.Fatalf("operation not supported")
	return ""
}

func (span *testSpan) Tracer() opentracing.Tracer {
	span.t.Fatalf("operation not supported")
	return nil
}

func (span *testSpan) LogEvent(event string) {
	span.t.Fatalf("operation not supported")
}

func (span *testSpan) LogEventWithPayload(event string, payload interface{}) {
	span.t.Fatalf("operation not supported")
}

func (span *testSpan) Log(data opentracing.LogData) {
	span.t.Fatalf("operation not supported")
}
