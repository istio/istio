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

// NOTE: TODO : Auto-generate this file for given templates

// Codegen blocks

// apa template
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -d false -t mixer/test/spyAdapter/template/apa/tmpl.proto

// check template
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -d false -t mixer/test/spyAdapter/template/check/tmpl.proto

// checkoutput template
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -d false -t mixer/test/spyAdapter/template/checkoutput/tmpl.proto

// report template
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -d false -t mixer/test/spyAdapter/template/report/reporttmpl.proto

// quota template
//go:generate $REPO_ROOT/bin/mixer_codegen.sh -t mixer/test/spyAdapter/template/quota/quotatmpl.proto

// Package spyadapter is intended for Mixer testing *ONLY*.
package spyadapter

import (
	"context"
	"time"

	"github.com/gogo/protobuf/types"

	"istio.io/istio/mixer/pkg/adapter"
	apaTmpl "istio.io/istio/mixer/test/spyAdapter/template/apa"
	checkTmpl "istio.io/istio/mixer/test/spyAdapter/template/check"
	checkOutputTmpl "istio.io/istio/mixer/test/spyAdapter/template/checkoutput"
	quotaTmpl "istio.io/istio/mixer/test/spyAdapter/template/quota"
	reportTmpl "istio.io/istio/mixer/test/spyAdapter/template/report"
)

type (

	// Adapter is a fake Adapter. It is used for controlling the Adapter's behavior as well as
	// inspect the input values that adapter receives from Mixer
	Adapter struct {
		Behavior    AdapterBehavior
		BuilderData *builderData
		HandlerData *handlerData
	}

	// AdapterBehavior defines the behavior of the Adapter
	// nolint: maligned
	AdapterBehavior struct {
		Name    string
		Builder BuilderBehavior
		Handler HandlerBehavior
	}

	// HandlerBehavior defines the behavior of the Handler
	// nolint: maligned
	HandlerBehavior struct {
		HandleSampleReportErr   error
		HandleSampleReportPanic bool
		HandleSampleReportSleep time.Duration

		HandleSampleCheckResult adapter.CheckResult
		HandleSampleCheckErr    error
		HandleSampleCheckPanic  bool
		HandleSampleCheckSleep  time.Duration

		HandleCheckProducerResult adapter.CheckResult
		HandleCheckProducerOutput *checkOutputTmpl.Output
		HandleCheckProducerErr    error
		HandleCheckProducerPanic  bool
		HandleCheckProducerSleep  time.Duration

		HandleSampleQuotaResult adapter.QuotaResult
		HandleSampleQuotaErr    error
		HandleSampleQuotaPanic  bool
		HandleSampleQuotaSleep  time.Duration

		GenerateSampleApaErr    error
		GenerateSampleApaOutput *apaTmpl.Output
		GenerateSampleApaPanic  bool
		GenerateSampleApaSleep  time.Duration

		CloseErr   error
		ClosePanic bool
	}

	// BuilderBehavior defines the behavior of the Builder
	// nolint: maligned
	BuilderBehavior struct {
		SetSampleReportTypesPanic  bool
		SetSampleCheckTypesPanic   bool
		SetCheckProducerTypesPanic bool
		SetSampleQuotaTypesPanic   bool

		SetAdapterConfigPanic bool

		ValidateErr   *adapter.ConfigErrors
		ValidatePanic bool

		BuildErr   error
		BuildPanic bool
	}

	// nolint: maligned
	builder struct {
		builderBehavior BuilderBehavior
		handlerBehavior HandlerBehavior
		builderData     *builderData
		handlerData     *handlerData
	}

	handler struct {
		behavior HandlerBehavior
		data     *handlerData
	}

	handlerData struct {
		CapturedCalls []CapturedCall

		CloseCount int
	}

	// CapturedCall describes a call into the adapter.
	CapturedCall struct {
		Name      string
		Instances []interface{}
	}

	builderData struct {
		// no of time called
		SetSampleReportTypesCount int

		// no of time called
		SetSampleQuotaTypesCount int

		// input to the method
		SetTypes map[string]interface{}

		// no of time called
		SetSampleCheckTypesCount int

		SetCheckProducerTypesCount int

		SetAdapterConfigAdptCfg adapter.Config
		SetAdapterConfigCount   int

		ValidateCount int

		BuildCount int
		BuildCtx   context.Context
		BuildEnv   adapter.Env
	}
)

var _ reportTmpl.HandlerBuilder = builder{}
var _ apaTmpl.HandlerBuilder = builder{}
var _ checkTmpl.HandlerBuilder = builder{}
var _ checkOutputTmpl.HandlerBuilder = builder{}

var _ reportTmpl.Handler = handler{}
var _ apaTmpl.Handler = handler{}
var _ checkTmpl.Handler = handler{}
var _ checkOutputTmpl.Handler = handler{}

func (b builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	b.builderData.BuildCount++
	if b.builderBehavior.BuildPanic {
		panic("Build")
	}

	b.builderData.BuildCtx = ctx
	b.builderData.BuildEnv = env

	return handler{behavior: b.handlerBehavior, data: b.handlerData}, b.builderBehavior.BuildErr
}

func (b builder) SetSampleCheckTypes(typeParams map[string]*checkTmpl.Type) {
	b.builderData.SetSampleCheckTypesCount++

	b.builderData.SetTypes = make(map[string]interface{}, len(typeParams))
	for k, v := range typeParams {
		b.builderData.SetTypes[k] = v
	}

	if b.builderBehavior.SetSampleCheckTypesPanic {
		panic("SetSampleCheckTypes")
	}
}

func (b builder) SetCheckProducerTypes(typeParams map[string]*checkOutputTmpl.Type) {
	b.builderData.SetCheckProducerTypesCount++

	b.builderData.SetTypes = make(map[string]interface{}, len(typeParams))
	for k, v := range typeParams {
		b.builderData.SetTypes[k] = v
	}

	if b.builderBehavior.SetCheckProducerTypesPanic {
		panic("SetCheckProducerTypes")
	}
}

func (b builder) SetSampleQuotaTypes(typeParams map[string]*quotaTmpl.Type) {
	b.builderData.SetSampleQuotaTypesCount++

	b.builderData.SetTypes = make(map[string]interface{}, len(typeParams))
	for k, v := range typeParams {
		b.builderData.SetTypes[k] = v
	}

	if b.builderBehavior.SetSampleQuotaTypesPanic {
		panic("SetSampleQuotaTypes")
	}
}

func (b builder) SetSampleReportTypes(typeParams map[string]*reportTmpl.Type) {
	b.builderData.SetSampleReportTypesCount++

	b.builderData.SetTypes = make(map[string]interface{}, len(typeParams))
	for k, v := range typeParams {
		b.builderData.SetTypes[k] = v
	}

	if b.builderBehavior.SetSampleReportTypesPanic {
		panic("SetSampleReportTypes")
	}
}

func (b builder) SetAdapterConfig(cfg adapter.Config) {
	b.builderData.SetAdapterConfigCount++
	b.builderData.SetAdapterConfigAdptCfg = cfg

	if b.builderBehavior.SetAdapterConfigPanic {
		panic("SetAdapterConfig")
	}
}

func (b builder) Validate() *adapter.ConfigErrors {
	b.builderData.ValidateCount++
	if b.builderBehavior.ValidatePanic {
		panic("Validate")
	}

	return b.builderBehavior.ValidateErr
}

func (h handler) HandleSampleCheck(ctx context.Context, instance *checkTmpl.Instance) (adapter.CheckResult, error) {
	c := CapturedCall{
		Name:      "HandleSampleCheck",
		Instances: []interface{}{instance},
	}
	if h.data.CapturedCalls == nil {
		h.data.CapturedCalls = []CapturedCall{}
	}
	h.data.CapturedCalls = append(h.data.CapturedCalls, c)

	if h.behavior.HandleSampleCheckPanic {
		panic("HandleSampleCheck")
	}

	time.Sleep(h.behavior.HandleSampleCheckSleep)
	return h.behavior.HandleSampleCheckResult, h.behavior.HandleSampleCheckErr
}

func (h handler) HandleCheckProducer(ctx context.Context, instance *checkOutputTmpl.Instance) (adapter.CheckResult, *checkOutputTmpl.Output, error) {
	c := CapturedCall{
		Name:      "HandleCheckProducer",
		Instances: []interface{}{instance},
	}
	h.data.CapturedCalls = append(h.data.CapturedCalls, c)

	if h.behavior.HandleCheckProducerPanic {
		panic("HandleCheckProducer")
	}

	time.Sleep(h.behavior.HandleCheckProducerSleep)
	return h.behavior.HandleCheckProducerResult, h.behavior.HandleCheckProducerOutput, h.behavior.HandleCheckProducerErr
}

func (h handler) HandleSampleQuota(ctx context.Context, instance *quotaTmpl.Instance, args adapter.QuotaArgs) (adapter.QuotaResult, error) {
	c := CapturedCall{
		Name:      "HandleSampleQuota",
		Instances: []interface{}{instance},
	}
	if h.data.CapturedCalls == nil {
		h.data.CapturedCalls = []CapturedCall{}
	}
	h.data.CapturedCalls = append(h.data.CapturedCalls, c)

	if h.behavior.HandleSampleQuotaPanic {
		panic("HandleSampleQuota")
	}

	time.Sleep(h.behavior.HandleSampleQuotaSleep)
	return h.behavior.HandleSampleQuotaResult, h.behavior.HandleSampleQuotaErr
}

func (h handler) HandleSampleReport(ctx context.Context, instances []*reportTmpl.Instance) error {

	c := CapturedCall{
		Name:      "HandleSampleReport",
		Instances: make([]interface{}, len(instances)),
	}
	for i, ins := range instances {
		c.Instances[i] = ins
	}

	if h.data.CapturedCalls == nil {
		h.data.CapturedCalls = []CapturedCall{}
	}
	h.data.CapturedCalls = append(h.data.CapturedCalls, c)

	if h.behavior.HandleSampleReportPanic {
		panic("HandleSampleReport")
	}
	time.Sleep(h.behavior.HandleSampleReportSleep)
	return h.behavior.HandleSampleReportErr
}

func (h handler) GenerateSampleApaAttributes(ctx context.Context, instance *apaTmpl.Instance) (*apaTmpl.Output, error) {
	c := CapturedCall{
		Name:      "HandleSampleApaAttributes",
		Instances: []interface{}{instance},
	}
	if h.data.CapturedCalls == nil {
		h.data.CapturedCalls = []CapturedCall{}
	}
	h.data.CapturedCalls = append(h.data.CapturedCalls, c)

	if h.behavior.GenerateSampleApaPanic {
		panic("GenerateSampleApaAttributes")
	}

	time.Sleep(h.behavior.GenerateSampleApaSleep)
	return h.behavior.GenerateSampleApaOutput, h.behavior.GenerateSampleApaErr
}

func (h handler) Close() error {
	h.data.CloseCount++
	if h.behavior.ClosePanic {
		panic("Close")
	}

	return h.behavior.CloseErr
}

// NewSpyAdapter returns a new instance of Adapter with the given behavior
func NewSpyAdapter(b AdapterBehavior) *Adapter {
	if b.Handler.GenerateSampleApaOutput == nil {
		b.Handler.GenerateSampleApaOutput = apaTmpl.NewOutput()
	}

	return &Adapter{Behavior: b, BuilderData: &builderData{}, HandlerData: &handlerData{}}
}

// GetAdptInfoFn returns the infoFn for the Adapter.
func (s *Adapter) GetAdptInfoFn() adapter.InfoFn {
	return func() adapter.Info {
		return adapter.Info{
			Name:        s.Behavior.Name,
			Description: "",
			SupportedTemplates: []string{reportTmpl.TemplateName, apaTmpl.TemplateName,
				checkTmpl.TemplateName, quotaTmpl.TemplateName, checkOutputTmpl.TemplateName},
			NewBuilder: func() adapter.HandlerBuilder {
				return builder{
					builderBehavior: s.Behavior.Builder,
					builderData:     s.BuilderData,
					handlerBehavior: s.Behavior.Handler,
					handlerData:     s.HandlerData,
				}
			},
			DefaultConfig: &types.Empty{},
			Impl:          "ThisIsASpyAdapter",
		}
	}
}
