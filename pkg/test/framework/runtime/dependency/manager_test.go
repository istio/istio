//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package dependency_test

import (
	"fmt"
	"reflect"
	"testing"

	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/framework/runtime/dependency"
	"istio.io/istio/pkg/test/framework/runtime/registry"
)

var (
	aID = id("a")
	bID = id("b")
	cID = id("c")
)

var _ context.Instance = &mockContext{}

func TestNoDependencies(t *testing.T) {
	m := newManager(t)

	a := m.registerDefault(aID)

	m.RequireOrFail(t, lifecycle.Suite, &a)
	m.assertCount(1)
}

func TestRequireIDs(t *testing.T) {
	m := newManager(t)

	a := m.registerDefault(aID, &bID, &cID)
	b := m.registerDefault(bID, &cID)
	c := m.registerDefault(cID)

	// Creating component a should create b and c.
	scope := lifecycle.Suite
	m.RequireOrFail(t, scope, &aID, &bID, &cID)

	m.assertCount(3)
	m.assertDescriptor(a, scope)
	m.assertDescriptor(b, scope)
	m.assertDescriptor(c, scope)
}

func TestRequireDescriptors(t *testing.T) {
	m := newManager(t)

	a := m.registerDefault(aID, &bID, &cID)
	b := m.registerDefault(bID, &cID)
	c := m.registerDefault(cID)

	// Creating component a should create b and c.
	scope := lifecycle.Suite
	m.RequireOrFail(t, scope, &a, &b, &c)

	m.assertCount(3)
	m.assertDescriptor(a, scope)
	m.assertDescriptor(b, scope)
	m.assertDescriptor(c, scope)
}

func TestRequireMixed(t *testing.T) {
	m := newManager(t)

	a := m.registerDefault(aID, &bID, &cID)
	b := m.registerDefault(bID, &cID)
	c := m.registerDefault(cID)

	// Creating component a should create b and c.
	scope := lifecycle.Suite
	m.RequireOrFail(t, scope, &aID, &b, &cID)

	m.assertCount(3)
	m.assertDescriptor(a, scope)
	m.assertDescriptor(b, scope)
	m.assertDescriptor(c, scope)
}

func TestDependencyDescriptors(t *testing.T) {
	m := newManager(t)

	c := m.registerDefault(cID)
	b := m.registerDefault(bID, &c)
	a := m.registerDefault(aID, &b, &c)

	// Creating component a should create b and c.
	scope := lifecycle.Suite
	m.RequireOrFail(t, scope, &a, &b, &c)

	m.assertCount(3)
	m.assertDescriptor(a, scope)
	m.assertDescriptor(b, scope)
	m.assertDescriptor(c, scope)
}

func TestOverrideDefault(t *testing.T) {
	m := newManager(t)

	a := m.registerDefault(aID, &bID, &cID)
	_ = m.registerDefault(bID, &cID)
	bOverride := m.registerVariant(bID, "b-variant", &cID)
	c := m.registerDefault(cID)

	// Creating component a should create b and c.
	scope := lifecycle.Suite
	m.RequireOrFail(t, scope, &aID, &bOverride, &cID)

	m.assertCount(3)
	m.assertDescriptor(a, scope)
	m.assertDescriptor(bOverride, scope)
	m.assertDescriptor(c, scope)
}

func TestConflictingDuplicateFails(t *testing.T) {
	m := newManager(t)

	// Creating component a should create b and c.
	scope := lifecycle.Suite
	expect(t).resolutionError(m.Require(scope, descriptorRef(aID, "v1"), descriptorRef(aID, "v2")))
}

func TestIdenticalDuplicateSucceeds(t *testing.T) {
	m := newManager(t)
	m.registerDefault(aID)

	a := descriptor(aID, "")

	// Creating component a should create b and c.
	scope := lifecycle.Suite
	m.RequireOrFail(t, scope, &a, &a)

	m.assertCount(1)
	m.assertDescriptor(a, scope)
}

func TestRequireExistingComponentSucceeds(t *testing.T) {
	m := newManager(t)

	// Creating component a should create b and c.
	scope := lifecycle.Suite

	a := m.registerDefault(aID, &bID)
	b := m.registerDefault(bID)

	// First create b
	m.RequireOrFail(t, scope, &b)
	m.assertCount(1)
	m.assertDescriptor(b, scope)

	// Now create a
	m.RequireOrFail(t, scope, &a)
	m.assertCount(2)
	m.assertDescriptor(a, scope)
}

func TestRequireConflictingExistingComponentFails(t *testing.T) {
	m := newManager(t)

	// Creating component a should create b and c.
	scope := lifecycle.Suite

	v1 := m.registerVariant(aID, "v1")
	v2 := m.registerVariant(aID, "v2")

	// First create v1
	m.RequireOrFail(t, scope, &v1)
	m.assertCount(1)
	m.assertDescriptor(v1, scope)

	// Now create v2
	expect(t).resolutionError(m.Require(scope, &v2))
}

func TestRequireGreaterScopeThanExistingComponentFails(t *testing.T) {
	m := newManager(t)

	a := m.registerDefault(aID)

	// First create a with Test scope
	m.RequireOrFail(t, lifecycle.Test, &a)
	m.assertCount(1)
	m.assertDescriptor(a, lifecycle.Test)

	// Now create a with Suite scope
	expect(t).resolutionError(m.Require(lifecycle.Suite, &a))
}

func TestRequireLesserScopeThanExistingComponentSucceeds(t *testing.T) {
	m := newManager(t)

	a := m.registerDefault(aID)

	// First create a with Suite scope
	m.RequireOrFail(t, lifecycle.Suite, &a)
	m.assertCount(1)
	m.assertDescriptor(a, lifecycle.Suite)

	// Now create a with Test scope
	m.RequireOrFail(t, lifecycle.Test, &a)
	m.assertCount(1)
	m.assertDescriptor(a, lifecycle.Suite)
}

func TestRequireIdenticalExistingComponentSucceeds(t *testing.T) {
	m := newManager(t)

	// Creating component a should create b and c.
	scope := lifecycle.Suite

	a := m.registerDefault(aID)

	// Create a
	m.RequireOrFail(t, scope, &a)
	m.assertCount(1)
	m.assertDescriptor(a, scope)

	// Now create a again
	m.RequireOrFail(t, scope, &a)
	m.assertCount(1)
	m.assertDescriptor(a, scope)
}

func TestMissingDefaultFails(t *testing.T) {
	m := newManager(t)

	// Creating component a should create b and c.
	scope := lifecycle.Suite

	expect(t).resolutionError(m.Require(scope, descriptorRef(aID, "", &bID)))
}

func TestDependencyWithGreaterScopeSucceeds(t *testing.T) {
	m := newManager(t)

	a := m.registerDefault(aID, &bID)
	b := m.registerDefault(bID)

	// Create b with Suite scope.
	m.RequireOrFail(t, lifecycle.Suite, &b)
	m.assertCount(1)
	m.assertDescriptor(b, lifecycle.Suite)

	// Now create a with Test scope.
	m.RequireOrFail(t, lifecycle.Test, &a)
	m.assertCount(2)
	m.assertDescriptor(a, lifecycle.Test)
}

func TestDependencyWithLesserScopeFails(t *testing.T) {
	m := newManager(t)

	a := m.registerDefault(aID, &bID)
	b := m.registerDefault(bID)

	// Create b with Test scope.
	m.RequireOrFail(t, lifecycle.Test, &b)
	m.assertCount(1)
	m.assertDescriptor(b, lifecycle.Test)

	// Now create a with Suite scope.
	expect(t).resolutionError(m.Require(lifecycle.Suite, &a))
}

func TestNamedIds(t *testing.T) {
	m := newManager(t)

	a := m.registerDefault(aID)
	req1 := component.NewNamedRequirement("one", &aID)
	req2 := component.NewNamedRequirement("two", &aID)

	m.RequireOrFail(t, lifecycle.Test, req1, req2)
	m.assertCount(2)
	m.assertNamedDescriptor("one", a, lifecycle.Test)
	m.assertNamedDescriptor("two", a, lifecycle.Test)
}

func assertDescriptor(c component.Instance, desc component.Descriptor, scope lifecycle.Scope, t *testing.T) {
	t.Helper()
	if c.Scope() != scope {
		t.Fatalf("expected %v, found %v", scope, c.Scope())
	}
	if !reflect.DeepEqual(c.Descriptor(), desc) {
		t.Fatalf("expected %v, found %v", desc, c.Descriptor())
	}
}

type manager struct {
	*dependency.Manager
	reg *registry.Instance
	t   *testing.T
}

func newManager(t *testing.T) *manager {
	t.Helper()
	c := &mockContext{}
	r := registry.New()
	return &manager{
		Manager: dependency.NewManager(c, r),
		reg:     r,
		t:       t,
	}
}

func (m *manager) registerDefault(cid component.ID, reqs ...component.Requirement) component.Descriptor {
	desc := descriptor(cid, "", reqs...)
	m.reg.Register(desc, true, newFactory(desc).newComponent)
	return desc
}

func (m *manager) registerVariant(cid component.ID, variant component.Variant, reqs ...component.Requirement) component.Descriptor {
	desc := descriptor(cid, variant, reqs...)
	m.reg.Register(desc, false, newFactory(desc).newComponent)
	return desc
}

func (m *manager) assertCount(expected int) {
	actual := len(m.GetAllComponents())
	if actual != expected {
		m.t.Fatalf("incorrect component count. Expected %d, actual %d", expected, actual)
	}
}

func (m *manager) assertDescriptor(desc component.Descriptor, scope lifecycle.Scope) {
	m.assertNamedDescriptor("", desc, scope)
}

func (m *manager) assertNamedDescriptor(name string, desc component.Descriptor, scope lifecycle.Scope) {
	c := m.GetComponentForDescriptor(name, desc)
	if c == nil {
		m.t.Fatalf("component not found for descriptor:  (%v) %v", name, desc)
	}
	assertDescriptor(c, desc, scope, m.t)
}

type comp struct {
	name   string
	desc   component.Descriptor
	config component.Configuration
	scope  lifecycle.Scope
}

func (c *comp) Name() string {
	return c.name
}

func (c *comp) Descriptor() component.Descriptor {
	return c.desc
}

func (c *comp) Configuration() component.Configuration {
	return c.config
}

func (c *comp) Scope() lifecycle.Scope {
	return c.scope
}

func (c *comp) Start(ctx context.Instance, scope lifecycle.Scope) error {
	c.scope = scope
	return nil
}

func id(i string) component.ID {
	return component.ID(i)
}

type mockContext struct {
}

func (c *mockContext) TestID() string {
	return ""
}

func (c *mockContext) RunID() string {
	return ""
}

func (c *mockContext) NoCleanup() bool {
	return false
}

func (c *mockContext) WorkDir() string {
	return ""
}

func (c *mockContext) CreateTmpDirectory(name string) (string, error) {
	return "", fmt.Errorf("unsupported")
}

func (c *mockContext) LogOptions() *log.Options {
	return nil
}

func (c *mockContext) DumpState(context string) {}

func (c *mockContext) Evaluate(t testing.TB, tmpl string) string {
	return ""
}

func (c *mockContext) GetComponent(name string, id component.ID) component.Instance {
	return nil
}

func (c *mockContext) GetComponentOrFail(name string, id component.ID, t testing.TB) component.Instance {
	t.Fatalf("unsupported")
	return nil
}

func (c *mockContext) GetComponentForDescriptor(name string, d component.Descriptor) component.Instance {
	return nil
}

func (c *mockContext) GetComponentForDescriptorOrFail(name string, d component.Descriptor, t testing.TB) component.Instance {
	t.Fatalf("unsupported")
	return nil
}

func (c *mockContext) GetAllComponents() []component.Instance {
	return nil
}

func (c *mockContext) NewComponent(name string, d component.Descriptor, scope lifecycle.Scope) (component.Instance, error) {
	return nil, fmt.Errorf("unsupported")
}

func (c *mockContext) NewComponentOrFail(name string, d component.Descriptor, scope lifecycle.Scope, t testing.TB) component.Instance {
	t.Fatalf("unsupported")
	return nil
}

func (c *mockContext) Require(scope lifecycle.Scope, reqs ...component.Requirement) component.RequirementError {
	return nil
}

func (c *mockContext) RequireOrFail(t testing.TB, scope lifecycle.Scope, reqs ...component.Requirement) {
	t.Fatalf("unsupported")
}

func (c *mockContext) RequireOrSkip(t testing.TB, scope lifecycle.Scope, reqs ...component.Requirement) {
	t.Fatalf("unsupported")
}

func (c *mockContext) GetDefaultDescriptor(id component.ID) (component.Descriptor, error) {
	return component.Descriptor{}, fmt.Errorf("unsupported")
}

func (c *mockContext) GetDefaultDescriptorOrFail(id component.ID, t testing.TB) component.Descriptor {
	t.Fatalf("unsupported")
	return component.Descriptor{}
}

func descriptor(cid component.ID, variant component.Variant, reqs ...component.Requirement) component.Descriptor {
	return component.Descriptor{
		ID:       cid,
		Variant:  variant,
		Requires: reqs,
	}
}

func descriptorRef(cid component.ID, variant component.Variant, reqs ...component.Requirement) *component.Descriptor {
	d := descriptor(cid, variant, reqs...)
	return &d
}

type factory struct {
	desc component.Descriptor
}

func newFactory(desc component.Descriptor) *factory {
	return &factory{desc}
}

// Factory function for components.
func (f *factory) newComponent() (api.Component, error) { // nolint: unparam
	return &comp{
		name: "",
		desc: f.desc,
	}, nil
}

type exp struct {
	t *testing.T
}

func expect(t *testing.T) *exp {
	t.Helper()
	return &exp{t}
}

func (e *exp) resolutionError(err component.RequirementError) {
	if err == nil {
		e.t.Fatal("expected resolution error")
	}
	if err.IsStartError() {
		e.t.Fatal("unexpected start error: ", err)
	}
}
