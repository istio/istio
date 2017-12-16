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
	"context"
	"fmt"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/template"
)

var serviceConfig = `
apiVersion: "config.istio.io/v1alpha2"
kind: attributemanifest
metadata:
  name: istio-proxy
  namespace: default
spec:
    attributes:
      source.name:
        value_type: STRING
      target.name:
        value_type: STRING
      response.count:
        value_type: INT64
      attr.bool:
        value_type: BOOL
      attr.string:
        value_type: STRING
      attr.double:
        value_type: DOUBLE
      attr.int64:
        value_type: INT64
---
`

var globalConfig = `
apiVersion: "config.istio.io/v1alpha2"
kind: a1
metadata:
  name: h1
  namespace: istio-system
---
apiVersion: "config.istio.io/v1alpha2"
kind: t1
metadata:
  name: i1
  namespace: istio-system
spec:
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r1
  namespace: istio-system
spec:
  selector: match(target.name, "*")
  actions:
  - handler: h1.a1
    instances:
    - i1.t1.istio-system
---
`

var globalConfigI2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: a1
metadata:
  name: h1
  namespace: istio-system
---
apiVersion: "config.istio.io/v1alpha2"
kind: t1
metadata:
  name: i1
  namespace: istio-system
spec:
---
apiVersion: "config.istio.io/v1alpha2"
kind: t1
metadata:
  name: i2
  namespace: istio-system
spec:
---
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r1
  namespace: istio-system
spec:
  selector: match(target.name, "*")
  actions:
  - handler: h1.a1
    instances:
    - i1.t1.istio-system
    - i2.t1.istio-system
---
`

func buildTemplates(override *template.Info) map[string]*template.Info {
	var t = map[string]*template.Info{
		"t1": {
			Name:   "t1",
			CtrCfg: &types.Empty{},

			InferType: func(p proto.Message, evalFn template.TypeEvalFn) (proto.Message, error) {
				_, _ = evalFn("source.name")
				return &types.Empty{}, nil
			},
			BuilderSupportsTemplate: func(hndlrBuilder adapter.HandlerBuilder) bool {
				return true
			},
			HandlerSupportsTemplate: func(hndlr adapter.Handler) bool {
				return true
			},
			SetType: func(types map[string]proto.Message, builder adapter.HandlerBuilder) {

			},
		},
	}

	if override != nil {
		if override.InferType != nil {
			t["t1"].InferType = override.InferType
		}
		if override.BuilderSupportsTemplate != nil {
			t["t1"].BuilderSupportsTemplate = override.BuilderSupportsTemplate
		}
		if override.HandlerSupportsTemplate != nil {
			t["t1"].HandlerSupportsTemplate = override.HandlerSupportsTemplate
		}
	}

	return t
}

func buildAdapters(override *adapter.Info) map[string]*adapter.Info {
	var a = map[string]*adapter.Info{
		"a1": {
			Name:               "a1",
			DefaultConfig:      &types.Empty{},
			SupportedTemplates: []string{"t1"},
			NewBuilder: func() adapter.HandlerBuilder {
				return &fakeHandlerBuilder{}
			},
		},
	}

	if override != nil {
		if override.NewBuilder != nil {
			a["a1"].NewBuilder = override.NewBuilder
		}
	}

	return a
}

type fakeEnv struct {
}

func (f *fakeEnv) Logger() adapter.Logger               { panic("should not be called") }
func (f *fakeEnv) ScheduleWork(fn adapter.WorkFunc)     { panic("should not be called") }
func (f *fakeEnv) ScheduleDaemon(fn adapter.DaemonFunc) { panic("should not be called") }

var _ adapter.Env = &fakeEnv{}

type fakeHandlerBuilder struct {
	panicAtSetAdapterConfig bool
	errorAtValidate         bool
	errorOnHandlerClose     bool
	handler                 *fakeHandler
}

func (f *fakeHandlerBuilder) SetAdapterConfig(adapter.Config) {
	if f.panicAtSetAdapterConfig {
		panic("panic at set adapter config")
	}
}

func (f *fakeHandlerBuilder) Validate() *adapter.ConfigErrors {
	if f.errorAtValidate {
		errs := &adapter.ConfigErrors{}
		errs.Append("field", fmt.Errorf("some validation error"))
		return errs
	}
	return nil
}

func (f *fakeHandlerBuilder) Build(context.Context, adapter.Env) (adapter.Handler, error) {
	f.handler = &fakeHandler{
		errorOnHandlerClose: f.errorOnHandlerClose,
	}
	return f.handler, nil
}

var _ adapter.HandlerBuilder = &fakeHandlerBuilder{}

type fakeHandler struct {
	closeCalled         bool
	errorOnHandlerClose bool
}

func (f *fakeHandler) Close() error {
	f.closeCalled = true

	if f.errorOnHandlerClose {
		return fmt.Errorf("error on close!")
	}

	return nil
}

var _ adapter.Handler = &fakeHandler{}
