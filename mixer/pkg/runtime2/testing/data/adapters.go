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

package data

import (
	"context"
	"fmt"

	"github.com/gogo/protobuf/types"

	"istio.io/istio/mixer/pkg/adapter"
)

// BuildAdapters builds a standard set of testing adapters. The supplied settings is used to override behavior.
func BuildAdapters(settings ...FakeAdapterSettings) map[string]*adapter.Info {
	m := make(map[string]FakeAdapterSettings)
	for _, setting := range settings {
		m[setting.Name] = setting
	}

	var a = map[string]*adapter.Info{
		"acheck": createFakeAdapter("acheck", m["acheck"], "tcheck", "thalt"),
		"apa":    createFakeAdapter("apa", m["apa"], "tapa"),
	}

	return a
}

func createFakeAdapter(name string, s FakeAdapterSettings, defaultTemplates ...string) *adapter.Info {
	templates := defaultTemplates
	if s.SupportedTemplates != nil {
		templates = s.SupportedTemplates
	}

	// A healthy adapter that implements the check interface. It's behavior is configurable.
	return &adapter.Info{
		Name:               name,
		DefaultConfig:      &types.Struct{},
		SupportedTemplates: templates,
		NewBuilder: func() adapter.HandlerBuilder {
			if s.NilBuilder {
				return nil
			}

			return &FakeHandlerBuilder{
				settings: s,
			}
		},
	}
}

// FakeEnv is a dummy implementation of adapter.Env
type FakeEnv struct {
}

// Logger is an implementation of adapter.Env.Logger.
func (f *FakeEnv) Logger() adapter.Logger { panic("should not be called") }

// ScheduleWork is an implementation of adapter.Env.ScheduleWork.
func (f *FakeEnv) ScheduleWork(fn adapter.WorkFunc) { panic("should not be called") }

// ScheduleDaemon is an implementation of adapter.Env.ScheduleDaemon.
func (f *FakeEnv) ScheduleDaemon(fn adapter.DaemonFunc) { panic("should not be called") }

var _ adapter.Env = &FakeEnv{}

// FakeHandlerBuilder is a fake of HandlerBuilder.
type FakeHandlerBuilder struct {
	settings FakeAdapterSettings
}

// SetAdapterConfig is an implementation of HandlerBuilder.SetAdapterConfig.
func (f *FakeHandlerBuilder) SetAdapterConfig(cfg adapter.Config) {
	if f.settings.PanicAtSetAdapterConfig {
		panic(f.settings.PanicData)
	}
}

// Validate is an implementation of HandlerBuilder.Validate.
func (f *FakeHandlerBuilder) Validate() *adapter.ConfigErrors {
	if f.settings.PanicAtValidate {
		panic(f.settings.PanicData)
	}

	if f.settings.ErrorAtValidate {
		errs := &adapter.ConfigErrors{}
		errs = errs.Append("field", fmt.Errorf("some validation error"))
		return errs
	}
	return nil
}

// Build is an implementation of HandlerBuilder.Build.
func (f *FakeHandlerBuilder) Build(context.Context, adapter.Env) (adapter.Handler, error) {
	if f.settings.PanicAtBuild {
		panic(f.settings.PanicData)
	}

	if f.settings.ErrorAtBuild {
		return nil, fmt.Errorf("this adapter is not available at the moment, please come back later")
	}

	if f.settings.NilHandlerAtBuild {
		return nil, nil
	}

	handler := &FakeHandler{
		settings: f.settings,
	}

	return handler, nil
}

var _ adapter.HandlerBuilder = &FakeHandlerBuilder{}

// FakeHandler is a fake implementation of adapter.Handler.
type FakeHandler struct {
	settings FakeAdapterSettings
}

// Close is an implementation of adapter.Handler.Close.
func (f *FakeHandler) Close() error {
	if f.settings.CloseCalled != nil {
		*f.settings.CloseCalled = true
	}

	if f.settings.PanicAtHandlerClose {
		panic(f.settings.PanicData)
	}

	if f.settings.ErrorAtHandlerClose {
		return fmt.Errorf("error on close")
	}

	return nil
}

var _ adapter.Handler = &FakeHandler{}

// FakeAdapterSettings describes the behavior of a fake adapter.
type FakeAdapterSettings struct {
	Name                    string
	PanicAtSetAdapterConfig bool
	PanicData               interface{}
	PanicAtValidate         bool
	ErrorAtValidate         bool
	ErrorAtBuild            bool
	PanicAtBuild            bool
	NilBuilder              bool
	NilHandlerAtBuild       bool
	ErrorAtHandlerClose     bool
	PanicAtHandlerClose     bool

	SupportedTemplates []string

	CloseCalled *bool
}
