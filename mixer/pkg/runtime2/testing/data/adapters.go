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

package data

import (
	"context"
	"fmt"

	"github.com/gogo/protobuf/types"
	"istio.io/istio/mixer/pkg/adapter"
)

func BuildAdapters(override *adapter.Info) map[string]*adapter.Info {
	var a = map[string]*adapter.Info{
		"a1": {
			Name:               "a1",
			DefaultConfig:      &types.Empty{},
			SupportedTemplates: []string{"t1"},
			NewBuilder: func() adapter.HandlerBuilder {
				return &FakeHandlerBuilder{}
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

type FakeEnv struct {
}

func (f *FakeEnv) Logger() adapter.Logger               { panic("should not be called") }
func (f *FakeEnv) ScheduleWork(fn adapter.WorkFunc)     { panic("should not be called") }
func (f *FakeEnv) ScheduleDaemon(fn adapter.DaemonFunc) { panic("should not be called") }

var _ adapter.Env = &FakeEnv{}

type FakeHandlerBuilder struct {
	PanicAtSetAdapterConfig bool
	ErrorAtValidate         bool
	ErrorOnHandlerClose     bool
	Handler                 *FakeHandler
}

func (f *FakeHandlerBuilder) SetAdapterConfig(adapter.Config) {
	if f.PanicAtSetAdapterConfig {
		panic("panic at set adapter config")
	}
}

func (f *FakeHandlerBuilder) Validate() *adapter.ConfigErrors {
	if f.ErrorAtValidate {
		errs := &adapter.ConfigErrors{}
		errs.Append("field", fmt.Errorf("some validation error"))
		return errs
	}
	return nil
}

func (f *FakeHandlerBuilder) Build(context.Context, adapter.Env) (adapter.Handler, error) {
	f.Handler = &FakeHandler{
		ErrorOnHandlerClose: f.ErrorOnHandlerClose,
	}
	return f.Handler, nil
}

var _ adapter.HandlerBuilder = &FakeHandlerBuilder{}

type FakeHandler struct {
	CloseCalled         bool
	ErrorOnHandlerClose bool
}

func (f *FakeHandler) Close() error {
	f.CloseCalled = true

	if f.ErrorOnHandlerClose {
		return fmt.Errorf("error on close!")
	}

	return nil
}

var _ adapter.Handler = &FakeHandler{}
