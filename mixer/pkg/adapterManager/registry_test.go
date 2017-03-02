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
	"flag"
	"fmt"
	"reflect"
	"testing"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/aspect"
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

func (denyBuilder) NewDenialsAspect(env adapter.Env, cfg adapter.AspectConfig) (adapter.DenialsAspect, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestRegisterDenyChecker(t *testing.T) {
	reg := newRegistry(nil)
	builder := denyBuilder{testBuilder{name: "foo"}}

	reg.RegisterDenialsBuilder(builder)

	impl, ok := reg.FindBuilder(builder.Name())
	if !ok {
		t.Errorf("No builder by impl with name %s, expected builder: %v", builder.Name(), builder)
	}

	if deny, ok := impl.(denyBuilder); !ok || deny != builder {
		t.Errorf("reg.ByImpl(%s) expected builder '%v', actual '%v'", builder.Name(), builder, impl)
	}
}

type listBuilder struct{ testBuilder }
type listBuilder2 struct{ listBuilder }

func (listBuilder) NewListsAspect(env adapter.Env, cfg adapter.AspectConfig) (adapter.ListsAspect, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestRegisterListChecker(t *testing.T) {
	reg := newRegistry(nil)
	builder := listBuilder{testBuilder{name: "foo"}}

	reg.RegisterListsBuilder(builder)

	impl, ok := reg.FindBuilder(builder.Name())
	if !ok {
		t.Errorf("No builder by impl with name %s, expected builder: %v", builder.Name(), builder)
	}

	if deny, ok := impl.(listBuilder); !ok || deny != builder {
		t.Errorf("reg.ByImpl(%s) expected builder '%v', actual '%v'", builder.Name(), builder, impl)
	}
}

type loggerBuilder struct{ testBuilder }

func (loggerBuilder) NewApplicationLogsAspect(env adapter.Env, cfg adapter.AspectConfig) (adapter.ApplicationLogsAspect, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestRegisterLogger(t *testing.T) {
	reg := newRegistry(nil)
	builder := loggerBuilder{testBuilder{name: "foo"}}

	reg.RegisterApplicationLogsBuilder(builder)

	impl, ok := reg.FindBuilder(builder.Name())
	if !ok {
		t.Errorf("No builder by impl with name %s, expected builder: %v", builder.Name(), builder)
	}

	if deny, ok := impl.(loggerBuilder); !ok || deny != builder {
		t.Errorf("reg.ByImpl(%s) expected builder '%v', actual '%v'", builder.Name(), builder, impl)
	}
}

type accessLoggerBuilder struct{ testBuilder }

func (accessLoggerBuilder) NewAccessLogsAspect(env adapter.Env, cfg adapter.AspectConfig) (adapter.AccessLogsAspect, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestRegistry_RegisterAccessLogger(t *testing.T) {
	reg := newRegistry(nil)
	builder := accessLoggerBuilder{testBuilder{name: "foo"}}

	reg.RegisterAccessLogsBuilder(builder)

	impl, ok := reg.FindBuilder(builder.Name())
	if !ok {
		t.Errorf("No builder by impl with name %s, expected builder: %v", builder.Name(), builder)
	}

	if deny, ok := impl.(accessLoggerBuilder); !ok || deny != builder {
		t.Errorf("reg.ByImpl(%s) expected builder '%v', actual '%v'", builder.Name(), builder, impl)
	}
}

type quotaBuilder struct{ testBuilder }

func (quotaBuilder) NewQuotasAspect(env adapter.Env, cfg adapter.AspectConfig, d map[string]*adapter.QuotaDefinition) (adapter.QuotasAspect, error) {
	return nil, fmt.Errorf("not implemented")
}

// enables multiple aspects for testing.
func (quotaBuilder) NewAccessLogsAspect(env adapter.Env, cfg adapter.AspectConfig) (adapter.AccessLogsAspect, error) {
	return nil, fmt.Errorf("not implemented")
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

type metricsBuilder struct{ testBuilder }

func (metricsBuilder) NewMetricsAspect(adapter.Env, adapter.AspectConfig, map[string]*adapter.MetricDefinition) (adapter.MetricsAspect, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestRegisterMetrics(t *testing.T) {
	reg := newRegistry(nil)
	builder := metricsBuilder{testBuilder{name: "foo"}}

	reg.RegisterMetricsBuilder(builder)
	impl, _ := reg.FindBuilder(builder.Name())
	if impl != builder {
		t.Errorf("Got :%#v, want: %#v", impl, builder)
	}
}

func TestCollision(t *testing.T) {
	reg := newRegistry(nil)
	name := "some name that they both have"

	a1 := listBuilder{testBuilder{name}}
	reg.RegisterListsBuilder(a1)

	if a, ok := reg.FindBuilder(name); !ok || a != a1 {
		t.Errorf("Failed to get first adapter by impl name; expected: '%v', actual: '%v'", a1, a)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected to recover from panic registering duplicate builder, but recover was nil.")
		}
	}()

	a2 := listBuilder2{listBuilder{testBuilder{name}}}
	reg.RegisterListsBuilder(a2)
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

	kinds := []string{aspect.QuotasKind.String(), aspect.AccessLogsKind.String()}

	reg.RegisterAccessLogsBuilder(builder)
	if !reflect.DeepEqual(reg.SupportedKinds(builder.Name()), kinds) {
		t.Errorf("SupportedKinds: got %s\nwant %s", reg.SupportedKinds(builder.Name()), kinds)
	}

	// register again and should be no change

	reg.RegisterAccessLogsBuilder(builder)
	if !reflect.DeepEqual(reg.SupportedKinds(builder.Name()), kinds) {
		t.Errorf("SupportedKinds: got %s\nwant %s", reg.SupportedKinds(builder.Name()), kinds)
	}

	if _, ok = reg.FindBuilder("DOES_NOT_EXIST"); ok {
		t.Errorf("Unexpectedly found builder: DOES_NOT_EXIST")
	}

	if len(reg.SupportedKinds("DOES_NOT_EXIST")) != 0 {
		t.Errorf("Unexpectedly found kinds for builder: DOES_NOT_EXIST")
	}

}

func TestBuilderMap(t *testing.T) {
	mp := BuilderMap([]adapter.RegisterFn{
		func(r adapter.Registrar) {
			r.RegisterQuotasBuilder(quotaBuilder{testBuilder{name: "quotaB"}})
		},
		func(r adapter.Registrar) {
			r.RegisterListsBuilder(listBuilder{testBuilder{name: "listB"}})
		},
	})

	if _, found := mp["listB"]; !found {
		t.Error("got nil, want listB")
	}
	if _, found := mp["quotaB"]; !found {
		t.Error("got nil, want quotaB")
	}
}

func init() {
	// bump up the log level so log-only logic runs during the tests, for correctness and coverage.
	_ = flag.Lookup("v").Value.Set("99")
}
