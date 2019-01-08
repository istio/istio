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

	multierror "github.com/hashicorp/go-multierror"

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

	// A map of all component instances
	compMap map[component.ID]api.Component

	// A list of all deployments in creation order.
	all []api.Component
}

// NewManager creates a new Manager instance.
func NewManager(ctx context.Instance, registry *registry.Instance) *Manager {
	return &Manager{
		Defaults: registry,
		ctx:      ctx,
		registry: registry,
		compMap:  make(map[component.ID]api.Component),
	}
}

// Require implements the component.Resolver interface
func (m *Manager) Require(scope lifecycle.Scope, reqs ...component.Requirement) component.RequirementError {
	// Gather the descriptors and component IDs.
	idMap := make(map[component.ID]bool)
	depMgr := newCreationProcessor(m, scope)
	for _, req := range reqs {
		if desc, ok := req.(*component.Descriptor); ok {
			if err := depMgr.Add(*desc); err != nil {
				return err
			}
		} else if id, ok := req.(*component.ID); ok {
			idMap[*id] = true
		} else {
			return resolutionError(fmt.Errorf("unsupported requirement type: %v", req))
		}
	}

	// Create any explicit dependencies first.
	if err := depMgr.CreateComponents(); err != nil {
		return err
	}

	// Now for any required component IDs, create default component as necessary.
	for compID := range idMap {
		if c := m.GetComponent(compID); c == nil {
			desc, err := m.GetDefaultDescriptor(compID)
			if err != nil {
				return resolutionError(err)
			}

			if _, err := m.requireComponent(desc, scope); err != nil {
				return err
			}
		}
	}

	return nil
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

// NewComponent implements the component.Factory interface
func (m *Manager) NewComponent(desc component.Descriptor, scope lifecycle.Scope) (component.Instance, error) {
	return m.requireComponent(desc, scope)
}

// NewComponentOrFail implements the component.Factory interface
func (m *Manager) NewComponentOrFail(desc component.Descriptor, scope lifecycle.Scope, t testing.TB) component.Instance {
	t.Helper()
	c, err := m.NewComponent(desc, scope)
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

func (m *Manager) requireComponent(desc component.Descriptor, scope lifecycle.Scope) (component.Instance, component.RequirementError) {
	// Make sure that system components are always created with suite scope.
	scope = normalizeScope(desc, scope)

	// First, check if we've already created this component.
	if c, ok := m.compMap[desc.ID]; ok {
		if !reflect.DeepEqual(c.Descriptor(), desc) {
			return nil, resolutionError(fmt.Errorf("cannot add component `%s`, already running with `%s`", desc.FriendlyName(), c.Descriptor().FriendlyName()))
		}
		if c.Scope().IsLower(scope) {
			return nil, resolutionError(fmt.Errorf("component `%s` already exists with lower lifecycle scope: %s", desc.FriendlyName(), c.Scope()))
		}
		// The component was already added.
		return c, nil
	}

	// Create any dependencies.
	if err := m.Require(scope, desc.Requires...); err != nil {
		return nil, err
	}

	// Now create the component.
	// Get the component factory function.
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

	// Store the deployment in the map.
	m.compMap[desc.ID] = c
	m.all = append(m.all, c)

	return c, nil
}

// GetComponent implements the component.Repository interface
func (m *Manager) GetComponent(id component.ID) component.Instance {
	return m.compMap[id]
}

// GetComponentOrFail implements the component.Repository interface
func (m *Manager) GetComponentOrFail(id component.ID, t testing.TB) component.Instance {
	t.Helper()
	c := m.GetComponent(id)
	if c == nil {
		t.Fatalf("component %s has not been created", id)
	}
	return c
}

// GetComponentForDescriptor implements the component.Repository interface
func (m *Manager) GetComponentForDescriptor(d component.Descriptor) component.Instance {
	return m.GetComponent(d.ID)
}

// GetComponentForDescriptorOrFail implements the component.Repository interface
func (m *Manager) GetComponentForDescriptorOrFail(d component.Descriptor, t testing.TB) component.Instance {
	c := m.GetComponentForDescriptor(d)
	if c == nil {
		t.Fatalf("component does not exist: %v", d)
	}
	return c
}

// GetAllComponents implements the component.Repository interface
func (m *Manager) GetAllComponents() []component.Instance {
	all := make([]component.Instance, 0, len(m.all))
	for _, c := range m.all {
		all = append(all, c)
	}
	return all
}

// Reset implements the api.Resettable interface
func (m *Manager) Reset() (err error) {

	// Close and remove any Test-scoped components.
	// Iterate in reverse, traversing from newest to oldest component.
	newAll := make([]api.Component, 0, len(m.all))
	for i := len(m.all) - 1; i >= 0; i-- {
		c := m.all[i]
		if c.Scope() == lifecycle.Test {
			err = multierror.Append(err, closeComponent(c)).ErrorOrNil()
			delete(m.compMap, c.Descriptor().ID)
			continue
		}
		newAll = append(newAll, c)
	}
	m.all = newAll

	// Now reset the remaining components.
	// Iterate in reverse, traversing from newest to oldest component.
	for i := len(m.all) - 1; i >= 0; i-- {
		c := m.all[i]
		err = multierror.Append(err, resetComponent(c)).ErrorOrNil()
	}
	return
}

// Close implements io.Closer
func (m *Manager) Close() (err error) {
	// Iterate in reverse, traversing from newest to oldest component.
	for i := len(m.all) - 1; i >= 0; i-- {
		err = multierror.Append(err, closeComponent(m.all[i])).ErrorOrNil()
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
