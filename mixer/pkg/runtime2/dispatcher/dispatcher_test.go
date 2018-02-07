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

package dispatcher

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	tpb "istio.io/api/mixer/v1/template"
	rpc "istio.io/gogo-genproto/googleapis/google/rpc"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/il/compiled"
	"istio.io/istio/mixer/pkg/pool"
	"istio.io/istio/mixer/pkg/runtime"
	"istio.io/istio/mixer/pkg/runtime2/handler"
	"istio.io/istio/mixer/pkg/runtime2/routing"
	"istio.io/istio/mixer/pkg/runtime2/testing/data"
	"istio.io/istio/mixer/pkg/runtime2/testing/util"
	"istio.io/istio/pkg/log"
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

	// the variety of the operation to apply.
	variety tpb.TemplateVariety

	// quota method arguments to pass
	qma *runtime.QuotaMethodArgs

	// Attributes to see the response bag with before call
	responseAttrs map[string]interface{}

	expectedQuotaResult *adapter.QuotaResult

	expectedCheckResult *adapter.CheckResult

	// expected error, if specified
	err string

	// expected adapter/template log.
	log string
}{
	{
		name: "BasicCheck",
		config: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.RuleCheck1,
		},
		variety:             tpb.TEMPLATE_VARIETY_CHECK,
		expectedCheckResult: &adapter.CheckResult{},
		log: `
[tcheck] InstanceBuilderFn() => name: 'tcheck', bag: '---
ident                         : dest.istio-system
'
[tcheck] InstanceBuilderFn() <= (SUCCESS)
[tcheck] DispatchCheck => instance: '&Empty{}'
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
[tcheck] DispatchCheck => instance: '&Empty{}'
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
		expectedCheckResult: &adapter.CheckResult{
			ValidUseCount: 10,
			ValidDuration: time.Millisecond,
		},
		log: `
[tcheck] InstanceBuilderFn() => name: 'tcheck', bag: '---
ident                         : dest.istio-system
'
[tcheck] InstanceBuilderFn() <= (SUCCESS)
[tcheck] DispatchCheck => instance: '&Empty{}'
[tcheck] DispatchCheck <= (SUCCESS)
[tcheck] InstanceBuilderFn() => name: 'tcheck', bag: '---
ident                         : dest.istio-system
'
[tcheck] InstanceBuilderFn() <= (SUCCESS)
[tcheck] DispatchCheck => instance: '&Empty{}'
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
		expectedCheckResult: &adapter.CheckResult{
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
[tcheck] DispatchCheck => instance: '&Empty{}'
[tcheck] DispatchCheck <= (SUCCESS)
[tcheck] InstanceBuilderFn() => name: 'tcheck', bag: '---
ident                         : dest.istio-system
'
[tcheck] InstanceBuilderFn() <= (SUCCESS)
[tcheck] DispatchCheck => instance: '&Empty{}'
[tcheck] DispatchCheck <= (SUCCESS)
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
[treport] DispatchReport => instances: '[&Empty{}]'
[treport] DispatchReport <= (SUCCESS)
`,
	},

	{
		name: "BasicReportError",
		templates: []data.FakeTemplateSettings{{
			Name: "treport",
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
[treport] DispatchReport => instances: '[&Empty{}]'
[treport] DispatchReport <= (ERROR)
`,
	},

	{
		name: "BasicQuota",
		config: []string{
			data.HandlerAQuota1,
			data.InstanceQuota1,
			data.RuleQuota1,
		},
		variety:             tpb.TEMPLATE_VARIETY_QUOTA,
		expectedQuotaResult: &adapter.QuotaResult{},
		log: `
[tquota] InstanceBuilderFn() => name: 'tquota', bag: '---
ident                         : dest.istio-system
'
[tquota] InstanceBuilderFn() <= (SUCCESS)
[tquota] DispatchQuota => instance: '&Empty{}'
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
[tquota] DispatchQuota => instance: '&Empty{}'
[tquota] DispatchQuota <= (ERROR)
`,
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
		expectedQuotaResult: &adapter.QuotaResult{
			Amount:        55,
			ValidDuration: time.Second,
		},
		log: `
[tquota] InstanceBuilderFn() => name: 'tquota', bag: '---
ident                         : dest.istio-system
'
[tquota] InstanceBuilderFn() <= (SUCCESS)
[tquota] DispatchQuota => instance: '&Empty{}'
[tquota] DispatchQuota <= (SUCCESS)
[tquota] InstanceBuilderFn() => name: 'tquota', bag: '---
ident                         : dest.istio-system
'
[tquota] InstanceBuilderFn() <= (SUCCESS)
[tquota] DispatchQuota => instance: '&Empty{}'
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
		variety: tpb.TEMPLATE_VARIETY_QUOTA,
		expectedQuotaResult: &adapter.QuotaResult{
			Status: rpc.Status{
				Code:    int32(rpc.CANCELLED),
				Message: "hquota1.aquota.istio-system:cancelled details, hquota1.aquota.istio-system:aborted details",
			},
			Amount:        55,
			ValidDuration: time.Second,
		},
		log: `
[tquota] InstanceBuilderFn() => name: 'tquota', bag: '---
ident                         : dest.istio-system
'
[tquota] InstanceBuilderFn() <= (SUCCESS)
[tquota] DispatchQuota => instance: '&Empty{}'
[tquota] DispatchQuota <= (SUCCESS)
[tquota] InstanceBuilderFn() => name: 'tquota', bag: '---
ident                         : dest.istio-system
'
[tquota] InstanceBuilderFn() <= (SUCCESS)
[tquota] DispatchQuota => instance: '&Empty{}'
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
[tapa] DispatchGenAttrs => instance: '&Empty{}'
[tapa] DispatchGenAttrs <= (SUCCESS)
`,
	},

	{
		name: "BasicPreprocessError",
		templates: []data.FakeTemplateSettings{{
			Name: "tapa",
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
[tapa] DispatchGenAttrs => instance: '&Empty{}'
[tapa] DispatchGenAttrs <= (ERROR)

`,
	},

	{
		name: "ErrorExtractingIdentityAttribute",
		config: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.RuleCheck1,
		},
		attr: map[string]interface{}{
			"ident": 23,
		},
		variety: tpb.TEMPLATE_VARIETY_CHECK,
		err:     "identity parameter is not a string: 'ident'",
	},

	{
		name: "InputSetDoesNotMatch",
		config: []string{
			data.HandlerACheck1,
			data.InstanceCheck1,
			data.RuleCheck1WithMatchClause,
		},
		attr: map[string]interface{}{
			"ident":       "dest.istio-system",
			"target.name": "barf", // "foo*" is expected
		},
		variety: tpb.TEMPLATE_VARIETY_CHECK,
		log:     ``, // log should be empty
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
[tcheck] DispatchCheck => instance: '&Empty{}'
[tcheck] DispatchCheck <= (PANIC)
`,
	},
}

func TestDispatcher(t *testing.T) {
	o := log.NewOptions()
	log.Configure(o)

	for _, tst := range tests {
		t.Run(tst.name, func(tt *testing.T) {

			dispatcher := New("ident", gp, true)

			l := &data.Logger{}

			templates := data.BuildTemplates(l, tst.templates...)
			adapters := data.BuildAdapters(l, tst.adapters...)
			config := data.JoinConfigs(tst.config...)

			s := util.GetSnapshot(templates, adapters, data.ServiceConfig, config)
			h := handler.NewTable(handler.Empty(), s, pool.NewGoroutinePool(1, false))

			expb := compiled.NewBuilder(s.Attributes)
			r := routing.BuildTable(h, s, expb, "istio-system", true)
			_ = dispatcher.ChangeRoute(r)

			// clear logger, as we are not interested in adapter/template logs during config step.
			l.Clear()

			attr := tst.attr
			if attr == nil {
				attr = map[string]interface{}{
					"ident": "dest.istio-system",
				}
			}
			bag := attribute.GetFakeMutableBagForTesting(attr)

			responseAttrs := map[string]interface{}{}
			if tst.responseAttrs != nil {
				responseAttrs = tst.responseAttrs
			}
			responseBag := attribute.GetFakeMutableBagForTesting(responseAttrs)

			var err error
			switch tst.variety {
			case tpb.TEMPLATE_VARIETY_CHECK:
				cres, e := dispatcher.Check(context.TODO(), bag)
				if e == nil {
					if !reflect.DeepEqual(cres, tst.expectedCheckResult) {
						tt.Fatalf("check result mismatch: '%v' != '%v'", cres, tst.expectedCheckResult)
					}
				} else {
					err = e
				}

			case tpb.TEMPLATE_VARIETY_REPORT:
				err = dispatcher.Report(context.TODO(), bag)

			case tpb.TEMPLATE_VARIETY_QUOTA:
				qma := tst.qma
				if qma == nil {
					qma = &runtime.QuotaMethodArgs{BestEffort: true}
				}
				qres, e := dispatcher.Quota(context.TODO(), bag, qma)
				if e == nil {
					if !reflect.DeepEqual(qres, tst.expectedQuotaResult) {
						tt.Fatalf("quota result mismatch: '%v' != '%v'", qres, tst.expectedQuotaResult)
					}
				} else {
					err = e
				}

			case tpb.TEMPLATE_VARIETY_ATTRIBUTE_GENERATOR:
				err = dispatcher.Preprocess(context.TODO(), bag, responseBag)

			default:
				tt.Fatalf("Unknown variety type: %v", tst.variety)
			}

			if tst.err != "" {
				if err == nil {
					tt.Fatalf("expected error was not thrown")
				} else if strings.TrimSpace(tst.err) != strings.TrimSpace(err.Error()) {
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
	d := New("ident", gp, true)
	old := d.ChangeRoute(routing.Empty())
	if old.GetRefs() != 0 {
		t.Fatalf("%d != 0", old.GetRefs())
	}
	old.IncRef()
	if old.GetRefs() != 1 {
		t.Fatalf("%d != 1", old.GetRefs())
	}
	old.DecRef()
	if old.GetRefs() != 0 {
		t.Fatalf("%d != -", old.GetRefs())
	}

}
