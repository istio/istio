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
	"context"
	"testing"

	"istio.io/istio/mixer/pkg/runtime2/testing/data"
)

func TestSafeHandlerBuilder_BuildSetAdapterConfig(t *testing.T) {
	b := &data.FakeHandlerBuilder{}
	builder := safeHandlerBuilder{b}

	if err := builder.SetAdapterConfig(nil); err != nil {
		t.Fail()
	}
}

func TestSafeHandlerBuilder_BuildSetAdapterConfig_Panic(t *testing.T) {
	b := &data.FakeHandlerBuilder{
		PanicAtSetAdapterConfig: true,
		PanicData:               "Ran out of coffee",
	}
	builder := safeHandlerBuilder{b}

	if err := builder.SetAdapterConfig(nil); err == nil {
		t.Fail()
	}
}

func TestSafeHandlerBuilder_BuildSetAdapterConfig_PanicWithNil(t *testing.T) {
	b := &data.FakeHandlerBuilder{
		PanicAtSetAdapterConfig: true,
		PanicData:               nil,
	}
	builder := safeHandlerBuilder{b}

	if err := builder.SetAdapterConfig(nil); err == nil {
		t.Fail()
	}
}

func TestSafeHandlerBuilder_Validate(t *testing.T) {
	b := &data.FakeHandlerBuilder{}
	builder := safeHandlerBuilder{b}

	if cerr, err := builder.Validate(); err != nil || cerr != nil {
		t.Fail()
	}
}

func TestSafeHandlerBuilder_Validate_Negative(t *testing.T) {
	b := &data.FakeHandlerBuilder{ErrorAtValidate: true}
	builder := safeHandlerBuilder{b}

	if cerr, err := builder.Validate(); err != nil || cerr == nil {
		t.Fail()
	}
}

func TestSafeHandlerBuilder_Validate_Panic(t *testing.T) {
	b := &data.FakeHandlerBuilder{
		PanicAtValidate: true,
		PanicData:       "is that a spider?",
	}
	builder := safeHandlerBuilder{b}

	if cerr, err := builder.Validate(); err == nil || cerr != nil {
		t.Fail()
	}
}

func TestSafeHandlerBuilder_Validate_Panic_Nil(t *testing.T) {
	b := &data.FakeHandlerBuilder{
		PanicAtValidate: true,
		PanicData:       nil,
	}
	builder := safeHandlerBuilder{b}

	if cerr, err := builder.Validate(); err == nil || cerr != nil {
		t.Fail()
	}
}

func TestSafeHandlerBuilder_Build(t *testing.T) {
	b := &data.FakeHandlerBuilder{}
	builder := safeHandlerBuilder{b}

	if h, err := builder.Build(context.TODO(), nil); err != nil || !h.IsValid() {
		t.Fail()
	}
}

func TestSafeHandlerBuilder_Build_Error(t *testing.T) {
	b := &data.FakeHandlerBuilder{
		ErrorAtBuild: true,
	}
	builder := safeHandlerBuilder{b}

	if h, err := builder.Build(context.TODO(), nil); err == nil || h.IsValid() {
		t.Fail()
	}
}

func TestSafeHandlerBuilder_Build_Panic(t *testing.T) {
	b := &data.FakeHandlerBuilder{
		PanicAtBuild: true,
		PanicData:    "cement out of bounds",
	}
	builder := safeHandlerBuilder{b}

	if h, err := builder.Build(context.TODO(), nil); err == nil || h.IsValid() {
		t.Fail()
	}
}

func TestSafeHandlerBuilder_Build_Panic_Nil(t *testing.T) {
	b := &data.FakeHandlerBuilder{
		PanicAtBuild: true,
		PanicData:    nil,
	}
	builder := safeHandlerBuilder{b}

	if h, err := builder.Build(context.TODO(), nil); err == nil || h.IsValid() {
		t.Fail()
	}
}

func TestSafeHandler_Close(t *testing.T) {
	h := &data.FakeHandler{}
	handler := SafeHandler{h}
	if err := handler.Close(); err != nil {
		t.Fail()
	}
}

func TestSafeHandler_Close_Panic(t *testing.T) {
	h := &data.FakeHandler{
		PanicOnHandlerClose: true,
		PanicData:           "I have Purgamentophobia",
	}
	handler := SafeHandler{h}
	if err := handler.Close(); err == nil {
		t.Fail()
	}
}

func TestSafeHandler_Close_Panic_Nil(t *testing.T) {
	h := &data.FakeHandler{
		PanicOnHandlerClose: true,
		PanicData:           nil,
	}
	handler := SafeHandler{h}
	if err := handler.Close(); err == nil {
		t.Fail()
	}
}

func TestSafeHandler_IsValid_False(t *testing.T) {
	handler := SafeHandler{}
	if handler.IsValid() {
		t.Fail()
	}
}

func TestSafeHandler_IsValid_True(t *testing.T) {
	handler := SafeHandler{h: &data.FakeHandler{}}
	if !handler.IsValid() {
		t.Fail()
	}
}
