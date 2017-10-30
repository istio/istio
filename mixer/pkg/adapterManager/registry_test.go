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

package adapterManager

import (
	"errors"
	"flag"
	"reflect"
	"testing"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/config"
)

type testBuilder struct {
	name string
}

func (t testBuilder) Name() string                                        { return t.name }
func (testBuilder) Close() error                                          { return nil }
func (testBuilder) Description() string                                   { return "mock builder for testing" }
func (testBuilder) DefaultConfig() adapter.Config                         { return nil }
func (testBuilder) ValidateConfig(c adapter.Config) *adapter.ConfigErrors { return nil }

type quotaBuilder struct{ testBuilder }
type quotaBuilder2 struct{ quotaBuilder }

func (quotaBuilder) NewQuotasAspect(env adapter.Env, cfg adapter.Config, d map[string]*adapter.QuotaDefinition) (adapter.QuotasAspect, error) {
	return nil, errors.New("not implemented")
}

// enables multiple aspects for testing.
func (quotaBuilder) BuildAttributesGenerator(env adapter.Env, c adapter.Config) (adapter.AttributesGenerator, error) {
	return nil, errors.New("not implemented")
}

func TestRegisterQuota(t *testing.T) {
	reg := newRegistry(nil)
	builder := quotaBuilder{testBuilder{name: "foo"}}

	reg.RegisterQuotasBuilder(builder)
	impl, ok := reg.FindBuilder(builder.Name())
	if !ok {
		t.Errorf("No builder by impl with name %s, expected builder: %v", builder.Name(), builder)
	}

	if deny, ok := impl.(quotaBuilder); !ok || deny != builder {
		t.Errorf("reg.ByImpl(%s) expected builder '%v', actual '%v'", builder.Name(), builder, impl)
	}
}

func TestCollision(t *testing.T) {
	reg := newRegistry(nil)
	name := "some name that they both have"

	a1 := quotaBuilder{testBuilder{name}}
	reg.RegisterQuotasBuilder(a1)

	if a, ok := reg.FindBuilder(name); !ok || a != a1 {
		t.Errorf("Failed to get first adapter by impl name; expected: '%v', actual: '%v'", a1, a)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected to recover from panic registering duplicate builder, but recover was nil.")
		}
	}()

	a2 := quotaBuilder2{quotaBuilder{testBuilder{name}}}
	reg.RegisterQuotasBuilder(a2)
	t.Error("Should not reach this statement due to panic.")
}

func TestMultiKinds(t *testing.T) {
	builder := quotaBuilder{testBuilder{name: "foo"}}
	reg := newRegistry([]adapter.RegisterFn{
		func(r adapter.Registrar) {
			r.RegisterQuotasBuilder(builder)
		},
	})

	impl, ok := reg.FindBuilder(builder.Name())
	if !ok {
		t.Errorf("No builder by impl with name %s, expected builder: %v", builder.Name(), builder)
	}
	var deny quotaBuilder
	if deny, ok = impl.(quotaBuilder); !ok || deny != builder {
		t.Errorf("reg.ByImpl(%s) expected builder '%v', actual '%v'", builder.Name(), builder, impl)
	}

	// register as accessLog

	kinds := config.KindSet(0).Set(config.QuotasKind).Set(config.AttributesKind)

	reg.RegisterAttributesGeneratorBuilder(builder)
	if !reflect.DeepEqual(reg.SupportedKinds(builder.Name()), kinds) {
		t.Errorf("SupportedKinds: got %s\nwant %s", reg.SupportedKinds(builder.Name()), kinds)
	}

	// register again and should be no change

	reg.RegisterAttributesGeneratorBuilder(builder)
	if !reflect.DeepEqual(reg.SupportedKinds(builder.Name()), kinds) {
		t.Errorf("SupportedKinds: got %s\nwant %s", reg.SupportedKinds(builder.Name()), kinds)
	}

	if _, ok = reg.FindBuilder("DOES_NOT_EXIST"); ok {
		t.Error("Unexpectedly found builder: DOES_NOT_EXIST")
	}

	if reg.SupportedKinds("DOES_NOT_EXIST") != 0 {
		t.Error("Unexpectedly found kinds for builder: DOES_NOT_EXIST")
	}

}

func TestBuilderMap(t *testing.T) {
	mp := BuilderMap([]adapter.RegisterFn{
		func(r adapter.Registrar) {
			r.RegisterQuotasBuilder(quotaBuilder{testBuilder{name: "quotaB"}})
		},
	})

	if _, found := mp["quotaB"]; !found {
		t.Error("got nil, want quotaB")
	}
}

func init() {
	// bump up the log level so log-only logic runs during the tests, for correctness and coverage.
	_ = flag.Lookup("v").Value.Set("99")
}
