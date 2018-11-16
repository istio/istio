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
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/framework/runtime/dependency"
	"istio.io/istio/pkg/test/framework/runtime/registry"
	"reflect"
	"testing"
)

var (
	aID = id("a")
	bID = id("b")
	cID = id("c")
)

var _ context.Instance = &mockContext{}

//var _ component.Defaults = &defaults{}

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
	b_override := m.registerVariant(bID, "b-variant", &cID)
	c := m.registerDefault(cID)

	// Creating component a should create b and c.
	scope := lifecycle.Suite
	m.RequireOrFail(t, scope, &aID, &b_override, &cID)

	m.assertCount(3)
	m.assertDescriptor(a, scope)
	m.assertDescriptor(b_override, scope)
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
	c := m.GetComponentForDescriptor(desc)
	if c == nil {
		m.t.Fatalf("component not found for descriptor: %v", desc)
	}
	assertDescriptor(c, desc, scope, m.t)
}

type comp struct {
	desc  component.Descriptor
	scope lifecycle.Scope
}

func (c *comp) Descriptor() component.Descriptor {
	return c.desc
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

func (d *mockContext) TestID() string {
	return ""
}

func (d *mockContext) RunID() string {
	return ""
}

func (d *mockContext) NoCleanup() bool {
	return false
}

func (d *mockContext) WorkDir() string {
	return ""
}

func (d *mockContext) CreateTmpDirectory(name string) (string, error) {
	return "", fmt.Errorf("unsupported")
}

func (d *mockContext) LogOptions() *log.Options {
	return nil
}

func (d *mockContext) DumpState(context string) {}

func (d *mockContext) Evaluate(t testing.TB, tmpl string) string {
	return ""
}

func (d *mockContext) GetComponent(id component.ID) component.Instance {
	return nil
}

func (d *mockContext) GetComponentOrFail(id component.ID, t testing.TB) component.Instance {
	t.Fatalf("unsupported")
	return nil
}

func (c *mockContext) GetComponentForDescriptor(d component.Descriptor) component.Instance {
	return nil
}

func (c *mockContext) GetComponentForDescriptorOrFail(d component.Descriptor, t testing.TB) component.Instance {
	t.Fatalf("unsupported")
	return nil
}

func (d *mockContext) GetAllComponents() []component.Instance {
	return nil
}

func (c *mockContext) NewComponent(d component.Descriptor, scope lifecycle.Scope) (component.Instance, error) {
	return nil, fmt.Errorf("unsupported")
}
func (c *mockContext) NewComponentOrFail(d component.Descriptor, scope lifecycle.Scope, t testing.TB) component.Instance {
	t.Fatalf("unsupported")
	return nil
}

func (d *mockContext) Require(scope lifecycle.Scope, reqs ...component.Requirement) (component.ResolutionError, component.StartError) {
	return fmt.Errorf("unsupported"), fmt.Errorf("unsupported")
}

func (d *mockContext) RequireOrFail(t testing.TB, scope lifecycle.Scope, reqs ...component.Requirement) {
	t.Fatalf("unsupported")
}

func (d *mockContext) RequireOrSkip(t testing.TB, scope lifecycle.Scope, reqs ...component.Requirement) {
	t.Fatalf("unsupported")
}

func (d *mockContext) GetDefaultDescriptor(id component.ID) (component.Descriptor, error) {
	return component.Descriptor{}, fmt.Errorf("unsupported")
}

func (d *mockContext) GetDefaultDescriptorOrFail(id component.ID, t testing.TB) component.Descriptor {
	t.Fatalf("unsupported")
	return component.Descriptor{}
}

/*
type defaults struct {
	cmap map[component.ID]component.Descriptor
}

func newDefaults() *defaults {
	return &defaults {
		cmap: make(map[component.ID]component.Descriptor),
	}
}

func (d *defaults) registerDefault(cid component.ID, reqs ...component.Requirement) component.Descriptor {
	desc := descriptor(cid, "", reqs...)
	d.cmap[component.ID(cid)] = desc
	return desc
}

func (d *defaults) GetDefaultDescriptor(id component.ID) (component.Descriptor, error) {
	desc, ok := d.cmap[id]
	if !ok {
		return component.Descriptor{}, fmt.Errorf("missing default for %v", id)
	}
	return desc, nil
}

func (d *defaults) GetDefaultDescriptorOrFail(id component.ID, t testing.TB) component.Descriptor {
	t.Fatal("unsupported")
	return component.Descriptor{}
}*/

func descriptor(cid component.ID, variant component.Variant, reqs ...component.Requirement) component.Descriptor {
	return component.Descriptor{
		ID:       component.ID(cid),
		Variant:  component.Variant(variant),
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
func (f *factory) newComponent() (api.Component, error) {
	return &comp{
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

func (e *exp) resolutionError(resErr, startErr error) {
	if startErr != nil {
		e.t.Fatal("unexpected start error: ", startErr)
	}
	if resErr == nil {
		e.t.Fatal("expected resolution error")
	}
}
