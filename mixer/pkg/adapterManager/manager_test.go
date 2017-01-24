// Copyright 2016 Google Inc.
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
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/genproto/googleapis/rpc/status"

	istioconfig "istio.io/api/mixer/v1/config"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/expr"
)

type (
	fakemgr struct {
		kind string
		aspect.Manager
	}

	fakebag struct {
		attribute.Bag
	}

	fakeevaluator struct {
		expr.Evaluator
	}

	testManager struct {
		name     string
		throw    bool
		instance testAspect
	}

	testAspect struct {
		throw bool
	}
)

func (m *fakemgr) Kind() string {
	return m.kind
}

func newTestManager(name string, throwOnNewAspect bool, aspectThrowOnExecute bool) testManager {
	return testManager{name, throwOnNewAspect, testAspect{aspectThrowOnExecute}}
}
func (testManager) Close() error                                                { return nil }
func (testManager) DefaultConfig() adapter.AspectConfig                         { return nil }
func (testManager) ValidateConfig(c adapter.AspectConfig) *adapter.ConfigErrors { return nil }
func (testManager) Kind() string                                                { return "denyChecker" }
func (m testManager) Name() string                                              { return m.name }
func (testManager) Description() string                                         { return "deny checker aspect manager for testing" }

func (m testManager) NewAspect(cfg *aspect.CombinedConfig, adapter adapter.Builder, env adapter.Env) (aspect.Wrapper, error) {
	if m.throw {
		panic("NewAspect panic")
	}
	return m.instance, nil
}
func (m testManager) NewDenyChecker(env adapter.Env, c adapter.AspectConfig) (adapter.DenyCheckerAspect, error) {
	return m.instance, nil
}

func (testAspect) Close() error { return nil }
func (t testAspect) Execute(attrs attribute.Bag, mapper expr.Evaluator) (*aspect.Output, error) {
	if t.throw {
		panic("Execute panic")
	}
	return nil, fmt.Errorf("empty")
}
func (testAspect) Deny() status.Status { return status.Status{Code: int32(code.Code_INTERNAL)} }

func TestManager(t *testing.T) {
	mgrs := []aspect.Manager{&fakemgr{kind: "k1"}, &fakemgr{kind: "k2"}}
	m := NewManager(mgrs)
	cfg := &aspect.CombinedConfig{
		Aspect:  &istioconfig.Aspect{},
		Builder: &istioconfig.Adapter{},
	}
	attrs := &fakebag{}
	mapper := &fakeevaluator{}
	if _, err := m.Execute(cfg, attrs, mapper); err != nil {
		if !strings.Contains(err.Error(), "could not find aspect manager") {
			t.Error("excute errored out: ", err)
		}

	}
}

func TestRecovery_NewAspect(t *testing.T) {
	name := "NewAspect Throws"
	cacheThrow := newTestManager(name, true, false)
	m := NewManager([]aspect.Manager{cacheThrow})
	if err := m.Registry().RegisterDenyChecker(cacheThrow); err != nil {
		t.Errorf("Failed to register deny checker in test setup with err: %v", err)
	}

	cfg := &aspect.CombinedConfig{
		&istioconfig.Aspect{Kind: name},
		&istioconfig.Adapter{Name: name},
	}

	_, err := m.Execute(cfg, nil, nil)
	if err == nil {
		t.Error("Aspect threw, but got no err from manager.Execute")
	}
	if !strings.Contains(err.Error(), "NewAspect") {
		t.Errorf("Expected err from panic with message containing 'NewAspect', actual: %v", err)
	}
}

func TestRecovery_AspectExecute(t *testing.T) {
	name := "aspect.Execute Throws"
	aspectThrow := newTestManager(name, false, true)
	m := NewManager([]aspect.Manager{aspectThrow})
	if err := m.Registry().RegisterDenyChecker(aspectThrow); err != nil {
		t.Errorf("Failed to register deny checker in test setup with err: %v", err)
	}

	cfg := &aspect.CombinedConfig{
		&istioconfig.Aspect{Kind: name},
		&istioconfig.Adapter{Name: name},
	}

	_, err := m.Execute(cfg, nil, nil)
	if err == nil {
		t.Error("Aspect threw, but got no err from manager.Execute")
	}
	if !strings.Contains(err.Error(), "Execute") {
		t.Errorf("Expected err from panic with message containing 'Execute', actual: %v", err)
	}
}
