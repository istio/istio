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

package handler

import (
	"fmt"
	"testing"

	"github.com/gogo/protobuf/proto"
	"istio.io/istio/mixer/pkg/runtime2/config"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/template"

	"istio.io/istio/mixer/pkg/runtime2/testing/data"
	"istio.io/istio/mixer/pkg/runtime2/testing/util"
)

func TestBasic(t *testing.T) {
	templates := data.BuildTemplates(nil)
	attributes := data.BuildAdapters(nil)

	s := util.GetSnapshot(templates, attributes, data.ServiceConfig, data.GlobalConfig)

	i1 := s.Instances["i1.t1.istio-system"]
	h1 := s.Handlers["h1.a1.istio-system"]

	f := newFactory(s, &data.FakeEnv{})
	h, err := f.build(h1, []*config.Instance{i1})
	if err != nil {
		t.Fatal(err)
	}

	if h == nil {
		t.Fail()
	}
}

func TestCacheUse(t *testing.T) {
	templates := data.BuildTemplates(nil)
	attributes := data.BuildAdapters(nil)

	s := util.GetSnapshot(templates, attributes, data.ServiceConfig, data.GlobalConfig)

	i1 := s.Instances["i1.t1.istio-system"]
	h1 := s.Handlers["h1.a1.istio-system"]

	f := newFactory(s, &data.FakeEnv{})
	_, err := f.build(h1, []*config.Instance{i1})
	if err != nil {
		t.Fatal(err)
	}

	// Do it again to use the cache.
	h, err := f.build(h1, []*config.Instance{i1})

	if err != nil {
		t.Fatal(err)
	}
	if h == nil {
		t.Fail()
	}
}

func TestInferError(t *testing.T) {
	templates := data.BuildTemplates(&template.Info{InferType: func(proto.Message, template.TypeEvalFn) (proto.Message, error) {
		return nil, fmt.Errorf("data.Fake infer error")
	}})

	attributes := data.BuildAdapters(nil)

	s := util.GetSnapshot(templates, attributes, data.ServiceConfig, data.GlobalConfig)

	i1 := s.Instances["i1.t1.istio-system"]
	h1 := s.Handlers["h1.a1.istio-system"]

	f := newFactory(s, &data.FakeEnv{})
	_, err := f.build(h1, []*config.Instance{i1})
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

	s := util.GetSnapshot(templates, attributes, data.ServiceConfig, data.GlobalConfig)

	i1 := s.Instances["i1.t1.istio-system"]
	h1 := s.Handlers["h1.a1.istio-system"]

	f := newFactory(s, &data.FakeEnv{})
	_, err := f.build(h1, []*config.Instance{i1})
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

	s := util.GetSnapshot(templates, attributes, data.ServiceConfig, data.GlobalConfig)

	i1 := s.Instances["i1.t1.istio-system"]
	h1 := s.Handlers["h1.a1.istio-system"]

	f := newFactory(s, &data.FakeEnv{})
	_, err := f.build(h1, []*config.Instance{i1})
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

	s := util.GetSnapshot(templates, attributes, data.ServiceConfig, data.GlobalConfig)

	i1 := s.Instances["i1.t1.istio-system"]
	h1 := s.Handlers["h1.a1.istio-system"]

	f := newFactory(s, &data.FakeEnv{})
	_, err := f.build(h1, []*config.Instance{i1})
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

	s := util.GetSnapshot(templates, attributes, data.ServiceConfig, data.GlobalConfig)

	i1 := s.Instances["i1.t1.istio-system"]
	h1 := s.Handlers["h1.a1.istio-system"]

	f := newFactory(s, &data.FakeEnv{})
	_, err := f.build(h1, []*config.Instance{i1})
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

	s := util.GetSnapshot(templates, attributes, data.ServiceConfig, data.GlobalConfig)

	i1 := s.Instances["i1.t1.istio-system"]
	h1 := s.Handlers["h1.a1.istio-system"]

	f := newFactory(s, &data.FakeEnv{})
	_, err := f.build(h1, []*config.Instance{i1})
	if err == nil {
		t.Fatal()
	}
}
