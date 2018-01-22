// Copyright 2017 Istio Authors
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
//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -t mixer/test/spyAdapter/template/apa/tmpl.proto

// report template
//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -t mixer/test/spyAdapter/template/report/reporttmpl.proto

// Package spyAdapter is intended for Mixer testing *ONLY*.
package spyAdapter

import (
	"context"

	"github.com/gogo/protobuf/types"

	"istio.io/istio/mixer/pkg/adapter"
	apaTmpl "istio.io/istio/mixer/test/spyAdapter/template/apa"
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

		GenerateSampleApaErr    error
		GenerateSampleApaOutput *apaTmpl.Output
		GenerateSampleApaPanic  bool

		CloseErr   error
		ClosePanic bool
	}

	// BuilderBehavior defines the behavior of the Builder
	// nolint: maligned
	BuilderBehavior struct {
		SetSampleReportTypesPanic bool

		SetAdapterConfigPanic bool

		ValidateErr   *adapter.ConfigErrors
		ValidatePanic bool

		BuildErr   error
		BuildPanic bool
	}

	// nolint: maligned
	builder struct {
		behavior        BuilderBehavior
		handlerBehavior HandlerBehavior
		data            *builderData
		handlerData     *handlerData
	}

	handler struct {
		behavior HandlerBehavior
		data     *handlerData
	}

	handlerData struct {
		HandleSampleReportInstances []*reportTmpl.Instance
		HandleSampleReportCount     int

		GenerateSampleApaInstance *apaTmpl.Instance
		GenerateSampleApaCount    int

		CloseCount int
	}

	builderData struct {
		// no of time called
		SetSampleReportTypesCount int
		// input to the method
		SetSampleReportTypesTypes map[string]*reportTmpl.Type

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

var _ reportTmpl.Handler = handler{}
var _ apaTmpl.Handler = handler{}

func (b builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	b.data.BuildCount++
	if b.behavior.BuildPanic {
		panic("Build")
	}

	b.data.BuildCtx = ctx
	b.data.BuildEnv = env

	return handler{behavior: b.handlerBehavior, data: b.handlerData}, b.behavior.BuildErr
}

func (b builder) SetSampleReportTypes(typeParams map[string]*reportTmpl.Type) {
	b.data.SetSampleReportTypesCount++
	b.data.SetSampleReportTypesTypes = typeParams

	if b.behavior.SetSampleReportTypesPanic {
		panic("SetSampleReportTypes")
	}
}

func (b builder) SetAdapterConfig(cfg adapter.Config) {
	b.data.SetAdapterConfigCount++
	b.data.SetAdapterConfigAdptCfg = cfg

	if b.behavior.SetAdapterConfigPanic {
		panic("SetAdapterConfig")
	}
}

func (b builder) Validate() *adapter.ConfigErrors {
	b.data.ValidateCount++
	if b.behavior.ValidatePanic {
		panic("Validate")
	}

	return b.behavior.ValidateErr
}

func (h handler) HandleSampleReport(ctx context.Context, instances []*reportTmpl.Instance) error {
	h.data.HandleSampleReportCount++
	if h.behavior.HandleSampleReportPanic {
		panic("HandleSampleReport")
	}

	h.data.HandleSampleReportInstances = instances
	return h.behavior.HandleSampleReportErr
}

func (h handler) GenerateSampleApaAttributes(ctx context.Context, instance *apaTmpl.Instance) (*apaTmpl.Output, error) {
	h.data.GenerateSampleApaCount++
	if h.behavior.GenerateSampleApaPanic {
		panic("GenerateSampleApaAttributes")
	}

	h.data.GenerateSampleApaInstance = instance
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
	return &Adapter{Behavior: b, BuilderData: &builderData{}, HandlerData: &handlerData{}}
}

// GetAdptInfoFn returns the infoFn for the Adapter.
func (s *Adapter) GetAdptInfoFn() adapter.InfoFn {
	return func() adapter.Info {
		return adapter.Info{
			Name:               s.Behavior.Name,
			Description:        "",
			SupportedTemplates: []string{reportTmpl.TemplateName, apaTmpl.TemplateName},
			NewBuilder: func() adapter.HandlerBuilder {
				return builder{
					behavior:        s.Behavior.Builder,
					data:            s.BuilderData,
					handlerBehavior: s.Behavior.Handler,
					handlerData:     s.HandlerData,
				}
			},
			DefaultConfig: &types.Empty{},
			Impl:          "ThisIsASpyAdapter",
		}
	}
}
