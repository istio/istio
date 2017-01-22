// Copyright 2017 Google Inc.
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

package adapterManager

import (
	"fmt"
	"testing"

	"istio.io/mixer/pkg/adapter"
)

type testBuilder struct {
	name string
}

func (t testBuilder) Name() string                                              { return t.name }
func (testBuilder) Close() error                                                { return nil }
func (testBuilder) Description() string                                         { return "mock builder for testing" }
func (testBuilder) DefaultConfig() adapter.AspectConfig                         { return nil }
func (testBuilder) ValidateConfig(c adapter.AspectConfig) *adapter.ConfigErrors { return nil }

type denyBuilder struct{ testBuilder }

func (denyBuilder) NewDenyChecker(env adapter.Env, cfg adapter.AspectConfig) (adapter.DenyCheckerAspect, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestRegisterDenyChecker(t *testing.T) {
	reg := newRegistry()
	builder := denyBuilder{testBuilder{name: "foo"}}

	if err := reg.RegisterDenyChecker(builder); err != nil {
		t.Errorf("Failed to register deny adapter with err: %v", err)
	}

	impl, ok := reg.ByImpl(builder.Name())
	if !ok {
		t.Errorf("No builder by impl with name %s, expected builder: %v", builder.Name(), builder)
	}

	if deny, ok := impl.(denyBuilder); !ok || deny != builder {
		t.Errorf("reg.ByImpl(%s) expected builder '%v', actual '%v'", builder.Name(), builder, impl)
	}
}

type listBuilder struct{ testBuilder }

func (listBuilder) NewListChecker(env adapter.Env, cfg adapter.AspectConfig) (adapter.ListCheckerAspect, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestRegisterListChecker(t *testing.T) {
	reg := newRegistry()
	builder := listBuilder{testBuilder{name: "foo"}}

	if err := reg.RegisterListChecker(builder); err != nil {
		t.Errorf("Failed to register check list builder with err: %v", err)
	}

	impl, ok := reg.ByImpl(builder.Name())
	if !ok {
		t.Errorf("No builder by impl with name %s, expected builder: %v", builder.Name(), builder)
	}

	if deny, ok := impl.(listBuilder); !ok || deny != builder {
		t.Errorf("reg.ByImpl(%s) expected builder '%v', actual '%v'", builder.Name(), builder, impl)
	}
}

type loggerBuilder struct{ testBuilder }

func (loggerBuilder) NewLogger(env adapter.Env, cfg adapter.AspectConfig) (adapter.LoggerAspect, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestRegisterLogger(t *testing.T) {
	reg := newRegistry()
	builder := loggerBuilder{testBuilder{name: "foo"}}

	if err := reg.RegisterLogger(builder); err != nil {
		t.Errorf("Failed to register logging builder with err: %v", err)
	}

	impl, ok := reg.ByImpl(builder.Name())
	if !ok {
		t.Errorf("No builder by impl with name %s, expected builder: %v", builder.Name(), builder)
	}

	if deny, ok := impl.(loggerBuilder); !ok || deny != builder {
		t.Errorf("reg.ByImpl(%s) expected builder '%v', actual '%v'", builder.Name(), builder, impl)
	}
}

type quotaBuilder struct{ testBuilder }

func (quotaBuilder) NewQuota(env adapter.Env, cfg adapter.AspectConfig) (adapter.QuotaAspect, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestRegisterQuota(t *testing.T) {
	reg := newRegistry()
	builder := quotaBuilder{testBuilder{name: "foo"}}

	if err := reg.RegisterQuota(builder); err != nil {
		t.Errorf("Failed to register quota builder with err: %v", err)
	}

	impl, ok := reg.ByImpl(builder.Name())
	if !ok {
		t.Errorf("No builder by impl with name %s, expected builder: %v", builder.Name(), builder)
	}

	if deny, ok := impl.(quotaBuilder); !ok || deny != builder {
		t.Errorf("reg.ByImpl(%s) expected builder '%v', actual '%v'", builder.Name(), builder, impl)
	}
}

func TestCollision(t *testing.T) {
	reg := newRegistry()
	name := "some name that they both have"

	a1 := denyBuilder{testBuilder{name}}
	if err := reg.RegisterDenyChecker(a1); err != nil {
		t.Errorf("Failed to insert first adapter with err: %s", err)
	}
	if a, ok := reg.ByImpl(name); !ok || a != a1 {
		t.Errorf("Failed to get first adapter by impl name; expected: '%v', actual: '%v'", a1, a)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected to recover from panic registering duplicate builder, but recover was nil.")
		}
	}()

	a2 := listBuilder{testBuilder{name}}
	if err := reg.RegisterListChecker(a2); err != nil {
		t.Errorf("Expected a panic inserting duplicate  builder, got err instead: %s", err)
	}
	t.Error("Should not reach this statement due to panic.")
}
