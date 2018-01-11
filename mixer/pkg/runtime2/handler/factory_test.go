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

package handler

import (
	"fmt"
	"testing"

	"github.com/gogo/protobuf/proto"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/runtime2/config"
	"istio.io/istio/mixer/pkg/runtime2/testing/data"
	"istio.io/istio/mixer/pkg/runtime2/testing/util"
	"istio.io/istio/mixer/pkg/template"
)

func TestBasic(t *testing.T) {
	templates := data.BuildTemplates(nil)
	attributes := data.BuildAdapters(nil)

	s := util.GetSnapshot(templates, attributes, data.ServiceConfig, globalCfg)

	i1 := s.Instances[data.FqnI1]
	h1 := s.Handlers[data.FqnH1]

	f := newFactory(s)
	h, err := f.build(h1, []*config.Instance{i1}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !h.IsValid() {
		t.Fail()
	}
}

func TestCacheUse(t *testing.T) {
	templates := data.BuildTemplates(nil)
	attributes := data.BuildAdapters(nil)

	s := util.GetSnapshot(templates, attributes, data.ServiceConfig, globalCfg)

	i1 := s.Instances[data.FqnI1]
	h1 := s.Handlers[data.FqnH1]

	f := newFactory(s)
	_, err := f.build(h1, []*config.Instance{i1}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Do it again to use the cache.
	h, err := f.build(h1, []*config.Instance{i1}, nil)

	if err != nil {
		t.Fatal(err)
	}
	if !h.IsValid() {
		t.Fail()
	}
}

func TestAdapterSupportsAdditionalTemplates(t *testing.T) {
	templates := data.BuildTemplates(nil)
	attributes := data.BuildAdapters(&adapter.Info{SupportedTemplates: []string{"t1", "unknown-template"}})

	s := util.GetSnapshot(templates, attributes, data.ServiceConfig, globalCfg)

	i1 := s.Instances[data.FqnI1]
	h1 := s.Handlers[data.FqnH1]

	f := newFactory(s)
	h, err := f.build(h1, []*config.Instance{i1}, nil)
	if err != nil {
		t.Fatal()
	}

	if !h.IsValid() {
		t.Fatal()
	}
}

func TestInferError(t *testing.T) {
	templates := data.BuildTemplates(&template.Info{InferType: func(proto.Message, template.TypeEvalFn) (proto.Message, error) {
		return nil, fmt.Errorf("data.Fake infer error")
	}})

	attributes := data.BuildAdapters(nil)

	s := util.GetSnapshot(templates, attributes, data.ServiceConfig, globalCfg)

	i1 := s.Instances[data.FqnI1]
	h1 := s.Handlers[data.FqnH1]

	f := newFactory(s)
	_, err := f.build(h1, []*config.Instance{i1}, nil)
	if err == nil {
		t.Fatal()
	}
}

func TestNilBuilder(t *testing.T) {
	templates := data.BuildTemplates(nil)

	attributes := data.BuildAdapters(&adapter.Info{
		NewBuilder: func() adapter.HandlerBuilder {
			return nil
		},
	})

	s := util.GetSnapshot(templates, attributes, data.ServiceConfig, globalCfg)

	i1 := s.Instances[data.FqnI1]
	h1 := s.Handlers[data.FqnH1]

	f := newFactory(s)
	_, err := f.build(h1, []*config.Instance{i1}, nil)
	if err == nil {
		t.Fatal()
	}
}

func TestBuilderDoesNotSupportTemplate(t *testing.T) {
	templates := data.BuildTemplates(&template.Info{
		BuilderSupportsTemplate: func(hndlrBuilder adapter.HandlerBuilder) bool {
			return false
		},
	})

	attributes := data.BuildAdapters(nil)

	s := util.GetSnapshot(templates, attributes, data.ServiceConfig, globalCfg)
	i1 := s.Instances[data.FqnI1]
	h1 := s.Handlers[data.FqnH1]

	f := newFactory(s)
	_, err := f.build(h1, []*config.Instance{i1}, nil)
	if err == nil {
		t.Fatal()
	}
}

func TestHandlerDoesNotSupportTemplate(t *testing.T) {
	templates := data.BuildTemplates(&template.Info{
		HandlerSupportsTemplate: func(hndlr adapter.Handler) bool {
			return false
		},
	})

	attributes := data.BuildAdapters(nil)

	s := util.GetSnapshot(templates, attributes, data.ServiceConfig, globalCfg)
	i1 := s.Instances[data.FqnI1]
	h1 := s.Handlers[data.FqnH1]

	f := newFactory(s)
	_, err := f.build(h1, []*config.Instance{i1}, nil)
	if err == nil {
		t.Fatal()
	}
}

func TestHandlerDoesNotSupportTemplate_ErrorDuringClose(t *testing.T) {
	// Trigger premature close due to handler not supporting the template.
	templates := data.BuildTemplates(&template.Info{
		HandlerSupportsTemplate: func(hndlr adapter.Handler) bool {
			return false
		},
	})

	attributes := data.BuildAdapters(&adapter.Info{
		NewBuilder: func() adapter.HandlerBuilder {
			return &data.FakeHandlerBuilder{
				HandlerErrorOnClose: true,
			}
		},
	})

	s := util.GetSnapshot(templates, attributes, data.ServiceConfig, globalCfg)
	i1 := s.Instances[data.FqnI1]
	h1 := s.Handlers[data.FqnH1]

	f := newFactory(s)
	_, err := f.build(h1, []*config.Instance{i1}, nil)
	if err == nil {
		t.Fatal()
	}
}

func TestHandlerDoesNotSupportTemplate_PanicDuringClose(t *testing.T) {
	// Trigger premature close due to handler not supporting the template.
	templates := data.BuildTemplates(&template.Info{
		HandlerSupportsTemplate: func(hndlr adapter.Handler) bool {
			return false
		},
	})

	attributes := data.BuildAdapters(&adapter.Info{
		NewBuilder: func() adapter.HandlerBuilder {
			return &data.FakeHandlerBuilder{
				HandlerPanicOnClose: true,
			}
		},
	})

	s := util.GetSnapshot(templates, attributes, data.ServiceConfig, globalCfg)
	i1 := s.Instances[data.FqnI1]
	h1 := s.Handlers[data.FqnH1]

	f := newFactory(s)
	_, err := f.build(h1, []*config.Instance{i1}, nil)
	if err == nil {
		t.Fatal()
	}
}

func TestPanicAtSetAdapterConfig(t *testing.T) {
	templates := data.BuildTemplates(nil)
	attributes := data.BuildAdapters(&adapter.Info{
		NewBuilder: func() adapter.HandlerBuilder {
			return &data.FakeHandlerBuilder{
				PanicAtSetAdapterConfig: true,
			}
		},
	})

	s := util.GetSnapshot(templates, attributes, data.ServiceConfig, globalCfg)

	i1 := s.Instances[data.FqnI1]
	h1 := s.Handlers[data.FqnH1]

	f := newFactory(s)
	_, err := f.build(h1, []*config.Instance{i1}, nil)
	if err == nil {
		t.Fatal()
	}
}

func TestFailedValidation(t *testing.T) {
	templates := data.BuildTemplates(nil)
	attributes := data.BuildAdapters(&adapter.Info{
		NewBuilder: func() adapter.HandlerBuilder {
			return &data.FakeHandlerBuilder{
				ErrorAtValidate: true,
			}
		},
	})

	s := util.GetSnapshot(templates, attributes, data.ServiceConfig, globalCfg)

	i1 := s.Instances[data.FqnI1]
	h1 := s.Handlers[data.FqnH1]

	f := newFactory(s)
	_, err := f.build(h1, []*config.Instance{i1}, nil)
	if err == nil {
		t.Fatal()
	}
}

func TestPanicAtValidation(t *testing.T) {
	templates := data.BuildTemplates(nil)
	attributes := data.BuildAdapters(&adapter.Info{
		NewBuilder: func() adapter.HandlerBuilder {
			return &data.FakeHandlerBuilder{
				PanicAtValidate: true,
			}
		},
	})

	s := util.GetSnapshot(templates, attributes, data.ServiceConfig, globalCfg)

	i1 := s.Instances[data.FqnI1]
	h1 := s.Handlers[data.FqnH1]

	f := newFactory(s)
	_, err := f.build(h1, []*config.Instance{i1}, nil)
	if err == nil {
		t.Fatal()
	}
}
