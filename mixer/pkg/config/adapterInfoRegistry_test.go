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

package config

import (
	"errors"
	"testing"

	"github.com/gogo/protobuf/types"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/handler"
	"istio.io/mixer/pkg/template"
	"istio.io/mixer/template/sample"
	sample_report "istio.io/mixer/template/sample/report"
)

type TestBuilderInfoInventory struct {
	name string
}

func createBuilderInfo(name string) handler.Info {
	return handler.Info{
		Name:                 name,
		Description:          "mock adapter for testing",
		CreateHandlerBuilder: func() adapter.HandlerBuilder { return fakeHandlerBuilder{} },
		SupportedTemplates:   []string{sample_report.TemplateName},
		DefaultConfig:        &types.Empty{},
		ValidateConfig:       func(c adapter.Config) *adapter.ConfigErrors { return nil },
	}
}

func (t *TestBuilderInfoInventory) getNewGetBuilderInfoFn() handler.Info {
	return createBuilderInfo(t.name)
}

type fakeHandlerBuilder struct{}

func (fakeHandlerBuilder) ConfigureSampleHandler(map[string]*sample_report.Type) error { return nil }
func (fakeHandlerBuilder) Build(adapter.Config, adapter.Env) (adapter.Handler, error) {
	return fakeHandler{}, nil
}

type fakeHandler struct{}

func (fakeHandler) Close() error { return nil }
func (fakeHandler) HandleSample([]*sample_report.Instance) error {
	return errors.New("not implemented")
}

func fakeValidateSupportedTmpl(hndlrBuilder adapter.HandlerBuilder, t string) (bool, string) {
	// always succeed
	return true, ""
}

func TestRegisterSampleProcessor(t *testing.T) {
	testBuilderInfoInventory := TestBuilderInfoInventory{"foo"}
	reg := newRegistry2([]handler.InfoFn{testBuilderInfoInventory.getNewGetBuilderInfoFn},
		template.NewRepository(sample.SupportedTmplInfo).SupportsTemplate)

	builderInfo, ok := reg.FindAdapterInfo(testBuilderInfoInventory.name)
	if !ok {
		t.Errorf("No builderInfo by name %s, expected %v", testBuilderInfoInventory.name, testBuilderInfoInventory)
	}

	testBuilderInfoObj := testBuilderInfoInventory.getNewGetBuilderInfoFn()
	if testBuilderInfoObj.Name != builderInfo.Name {
		t.Errorf("reg.FindBuilderInfo(%s) expected builderInfo '%v', actual '%v'", testBuilderInfoObj.Name, testBuilderInfoObj, builderInfo)
	}
}

func TestCollisionSameNameAdapter(t *testing.T) {
	testBuilderInfoInventory := TestBuilderInfoInventory{"some name that they both have"}
	testBuilderInfoInventory2 := TestBuilderInfoInventory{"some name that they both have"}

	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected to recover from panic registering duplicate adapter, but recover was nil.")
		}
	}()

	_ = newRegistry2([]handler.InfoFn{
		testBuilderInfoInventory.getNewGetBuilderInfoFn,
		testBuilderInfoInventory2.getNewGetBuilderInfoFn}, fakeValidateSupportedTmpl,
	)

	t.Error("Should not reach this statement due to panic.")
}

func TestMissingDefaultValue(t *testing.T) {
	builderCreatorInventory := TestBuilderInfoInventory{"foo"}
	builderInfo := builderCreatorInventory.getNewGetBuilderInfoFn()
	builderInfo.DefaultConfig = nil

	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected to recover from panic due to missing DefaultValue in Info, " +
				"but recover was nil.")
		}
	}()

	_ = newRegistry2([]handler.InfoFn{func() handler.Info { return builderInfo }}, fakeValidateSupportedTmpl)

	t.Error("Should not reach this statement due to panic.")
}

func TestMissingValidateConfigFn(t *testing.T) {
	builderCreatorInventory := TestBuilderInfoInventory{"foo"}
	builderInfo := builderCreatorInventory.getNewGetBuilderInfoFn()
	builderInfo.ValidateConfig = nil

	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected to recover from panic due to missing ValidateConfig in Info, " +
				"but recover was nil.")
		}
	}()

	_ = newRegistry2([]handler.InfoFn{func() handler.Info { return builderInfo }}, fakeValidateSupportedTmpl)

	t.Error("Should not reach this statement due to panic.")
}

func TestHandlerMap(t *testing.T) {
	testBuilderInfoInventory := TestBuilderInfoInventory{"foo"}
	testBuilderInfoInventory2 := TestBuilderInfoInventory{"bar"}

	mp := AdapterInfoMap([]handler.InfoFn{
		testBuilderInfoInventory.getNewGetBuilderInfoFn,
		testBuilderInfoInventory2.getNewGetBuilderInfoFn,
	}, fakeValidateSupportedTmpl)

	if _, found := mp["foo"]; !found {
		t.Error("got nil, want foo")
	}
	if _, found := mp["bar"]; !found {
		t.Error("got nil, want bar")
	}
}

type badHandlerBuilder struct{}

func (badHandlerBuilder) DefaultConfig() adapter.Config                       { return nil }
func (badHandlerBuilder) ValidateConfig(adapter.Config) *adapter.ConfigErrors { return nil }

// This misspelled function cause the Builder to not implement SampleProcessorBuilder
func (fakeHandlerBuilder) MisspelledXXConfigureSample(map[string]*sample_report.Type) error {
	return nil
}
func (badHandlerBuilder) Build(adapter.Config, adapter.Env) (adapter.Handler, error) {
	return fakeHandler{}, nil
}

func TestBuilderNotImplementRightTemplateInterface(t *testing.T) {
	badHandlerBuilderBuilderInfo1 := func() handler.Info {
		return handler.Info{
			Name:                 "badAdapter1",
			Description:          "mock adapter for testing",
			DefaultConfig:        &types.Empty{},
			ValidateConfig:       func(c adapter.Config) *adapter.ConfigErrors { return nil },
			CreateHandlerBuilder: func() adapter.HandlerBuilder { return badHandlerBuilder{} },
			SupportedTemplates:   []string{sample_report.TemplateName},
		}
	}
	badHandlerBuilderBuilderInfo2 := func() handler.Info {
		return handler.Info{
			Name:                 "badAdapter1",
			Description:          "mock adapter for testing",
			DefaultConfig:        &types.Empty{},
			ValidateConfig:       func(c adapter.Config) *adapter.ConfigErrors { return nil },
			CreateHandlerBuilder: func() adapter.HandlerBuilder { return badHandlerBuilder{} },
			SupportedTemplates:   []string{sample_report.TemplateName},
		}
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected to recover from panic registering bad builder that does not implement Builders " +
				"for all supported templates, but recover was nil.")
		}
	}()

	_ = newRegistry2([]handler.InfoFn{
		badHandlerBuilderBuilderInfo1, badHandlerBuilderBuilderInfo2}, template.NewRepository(sample.SupportedTmplInfo).SupportsTemplate,
	)

	t.Error("Should not reach this statement due to panic.")
}
