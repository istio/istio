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

// BuildAdapters builds a standard set of testing adapters. The supplied override is used to override entries in the
// 'a1' adapter.
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

// FakeEnv is a dummy implementation of adapter.Env
type FakeEnv struct {
}

// Logger is an implementation of adapter.Env.Logger.
func (f *FakeEnv) Logger() adapter.Logger               { panic("should not be called") }

// ScheduleWork is an implementation of adapter.Env.ScheduleWork.
func (f *FakeEnv) ScheduleWork(fn adapter.WorkFunc)     { panic("should not be called") }

// ScheduleDaemon is an implementation of adapter.Env.ScheduleDaemon.
func (f *FakeEnv) ScheduleDaemon(fn adapter.DaemonFunc) { panic("should not be called") }

var _ adapter.Env = &FakeEnv{}

// FakeHandlerBuilder is a fake of HandlerBuilder.
type FakeHandlerBuilder struct {
	PanicAtSetAdapterConfig bool
	ErrorAtValidate         bool
	ErrorOnHandlerClose     bool
	Handler                 *FakeHandler
}

// SetAdapterConfig is an implementation of HandlerBuilder.SetAdapterConfig.
func (f *FakeHandlerBuilder) SetAdapterConfig(adapter.Config) {
	if f.PanicAtSetAdapterConfig {
		panic("panic at set adapter config")
	}
}

// Validate is an implementation of HandlerBuilder.Validate.
func (f *FakeHandlerBuilder) Validate() *adapter.ConfigErrors {
	if f.ErrorAtValidate {
		errs := &adapter.ConfigErrors{}
		errs.Append("field", fmt.Errorf("some validation error"))
		return errs
	}
	return nil
}

// Build is an implementation of HandlerBuilder.Build.
func (f *FakeHandlerBuilder) Build(context.Context, adapter.Env) (adapter.Handler, error) {
	f.Handler = &FakeHandler{
		ErrorOnHandlerClose: f.ErrorOnHandlerClose,
	}
	return f.Handler, nil
}

var _ adapter.HandlerBuilder = &FakeHandlerBuilder{}

// FakeHandler is a fake implementation of adapter.Handler.
type FakeHandler struct {
	CloseCalled         bool
	ErrorOnHandlerClose bool
}

// Close is an implementation of adapter.Handler.Close.
func (f *FakeHandler) Close() error {
	f.CloseCalled = true

	if f.ErrorOnHandlerClose {
		return fmt.Errorf("error on close")
	}

	return nil
}

var _ adapter.Handler = &FakeHandler{}
