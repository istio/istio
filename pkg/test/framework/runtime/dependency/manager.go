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

package dependency

import (
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/framework/runtime/registry"
	"istio.io/istio/pkg/test/scopes"
)

var _ component.Factory = &Manager{}
var _ component.Repository = &Manager{}
var _ component.Resolver = &Manager{}
var _ api.Resettable = &Manager{}
var _ io.Closer = &Manager{}

// Manager for the dependencies for all deployments.
type Manager struct {
	component.Defaults

	ctx context.Instance

	// The underlying Factory used for creating new component instances.
	registry *registry.Instance

	// A map of all components
	compMap map[namedId]*compEntry

	// A list of all components in creation order.
	all []*compEntry
}

type compEntry struct {
	id   namedId
	comp api.Component
}

// NewManager creates a new Manager instance.
func NewManager(ctx context.Instance, registry *registry.Instance) *Manager {
	return &Manager{
		Defaults: registry,
		ctx:      ctx,
		registry: registry,
		compMap:  make(map[namedId]*compEntry),
	}
}

// Require implements the component.Resolver interface
func (m *Manager) Require(scope lifecycle.Scope, reqs ...component.Requirement) component.RequirementError {
	// Two phase processing, first gather all the requirements then create them.
	depMgr := newCreationProcessor(m, scope)
	if err := depMgr.ProcessRequirements(reqs); err != nil {
		return err
	}
	if err := depMgr.ApplyDefaults(); err != nil {
		return err
	}
	return depMgr.CreateComponents()
}

// RequireOrFail implements the component.Resolver interface
func (m *Manager) RequireOrFail(t testing.TB, scope lifecycle.Scope, reqs ...component.Requirement) {
	t.Helper()
	if err := m.Require(scope, reqs...); err != nil {
		t.Fatal(err)
	}
}

// RequireOrSkip implements the component.Resolver interface
func (m *Manager) RequireOrSkip(t testing.TB, scope lifecycle.Scope, reqs ...component.Requirement) {
	t.Helper()
	if err := m.Require(scope, reqs...); err != nil {
		if err.IsStartError() {
			t.Fatal(err)
		} else {
			t.Skipf("Missing requirement: %v", err)
		}
	}
}

// NewNamedComponent implements the component.Factory interface
func (m *Manager) NewComponent(name string, desc component.Descriptor, scope lifecycle.Scope) (component.Instance, error) {
	// Require all of the children to be loaded first.
	if err := m.Require(scope, desc.Requires...); err != nil {
		return nil, err
	}
	// Now we can load the component itself.
	return m.requireComponent(name, desc, scope)
}

// NewNameComponentOrFail implements the component.Factory interface
func (m *Manager) NewComponentOrFail(name string, desc component.Descriptor, scope lifecycle.Scope, t testing.TB) component.Instance {
	t.Helper()
	c, err := m.NewComponent(name, desc, scope)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func normalizeScope(desc component.Descriptor, scope lifecycle.Scope) lifecycle.Scope {
	// Make sure that system components are always created with suite scope.
	if desc.IsSystemComponent && scope != lifecycle.System {
		scopes.Framework.Debugf("adjusting scope to '%s' for system component %s", lifecycle.System, desc.FriendlyName())
		return lifecycle.System
	}
	return scope
}

// TODO(sven): Take configuration here as well.
func (m *Manager) requireComponent(name string, desc component.Descriptor, scope lifecycle.Scope) (component.Instance, component.RequirementError) {
	// Make sure that system components are always created with suite scope.
	scope = normalizeScope(desc, scope)
	compId := namedIdFor(name, desc.ID)

	// First, check if we've already created this named component.
	if c, ok := m.compMap[compId]; ok {
		if !reflect.DeepEqual(c.comp.Descriptor(), desc) {
			return nil, resolutionError(fmt.Errorf("cannot add component `%s`, already running with `%s`", desc.FriendlyName(), c.comp.Descriptor().FriendlyName()))
		}
		if c.comp.Scope().IsLower(scope) {
			return nil, resolutionError(fmt.Errorf("component `%s` already exists with lower lifecycle scope: %s", desc.FriendlyName(), c.comp.Scope()))
		}
		// The component was already added.
		return c.comp, nil
	}

	// Verify all of the child dependencies were already created, as they should have been by callers
	// of this function. Otherwise report an error.
	for _, childReq := range desc.Requires {
		childEntry, err := parseRequirement(childReq)
		if err != nil {
			return nil, err
		}
		if _, ok := m.compMap[childEntry.id]; !ok {
			return nil, resolutionError(fmt.Errorf("missing child component %v while trying to create component %v", childEntry.id, compId))
		}
	}

	// Get the component factory function.
	// TODO(sven): Allow config-taking factory methods in addition to empty factor methods, or add a setConfig method to the resulting component, or add config to Start().
	fn, err := m.registry.GetFactory(desc)
	if err != nil {
		return nil, resolutionError(err)
	}

	// Create the component.
	c, err := fn()
	if err != nil {
		return nil, startError(err)
	}

	// Start the component.
	if err := c.Start(m.ctx, scope); err != nil {
		// Close the component if we can.
		if cl, ok := c.(io.Closer); ok {
			err = multierror.Append(err, cl.Close())
		}
		return nil, startError(err)
	}

	// Store the component entry in the manager.
	cEntry := compEntry{
		id:   compId,
		comp: c,
	}
	m.compMap[compId] = &cEntry
	m.all = append(m.all, &cEntry)

	return c, nil
}

// GetComponent implements the component.Repository interface
func (m *Manager) GetComponent(name string, id component.ID) component.Instance {
	if compEntry, ok := m.compMap[namedIdFor(name, id)]; ok {
		return compEntry.comp
	}
	return nil
}

// GetComponentOrFail implements the component.Repository interface
func (m *Manager) GetComponentOrFail(name string, id component.ID, t testing.TB) component.Instance {
	t.Helper()
	c := m.GetComponent(name, id)
	if c == nil {
		t.Fatalf("component %s has not been created", id)
	}
	return c
}

// GetComponentForDescriptor implements the component.Repository interface
func (m *Manager) GetComponentForDescriptor(name string, d component.Descriptor) component.Instance {
	return m.GetComponent(name, d.ID)
}

// GetComponentForDescriptorOrFail implements the component.Repository interface
func (m *Manager) GetComponentForDescriptorOrFail(name string, d component.Descriptor, t testing.TB) component.Instance {
	c := m.GetComponentForDescriptor(name, d)
	if c == nil {
		t.Fatalf("component does not exist: %v", d)
	}
	return c
}

// GetAllComponents implements the component.Repository interface
func (m *Manager) GetAllComponents() []component.Instance {
	all := make([]component.Instance, 0, len(m.all))
	for _, c := range m.all {
		all = append(all, c.comp)
	}
	return all
}

// Reset implements the api.Resettable interface
func (m *Manager) Reset() (err error) {

	// Close and remove any Test-scoped components.
	// Iterate in reverse, traversing from newest to oldest component.
	newAll := make([]*compEntry, 0, len(m.all))
	for i := len(m.all) - 1; i >= 0; i-- {
		c := m.all[i]
		if c.comp.Scope() == lifecycle.Test {
			err = multierror.Append(err, closeComponent(c.comp)).ErrorOrNil()
			delete(m.compMap, c.id)
			continue
		}
		newAll = append(newAll, c)
	}
	m.all = newAll

	// Now reset the remaining components.
	// Iterate in reverse, traversing from newest to oldest component.
	for i := len(m.all) - 1; i >= 0; i-- {
		c := m.all[i]
		err = multierror.Append(err, resetComponent(c.comp)).ErrorOrNil()
	}
	return
}

// Close implements io.Closer
func (m *Manager) Close() (err error) {
	// Iterate in reverse, traversing from newest to oldest component.
	for i := len(m.all) - 1; i >= 0; i-- {
		err = multierror.Append(err, closeComponent(m.all[i].comp)).ErrorOrNil()
	}
	// Clear the array.
	m.all = m.all[:0]

	// Clear the map.
	for k := range m.compMap {
		delete(m.compMap, k)
	}
	return err
}

func closeComponent(c api.Component) error {
	if cl, ok := c.(io.Closer); ok {
		scopes.Framework.Debugf("Cleaning up state for dependency: %s", c.Descriptor().ID)
		if err := cl.Close(); err != nil {
			scopes.Framework.Errorf("Error cleaning up dependency state: %s: %v", c.Descriptor().ID, err)
			return err
		}
	}
	return nil
}

func resetComponent(c api.Component) error {
	if cl, ok := c.(api.Resettable); ok {
		scopes.Framework.Debugf("Resetting state for component: %s", c.Descriptor().ID)
		if err := cl.Reset(); err != nil {
			scopes.Framework.Errorf("Error resetting component state: %s: %v", c.Descriptor().ID, err)
			return err
		}
	}
	return nil
}
