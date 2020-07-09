// Copyright Istio Authors
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
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	tpb "istio.io/api/mixer/adapter/model/v1beta1"
	v1 "istio.io/api/mixer/v1"
	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/runtime/config"
	"istio.io/istio/mixer/pkg/runtime/handler"
	"istio.io/istio/mixer/pkg/runtime/routing"
	"istio.io/istio/mixer/pkg/runtime/testing/data"
	"istio.io/istio/mixer/pkg/status"
	"istio.io/pkg/attribute"
	"istio.io/pkg/log"
	"istio.io/pkg/pool"
)

var gp = pool.NewGoroutinePool(10, true)

var tests = []struct {
	// name of the test
	name string

	// fake template settings to use. Default settings will be used if empty.
	templates []data.FakeTemplateSettings

	// fake adapter settings to use. Default settings will be used if empty.
	adapters []data.FakeAdapterSettings

	// configs to use
	config []string

	// attributes to use. If left empty, a default bag will be used.
	attr map[string]interface{}

	// quota method arguments to pass
	qma *QuotaMethodArgs

	// Attributes to expect in the response bag
	responseAttrs map[string]interface{}

	expectedQuotaResult adapter.QuotaResult

	expectedCheckResult adapter.CheckResult

	// expected error, if specified
	err string

	// expected adapter/template log.
	log string

	// the variety of the operation to apply.
	variety tpb.TemplateVariety

	// print out the full log for this test. Useful for debugging.
	fullLog bool
}{
	{
		name: "BasicCheck",
		config: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.RuleCheck1,
		},
		variety:             tpb.TEMPLATE_VARIETY_CHECK,
		expectedCheckResult: adapter.CheckResult{ValidDuration: 123 * time.Second, ValidUseCount: 123},
		log: `
[tcheck] InstanceBuilderFn() => name: 'tcheck', bag: '---
ident                         : dest.istio-system
'
[tcheck] InstanceBuilderFn() <= (SUCCESS)
[tcheck] DispatchCheck => context exists: 'true'
[tcheck] DispatchCheck => handler exists: 'true'
[tcheck] DispatchCheck => instance:       '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}'
[tcheck] DispatchCheck <= (SUCCESS)
`,
	},

	{
		name: "BasicCheckError",
		templates: []data.FakeTemplateSettings{{
			Name:                 "tcheck",
			ErrorOnDispatchCheck: true,
		}},
		config: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.RuleCheck1,
		},
		variety: tpb.TEMPLATE_VARIETY_CHECK,
		err: `
1 error occurred:

* error at dispatch check, as expected
`,
		log: `
[tcheck] InstanceBuilderFn() => name: 'tcheck', bag: '---
ident                         : dest.istio-system
'
[tcheck] InstanceBuilderFn() <= (SUCCESS)
[tcheck] DispatchCheck => context exists: 'true'
[tcheck] DispatchCheck => handler exists: 'true'
[tcheck] DispatchCheck => instance:       '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}'
[tcheck] DispatchCheck <= (ERROR)
`,
	},

	{
		name: "CheckResultCombination",
		templates: []data.FakeTemplateSettings{{
			Name: "tcheck",
			CheckResults: []adapter.CheckResult{
				{ValidUseCount: 10, ValidDuration: time.Minute},
				{ValidUseCount: 20, ValidDuration: time.Millisecond},
			},
		}},
		config: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.InstanceCheck2,
			data.RuleCheck1WithInstance1And2,
		},
		variety: tpb.TEMPLATE_VARIETY_CHECK,
		expectedCheckResult: adapter.CheckResult{
			ValidUseCount: 10,
			ValidDuration: time.Millisecond,
		},
		log: `
[tcheck] InstanceBuilderFn() => name: 'tcheck', bag: '---
ident                         : dest.istio-system
'
[tcheck] InstanceBuilderFn() <= (SUCCESS)
[tcheck] DispatchCheck => context exists: 'true'
[tcheck] DispatchCheck => handler exists: 'true'
[tcheck] DispatchCheck => instance:       '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}'
[tcheck] DispatchCheck <= (SUCCESS)
[tcheck] InstanceBuilderFn() => name: 'tcheck', bag: '---
ident                         : dest.istio-system
'
[tcheck] InstanceBuilderFn() <= (SUCCESS)
[tcheck] DispatchCheck => context exists: 'true'
[tcheck] DispatchCheck => handler exists: 'true'
[tcheck] DispatchCheck => instance:       '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}'
[tcheck] DispatchCheck <= (SUCCESS)
`,
	},

	{
		name: "CheckResultCombinationWithError",
		templates: []data.FakeTemplateSettings{{
			Name: "tcheck",
			CheckResults: []adapter.CheckResult{
				{
					Status: rpc.Status{
						Code:    int32(rpc.DATA_LOSS),
						Message: "data loss details",
					},
					ValidUseCount: 10,
					ValidDuration: time.Minute,
				},
				{
					Status: rpc.Status{
						Code:    int32(rpc.DEADLINE_EXCEEDED),
						Message: "deadline exceeded details",
					},

					ValidUseCount: 20,
					ValidDuration: time.Millisecond,
				},
			},
		}},
		config: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.InstanceCheck2,
			data.RuleCheck1WithInstance1And2,
		},
		variety: tpb.TEMPLATE_VARIETY_CHECK,
		expectedCheckResult: adapter.CheckResult{
			Status: rpc.Status{
				Code:    int32(rpc.DATA_LOSS),
				Message: "hcheck1.acheck.istio-system:data loss details, hcheck1.acheck.istio-system:deadline exceeded details",
			},
			ValidUseCount: 10,
			ValidDuration: time.Millisecond,
		},
		log: `
[tcheck] InstanceBuilderFn() => name: 'tcheck', bag: '---
ident                         : dest.istio-system
'
[tcheck] InstanceBuilderFn() <= (SUCCESS)
[tcheck] DispatchCheck => context exists: 'true'
[tcheck] DispatchCheck => handler exists: 'true'
[tcheck] DispatchCheck => instance:       '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}'
[tcheck] DispatchCheck <= (SUCCESS)
[tcheck] InstanceBuilderFn() => name: 'tcheck', bag: '---
ident                         : dest.istio-system
'
[tcheck] InstanceBuilderFn() <= (SUCCESS)
[tcheck] DispatchCheck => context exists: 'true'
[tcheck] DispatchCheck => handler exists: 'true'
[tcheck] DispatchCheck => instance:       '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}'
[tcheck] DispatchCheck <= (SUCCESS)
`,
	},

	{
		name: "CheckResultCustomError",
		templates: []data.FakeTemplateSettings{{
			Name: "tcheck",
			CheckResults: []adapter.CheckResult{
				{
					Status: rpc.Status{
						Code:    int32(rpc.DATA_LOSS),
						Message: "data loss details",
						Details: []*types.Any{status.PackErrorDetail(&v1beta1.DirectHttpResponse{
							Code:    403,
							Body:    "nope",
							Headers: map[string]string{"istio-test": "istio-value"},
						})},
					},
					ValidUseCount: 10,
					ValidDuration: time.Second,
				},
				{
					Status: rpc.Status{
						Code:    int32(rpc.DEADLINE_EXCEEDED),
						Message: "deadline",
					},
					ValidUseCount: 20,
					ValidDuration: time.Second,
				},
			},
		}},
		config: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.InstanceCheck2,
			data.RuleCheck1WithInstance1And2Operation,
		},
		variety: tpb.TEMPLATE_VARIETY_CHECK,
		expectedCheckResult: adapter.CheckResult{
			Status: rpc.Status{
				Code:    int32(rpc.DATA_LOSS),
				Message: "hcheck1.acheck.istio-system:data loss details, hcheck1.acheck.istio-system:deadline",
			},
			ValidUseCount: 10,
			ValidDuration: time.Second,
			// note no header operation in case of an error
			RouteDirective: &v1.RouteDirective{
				DirectResponseCode: 403,
				DirectResponseBody: "nope",
				ResponseHeaderOperations: []v1.HeaderOperation{{
					Name:  "istio-test",
					Value: "istio-value",
				}},
			},
		},
		log: `
[tcheck] InstanceBuilderFn() => name: 'tcheck', bag: '---
ident                         : dest.istio-system
'
[tcheck] InstanceBuilderFn() <= (SUCCESS)
[tcheck] DispatchCheck => context exists: 'true'
[tcheck] DispatchCheck => handler exists: 'true'
[tcheck] DispatchCheck => instance:       '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}'
[tcheck] DispatchCheck <= (SUCCESS)
[tcheck] InstanceBuilderFn() => name: 'tcheck', bag: '---
ident                         : dest.istio-system
'
[tcheck] InstanceBuilderFn() <= (SUCCESS)
[tcheck] DispatchCheck => context exists: 'true'
[tcheck] DispatchCheck => handler exists: 'true'
[tcheck] DispatchCheck => instance:       '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}'
[tcheck] DispatchCheck <= (SUCCESS)
`,
	},

	{
		name: "CheckResultCustomErrorNoCode",
		templates: []data.FakeTemplateSettings{{
			Name: "tcheck",
			CheckResults: []adapter.CheckResult{
				{
					Status: rpc.Status{
						Code:    int32(rpc.DATA_LOSS),
						Message: "data loss details",
						Details: []*types.Any{status.PackErrorDetail(&v1beta1.DirectHttpResponse{
							Headers: map[string]string{"istio-test": "istio-value"},
						})},
					},
					ValidUseCount: 10,
					ValidDuration: time.Second,
				},
				{
					Status: rpc.Status{
						Code:    int32(rpc.DEADLINE_EXCEEDED),
						Message: "deadline",
					},
					ValidUseCount: 20,
					ValidDuration: time.Second,
				},
			},
		}},
		config: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.InstanceCheck2,
			data.RuleCheck1WithInstance1And2Operation,
		},
		variety: tpb.TEMPLATE_VARIETY_CHECK,
		expectedCheckResult: adapter.CheckResult{
			Status: rpc.Status{
				Code:    int32(rpc.DATA_LOSS),
				Message: "hcheck1.acheck.istio-system:data loss details, hcheck1.acheck.istio-system:deadline",
			},
			ValidUseCount: 10,
			ValidDuration: time.Second,
			// note no header operation in case of an error
			RouteDirective: &v1.RouteDirective{
				DirectResponseCode: 500,
				ResponseHeaderOperations: []v1.HeaderOperation{{
					Name:  "istio-test",
					Value: "istio-value",
				}},
			},
		},
		log: `
[tcheck] InstanceBuilderFn() => name: 'tcheck', bag: '---
ident                         : dest.istio-system
'
[tcheck] InstanceBuilderFn() <= (SUCCESS)
[tcheck] DispatchCheck => context exists: 'true'
[tcheck] DispatchCheck => handler exists: 'true'
[tcheck] DispatchCheck => instance:       '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}'
[tcheck] DispatchCheck <= (SUCCESS)
[tcheck] InstanceBuilderFn() => name: 'tcheck', bag: '---
ident                         : dest.istio-system
'
[tcheck] InstanceBuilderFn() <= (SUCCESS)
[tcheck] DispatchCheck => context exists: 'true'
[tcheck] DispatchCheck => handler exists: 'true'
[tcheck] DispatchCheck => instance:       '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}'
[tcheck] DispatchCheck <= (SUCCESS)
`,
	},

	{
		name: "BasicCheckWithExpressions",
		config: []string{
			data.HandlerACheck1,
			data.InstanceCheck1WithSpec,
			data.RuleCheck1,
		},
		variety: tpb.TEMPLATE_VARIETY_CHECK,
		attr: map[string]interface{}{
			"attr.string": "bar",
			"ident":       "dest.istio-system",
		},
		expectedCheckResult: adapter.CheckResult{ValidDuration: 123 * time.Second, ValidUseCount: 123},
		// nolint
		log: `
[tcheck] InstanceBuilderFn() => name: 'tcheck', bag: '---
attr.string                   : bar
ident                         : dest.istio-system
'
[tcheck] InstanceBuilderFn() <= (SUCCESS)
[tcheck] DispatchCheck => context exists: 'true'
[tcheck] DispatchCheck => handler exists: 'true'
[tcheck] DispatchCheck => instance:       '&Struct{Fields:map[string]*Value{foo: &Value{Kind:&Value_StringValue{StringValue:bar,},XXX_unrecognized:[],},},XXX_unrecognized:[],}'
[tcheck] DispatchCheck <= (SUCCESS)
`,
	},

	{
		name: "BasicCheckOutput",
		config: []string{
			data.HandlerACheckOutput1,
			data.InstanceCheckOutput1,
			data.RuleCheckOutput1,
		},
		variety: tpb.TEMPLATE_VARIETY_CHECK_WITH_OUTPUT,
		expectedCheckResult: adapter.CheckResult{
			ValidDuration: 123 * time.Second,
			ValidUseCount: 123,
			RouteDirective: &v1.RouteDirective{
				RequestHeaderOperations: []v1.HeaderOperation{{
					Name:  "user",
					Value: "1337",
				}},
			},
		},
		log: `
[tcheckoutput] InstanceBuilderFn() => name: 'tcheckoutput', bag: '---
ident                         : dest.istio-system
'
[tcheckoutput] InstanceBuilderFn() <= (SUCCESS)
[tcheckoutput] DispatchCheck => context exists: 'true'
[tcheckoutput] DispatchCheck => handler exists: 'true'
[tcheckoutput] DispatchCheck => instance:       '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}'
[tcheckoutput] DispatchCheck <= (SUCCESS)
[tcheckoutput] DispatchCheck => output: {value: '1337'}
`,
	},

	{
		name: "BasicCheckMultipleOutput",
		config: []string{
			data.HandlerACheckOutput1,
			data.InstanceCheckOutput1,
			data.RuleCheckOutput2,
		},
		variety: tpb.TEMPLATE_VARIETY_CHECK_WITH_OUTPUT,
		attr: map[string]interface{}{
			"prefix.generated.string": "bar",
			"ident":                   "dest.istio-system",
		},
		expectedCheckResult: adapter.CheckResult{
			ValidDuration: 123 * time.Second,
			ValidUseCount: 123,
			RouteDirective: &v1.RouteDirective{
				RequestHeaderOperations: []v1.HeaderOperation{{
					Name:      "a-header",
					Value:     "1337",
					Operation: v1.REPLACE,
				}, {
					Name:      "user",
					Operation: v1.REMOVE,
				}},
				ResponseHeaderOperations: []v1.HeaderOperation{{
					Name:      "b-header",
					Value:     "1337",
					Operation: v1.APPEND,
				}, {
					Name:      "b-header",
					Value:     "bar",
					Operation: v1.APPEND,
				}},
			},
		},
		log: `
[tcheckoutput] InstanceBuilderFn() => name: 'tcheckoutput', bag: '---
ident                         : dest.istio-system
prefix.generated.string       : bar
'
[tcheckoutput] InstanceBuilderFn() <= (SUCCESS)
[tcheckoutput] DispatchCheck => context exists: 'true'
[tcheckoutput] DispatchCheck => handler exists: 'true'
[tcheckoutput] DispatchCheck => instance:       '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}'
[tcheckoutput] DispatchCheck <= (SUCCESS)
[tcheckoutput] DispatchCheck => output: {value: '1337'}
[tcheckoutput] InstanceBuilderFn() => name: 'tcheckoutput', bag: '---
ident                         : dest.istio-system
prefix.generated.string       : bar
'
[tcheckoutput] InstanceBuilderFn() <= (SUCCESS)
[tcheckoutput] DispatchCheck => context exists: 'true'
[tcheckoutput] DispatchCheck => handler exists: 'true'
[tcheckoutput] DispatchCheck => instance:       '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}'
[tcheckoutput] DispatchCheck <= (SUCCESS)
[tcheckoutput] DispatchCheck => output: {value: '1337'}
`,
	},

	{
		name: "BasicReport",
		config: []string{
			data.HandlerAReport1,
			data.InstanceReport1,
			data.RuleReport1,
		},
		variety: tpb.TEMPLATE_VARIETY_REPORT,
		log: `
[treport] InstanceBuilderFn() => name: 'treport', bag: '---
ident                         : dest.istio-system
'
[treport] InstanceBuilderFn() <= (SUCCESS)
[treport] DispatchReport => context exists: 'true'
[treport] DispatchReport => handler exists: 'true'
[treport] DispatchReport => instances: '[&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}]'
[treport] DispatchReport <= (SUCCESS)
`,
	},

	{
		name: "BasicReportError",
		templates: []data.FakeTemplateSettings{{
			// nolint: goimports
			Name:                  "treport",
			ErrorOnDispatchReport: true,
		}},
		config: []string{
			data.HandlerAReport1,
			data.InstanceReport1,
			data.RuleReport1,
		},
		variety: tpb.TEMPLATE_VARIETY_REPORT,
		err: `
1 error occurred:

* error at dispatch report, as expected
`,
		log: `
[treport] InstanceBuilderFn() => name: 'treport', bag: '---
ident                         : dest.istio-system
'
[treport] InstanceBuilderFn() <= (SUCCESS)
[treport] DispatchReport => context exists: 'true'
[treport] DispatchReport => handler exists: 'true'
[treport] DispatchReport => instances: '[&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}]'
[treport] DispatchReport <= (ERROR)
`,
	},

	{
		name: "BasicReportWithExpressions",
		config: []string{
			data.HandlerAReport1,
			data.InstanceReport1WithSpec,
			data.RuleReport1,
		},
		variety: tpb.TEMPLATE_VARIETY_REPORT,
		attr: map[string]interface{}{
			"attr.string": "bar",
			"ident":       "dest.istio-system",
		},
		// nolint
		log: `
[treport] InstanceBuilderFn() => name: 'treport', bag: '---
attr.string                   : bar
ident                         : dest.istio-system
'
[treport] InstanceBuilderFn() <= (SUCCESS)
[treport] DispatchReport => context exists: 'true'
[treport] DispatchReport => handler exists: 'true'
[treport] DispatchReport => instances: '[&Struct{Fields:map[string]*Value{foo: &Value{Kind:&Value_StringValue{StringValue:bar,},XXX_unrecognized:[],},},XXX_unrecognized:[],}]'
[treport] DispatchReport <= (SUCCESS)

`,
	},

	{
		name: "BasicQuota",
		config: []string{
			data.HandlerAQuota1,
			data.InstanceQuota1,
			data.RuleQuota1,
		},
		variety: tpb.TEMPLATE_VARIETY_QUOTA,
		qma: &QuotaMethodArgs{
			Quota:           "iquota1",
			BestEffort:      true,
			DeduplicationID: "42",
			Amount:          64,
		},
		log: `
[tquota] InstanceBuilderFn() => name: 'tquota', bag: '---
ident                         : dest.istio-system
'
[tquota] InstanceBuilderFn() <= (SUCCESS)
[tquota] DispatchQuota => context exists: 'true'
[tquota] DispatchQuota => handler exists: 'true'
[tquota] DispatchQuota => instance: '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}' qArgs:{dedup:'42', amount:'64', best:'true'}
[tquota] DispatchQuota <= (SUCCESS)
`,
	},

	{
		name: "BasicQuotaError",
		templates: []data.FakeTemplateSettings{{
			Name:                 "tquota",
			ErrorOnDispatchQuota: true,
		}},
		config: []string{
			data.HandlerAQuota1,
			data.InstanceQuota1,
			data.RuleQuota1,
		},
		variety: tpb.TEMPLATE_VARIETY_QUOTA,
		err: `
1 error occurred:

* error at dispatch quota, as expected`,
		log: `
[tquota] InstanceBuilderFn() => name: 'tquota', bag: '---
ident                         : dest.istio-system
'
[tquota] InstanceBuilderFn() <= (SUCCESS)
[tquota] DispatchQuota => context exists: 'true'
[tquota] DispatchQuota => handler exists: 'true'
[tquota] DispatchQuota => instance: '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}' qArgs:{dedup:'', amount:'0', best:'true'}
[tquota] DispatchQuota <= (ERROR)
`,
	},

	{
		name: "QuotaRequestUnknownQuota",
		config: []string{
			data.HandlerAQuota1,
			data.InstanceQuota1,
			data.RuleQuota1,
		},
		variety: tpb.TEMPLATE_VARIETY_QUOTA,
		qma: &QuotaMethodArgs{
			Quota:           "XXX",
			BestEffort:      true,
			DeduplicationID: "42",
			Amount:          10697,
		},
		expectedQuotaResult: adapter.QuotaResult{
			Amount:        10697,
			ValidDuration: time.Minute,
		},
	},

	{
		name: "QuotaRequestConditionalUnmatchedQuota",
		config: []string{
			data.HandlerAQuota1,
			data.InstanceQuota1WithSpec,
			data.RuleQuota1Conditional,
		},
		variety: tpb.TEMPLATE_VARIETY_QUOTA,
		qma: &QuotaMethodArgs{
			Quota:           "iquota1",
			BestEffort:      true,
			DeduplicationID: "42",
			Amount:          10697,
		},
		attr: map[string]interface{}{
			"attr.string": "XXX",
			"ident":       "dest.istio-system",
		},
		expectedQuotaResult: adapter.QuotaResult{
			Amount:        10697,
			ValidDuration: time.Minute,
		},
	},

	{
		name: "QuotaResultCombination",
		templates: []data.FakeTemplateSettings{{
			Name: "tquota",
			QuotaResults: []adapter.QuotaResult{
				{
					Amount:        55,
					ValidDuration: time.Second,
				},
				{
					Amount:        66,
					ValidDuration: time.Hour,
				},
			},
		}},
		config: []string{
			data.HandlerAQuota1,
			data.InstanceQuota1,
			data.InstanceQuota2,
			data.RuleQuota1,
			data.RuleQuota2,
		},
		variety: tpb.TEMPLATE_VARIETY_QUOTA,
		expectedQuotaResult: adapter.QuotaResult{
			Amount:        55,
			ValidDuration: time.Second,
		},
		log: `
[tquota] InstanceBuilderFn() => name: 'tquota', bag: '---
ident                         : dest.istio-system
'
[tquota] InstanceBuilderFn() <= (SUCCESS)
[tquota] DispatchQuota => context exists: 'true'
[tquota] DispatchQuota => handler exists: 'true'
[tquota] DispatchQuota => instance: '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}' qArgs:{dedup:'', amount:'0', best:'true'}
[tquota] DispatchQuota <= (SUCCESS)
`,
	},

	{
		name: "QuotaResultCombinationWithError",
		templates: []data.FakeTemplateSettings{{
			Name: "tquota",
			QuotaResults: []adapter.QuotaResult{
				{
					Amount:        55,
					ValidDuration: time.Second,
					Status: rpc.Status{
						Code:    int32(rpc.CANCELLED),
						Message: "cancelled details",
					},
				},
				{
					Amount:        66,
					ValidDuration: time.Hour,
					Status: rpc.Status{
						Code:    int32(rpc.ABORTED),
						Message: "aborted details",
					},
				},
			},
		}},
		config: []string{
			data.HandlerAQuota1,
			data.InstanceQuota1,
			data.InstanceQuota2,
			data.RuleQuota1,
			data.RuleQuota2,
		},
		qma: &QuotaMethodArgs{
			Quota:           "iquota1",
			DeduplicationID: "dedup-id",
			BestEffort:      true,
			Amount:          42,
		},
		variety: tpb.TEMPLATE_VARIETY_QUOTA,
		expectedQuotaResult: adapter.QuotaResult{
			Status: rpc.Status{
				Code:    int32(rpc.CANCELLED),
				Message: "hquota1.aquota.istio-system:cancelled details",
			},
			Amount:        55,
			ValidDuration: time.Second,
		},
		log: `
[tquota] InstanceBuilderFn() => name: 'tquota', bag: '---
ident                         : dest.istio-system
'
[tquota] InstanceBuilderFn() <= (SUCCESS)
[tquota] DispatchQuota => context exists: 'true'
[tquota] DispatchQuota => handler exists: 'true'
[tquota] DispatchQuota => instance: '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}' qArgs:{dedup:'dedup-id', amount:'42', best:'true'}
[tquota] DispatchQuota <= (SUCCESS)
`,
	},

	{
		name: "BasicQuotaWithExpressions",
		config: []string{
			data.HandlerAQuota1,
			data.InstanceQuota1WithSpec,
			data.RuleQuota1,
		},
		variety: tpb.TEMPLATE_VARIETY_QUOTA,
		attr: map[string]interface{}{
			"attr.string": "bar",
			"ident":       "dest.istio-system",
		},
		qma: &QuotaMethodArgs{
			Quota:           "iquota1",
			BestEffort:      true,
			DeduplicationID: "42",
			Amount:          64,
		},
		// nolint
		log: `
[tquota] InstanceBuilderFn() => name: 'tquota', bag: '---
attr.string                   : bar
ident                         : dest.istio-system
'
[tquota] InstanceBuilderFn() <= (SUCCESS)
[tquota] DispatchQuota => context exists: 'true'
[tquota] DispatchQuota => handler exists: 'true'
[tquota] DispatchQuota => instance: '&Struct{Fields:map[string]*Value{foo: &Value{Kind:&Value_StringValue{StringValue:bar,},XXX_unrecognized:[],},},XXX_unrecognized:[],}' ` +
			`qArgs:{dedup:'42', amount:'64', best:'true'}
[tquota] DispatchQuota <= (SUCCESS)
			`,
	},

	{
		name: "BasicPreprocess",
		config: []string{
			data.HandlerAPA1,
			data.InstanceAPA1,
			data.RuleApa1,
		},
		variety: tpb.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR,
		log: `
[tapa] InstanceBuilderFn() => name: 'tapa', bag: '---
ident                         : dest.istio-system
'
[tapa] InstanceBuilderFn() <= (SUCCESS)
[tapa] DispatchGenAttrs => instance: '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}'
[tapa] DispatchGenAttrs => attrs:    '---
ident                         : dest.istio-system
'
[tapa] DispatchGenAttrs => mapper(exists):   'true'
[tapa] DispatchGenAttrs <= (SUCCESS)
`,
	},

	{
		name: "BasicPreprocessError",
		templates: []data.FakeTemplateSettings{{
			// nolint: goimports
			Name:                    "tapa",
			ErrorOnDispatchGenAttrs: true,
		}},
		config: []string{
			data.HandlerAPA1,
			data.InstanceAPA1,
			data.RuleApa1,
		},
		variety: tpb.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR,
		err: `
1 error occurred:

* error at dispatch quota, as expected
`,
		log: `
[tapa] InstanceBuilderFn() => name: 'tapa', bag: '---
ident                         : dest.istio-system
'
[tapa] InstanceBuilderFn() <= (SUCCESS)
[tapa] DispatchGenAttrs => instance: '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}'
[tapa] DispatchGenAttrs => attrs:    '---
ident                         : dest.istio-system
'
[tapa] DispatchGenAttrs => mapper(exists):   'true'
[tapa] DispatchGenAttrs <= (ERROR)
`,
	},

	{
		name: "BasicPreprocessWithExpressions",
		config: []string{
			data.HandlerAPA1,
			data.InstanceAPA1WithSpec,
			data.RuleApa1,
		},
		variety: tpb.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR,
		attr: map[string]interface{}{
			"attr.string": "bar",
			"ident":       "dest.istio-system",
		},
		templates: []data.FakeTemplateSettings{{
			Name: "tapa",
			OutputAttrs: map[string]interface{}{
				"prefix.generated.string": "boz",
			},
		}},
		responseAttrs: map[string]interface{}{
			"prefix.generated.string": "boz",
		},
		// nolint
		log: `
[tapa] InstanceBuilderFn() => name: 'tapa', bag: '---
attr.string                   : bar
ident                         : dest.istio-system
'
[tapa] InstanceBuilderFn() <= (SUCCESS)
[tapa] DispatchGenAttrs => instance: '&Struct{Fields:map[string]*Value{foo: &Value{Kind:&Value_StringValue{StringValue:bar,},XXX_unrecognized:[],},},XXX_unrecognized:[],}'
[tapa] DispatchGenAttrs => attrs:    '---
attr.string                   : bar
ident                         : dest.istio-system
'
[tapa] DispatchGenAttrs => mapper(exists):   'true'
[tapa] DispatchGenAttrs <= (SUCCESS)
`,
	},

	{
		name: "InputSetDoesNotMatch",
		config: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.RuleCheck1WithMatchClause,
		},
		attr: map[string]interface{}{
			"ident":            "dest.istio-system",
			"destination.name": "barf", // "foo*" is expected
		},
		variety:             tpb.TEMPLATE_VARIETY_CHECK,
		expectedCheckResult: adapter.CheckResult{ValidDuration: defaultValidDuration, ValidUseCount: defaultValidUseCount},
		log:                 ``, // log should be empty
	},

	{
		name: "InstanceError",
		config: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.RuleCheck1,
		},
		templates: []data.FakeTemplateSettings{{
			Name: "tcheck", ErrorAtCreateInstance: true,
		}},
		variety: tpb.TEMPLATE_VARIETY_CHECK,
		err:     "error at create instance",
		log: `
[tcheck] InstanceBuilderFn() => name: 'tcheck', bag: '---
ident                         : dest.istio-system
'
[tcheck] InstanceBuilderFn() <= (ERROR)
`,
	},

	{
		name: "HandlerPanic",
		config: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.RuleCheck1,
		},
		templates: []data.FakeTemplateSettings{{
			Name: "tcheck", PanicOnDispatchCheck: true,
		}},
		variety: tpb.TEMPLATE_VARIETY_CHECK,
		err: `
1 error occurred:

* panic during handler dispatch: <nil>
`,
		log: `
[tcheck] InstanceBuilderFn() => name: 'tcheck', bag: '---
ident                         : dest.istio-system
'
[tcheck] InstanceBuilderFn() <= (SUCCESS)
[tcheck] DispatchCheck => context exists: 'true'
[tcheck] DispatchCheck => handler exists: 'true'
[tcheck] DispatchCheck => instance:       '&Struct{Fields:map[string]*Value{},XXX_unrecognized:[],}'
[tcheck] DispatchCheck <= (PANIC)
`,
	},

	{
		name: "CheckElidedRule",
		config: []string{
			data.HandlerACheckOutput1,
			data.InstanceCheckOutput1,
			data.RuleCheckNoActionsOrHeaderOps,
		},
		variety: tpb.TEMPLATE_VARIETY_CHECK_WITH_OUTPUT,
		expectedCheckResult: adapter.CheckResult{
			ValidDuration: defaultValidDuration,
			ValidUseCount: defaultValidUseCount,
		},
		log: ``,
	},

	{
		name: "CheckOnlyHeaderOperationRule",
		config: []string{
			data.HandlerACheckOutput1,
			data.InstanceCheckOutput1,
			data.RuleCheckHeaderOpWithNoActions,
		},
		variety: tpb.TEMPLATE_VARIETY_CHECK_WITH_OUTPUT,
		expectedCheckResult: adapter.CheckResult{
			ValidDuration: defaultValidDuration,
			ValidUseCount: defaultValidUseCount,
			RouteDirective: &v1.RouteDirective{
				ResponseHeaderOperations: []v1.HeaderOperation{{
					Name:      "b-header",
					Value:     "test",
					Operation: v1.APPEND,
				}},
			},
		},
		log: ``,
	},
}

func TestDispatcher(t *testing.T) {
	o := log.DefaultOptions()
	if err := log.Configure(o); err != nil {
		t.Fatal(err)
	}

	for _, tst := range tests {
		t.Run(tst.name, func(tt *testing.T) {

			dispatcher := New(gp, true)

			l := &data.Logger{}

			templates := data.BuildTemplates(l, tst.templates...)
			adapters := data.BuildAdapters(l, tst.adapters...)
			cfg := data.JoinConfigs(tst.config...)

			s, _ := config.GetSnapshotForTest(templates, adapters, data.ServiceConfig, cfg)
			h := handler.NewTable(handler.Empty(), s, pool.NewGoroutinePool(1, false), []string{metav1.NamespaceAll})

			r := routing.BuildTable(h, s, "istio-system", true)
			_ = dispatcher.ChangeRoute(r)

			// clear logger, as we are not interested in adapter/template logs during config step.
			if !tst.fullLog {
				l.Clear()
			}

			attr := tst.attr
			if attr == nil {
				attr = map[string]interface{}{
					"ident": "dest.istio-system",
				}
			}
			bag := attribute.GetMutableBagForTesting(attr)

			responseBag := attribute.GetMutableBagForTesting(make(map[string]interface{}))

			var err error
			switch tst.variety {
			case tpb.TEMPLATE_VARIETY_CHECK, tpb.TEMPLATE_VARIETY_CHECK_WITH_OUTPUT:
				cres, e := dispatcher.Check(context.TODO(), bag)

				if e == nil {
					if !reflect.DeepEqual(&cres, &tst.expectedCheckResult) {
						tt.Fatalf("check result mismatch: '%#v' != '%#v'", cres, tst.expectedCheckResult)
					}
				} else {
					err = e
				}

			case tpb.TEMPLATE_VARIETY_REPORT:
				reporter := dispatcher.GetReporter(context.TODO())
				err = reporter.Report(bag)
				if err != nil {
					tt.Fatalf("unexpected failure from Buffer: %v", err)
				}

				err = reporter.Flush()
				reporter.Done()

			case tpb.TEMPLATE_VARIETY_QUOTA:
				qma := tst.qma
				if qma == nil {
					qma = &QuotaMethodArgs{BestEffort: true, Quota: "iquota1"}
				}
				qres, e := dispatcher.Quota(context.TODO(), bag, *qma)

				if e == nil {
					if !reflect.DeepEqual(&qres, &tst.expectedQuotaResult) {
						tt.Fatalf("quota result mismatch: '%v' != '%v'", qres, tst.expectedQuotaResult)
					}
				} else {
					err = e
				}

			case tpb.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR:
				err = dispatcher.Preprocess(context.TODO(), bag, responseBag)

				expectedBag := attribute.GetMutableBagForTesting(tst.responseAttrs)
				if strings.TrimSpace(responseBag.String()) != strings.TrimSpace(expectedBag.String()) {
					tt.Fatalf("Output attributes mismatch: \n%s\n!=\n%s\n", responseBag.String(), expectedBag.String())
				}
			default:
				tt.Fatalf("Unknown variety type: %v", tst.variety)
			}

			if tst.err != "" {
				if err == nil {
					tt.Fatalf("expected error was not thrown")
				} else if !reflect.DeepEqual(strings.Fields(tst.err), strings.Fields(err.Error())) &&
					!strings.Contains(err.Error(), tst.err) {
					tt.Fatalf("error mismatch: '%v' != '%v'", err, tst.err)
				}
			} else {
				if err != nil {
					tt.Fatalf("unexpected error: '%v'", err)
				}
			}

			if strings.TrimSpace(tst.log) != strings.TrimSpace(l.String()) {
				tt.Fatalf("template/adapter log mismatch: '%v' != '%v'", l.String(), tst.log)
			}
		})
	}
}

func TestRefCount(t *testing.T) {
	d := New(gp, true)
	old := d.ChangeRoute(routing.Empty())
	if old.GetRefs() != 0 {
		t.Fatalf("%d != 0", old.GetRefs())
	}
	old.incRef()
	if old.GetRefs() != 1 {
		t.Fatalf("%d != 1", old.GetRefs())
	}
	old.decRef()
	if old.GetRefs() != 0 {
		t.Fatalf("%d != -", old.GetRefs())
	}

}
