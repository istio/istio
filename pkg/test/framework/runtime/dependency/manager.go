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

var (
	_ component.Factory    = &Manager{}
	_ component.Repository = &Manager{}
	_ component.Resolver   = &Manager{}
	_ api.Resettable       = &Manager{}
	_ io.Closer            = &Manager{}
)

// Manager for the dependencies for all deployments.
type Manager struct {
	component.Defaults

	ctx context.Instance

	// The underlying Factory used for creating new component instances.
	registry *registry.Instance

	// A map of all components
	compMap map[component.Key]*compEntry

	// A list of all components in creation order.
	all []*compEntry
}

type compEntry struct {
	key  component.Key
	comp api.Component
}

func (c compEntry) String() string {
	return "{" + c.key.String() + ": " + c.comp.Descriptor().String() + "}"
}

// NewManager creates a new Manager instance.
func NewManager(ctx context.Instance, registry *registry.Instance) *Manager {
	return &Manager{
		Defaults: registry,
		ctx:      ctx,
		registry: registry,
		compMap:  make(map[component.Key]*compEntry),
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

// NewComponent implements the component.Factory interface
func (m *Manager) NewComponent(desc component.Descriptor, scope lifecycle.Scope) (component.Instance, error) {
	// Require all of the children to be loaded first.
	if err := m.Require(scope, desc.Requires...); err != nil {
		return nil, err
	}
	// Now we can load the component itself.
	reqEntry := createEntry(&desc)
	return m.requireComponent(reqEntry, scope)
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

func (m *Manager) requireComponent(entry *reqEntry, s lifecycle.Scope) (component.Instance, component.RequirementError) {
	// Make sure that system components are always created with suite scope.
	s = normalizeScope(*entry.desc, s)

	// First, check if we've already created this component.
	if cEntry, ok := m.compMap[entry.key]; ok {
		descName := entry.desc.FriendlyName()
		if !reflect.DeepEqual(cEntry.comp.Descriptor(), *entry.desc) {
			err := fmt.Errorf("cannot add component `%s`, already running with `%s`", descName, cEntry.comp.Descriptor().FriendlyName())
			return nil, resolutionError(err)
		}
		if cEntry.comp.Scope().IsLower(s) {
			err := fmt.Errorf("component `%s` already exists with lower lifecycle scope: %s", descName, cEntry.comp.Scope())
			return nil, resolutionError(err)
		}
		// The component was already added.
		return cEntry.comp, nil
	}

	// Verify all of the child dependencies were already created, as they should have been by callers
	// of this function. Otherwise report an error.
	for _, childReq := range entry.desc.Requires {
		childEntry := createEntry(childReq)
		if _, ok := m.compMap[childEntry.key]; !ok {
			err := fmt.Errorf("missing child component %v while trying to create component %v", childEntry, entry)
			return nil, resolutionError(err)
		}
	}

	// Get the component factory function.
	fn, err := m.registry.GetFactory(*entry.desc)
	if err != nil {
		return nil, resolutionError(err)
	}

	// Create the component.
	comp, err := fn()
	if err != nil {
		return nil, startError(err)
	}

	// Configure the component if configuration is non-nil and the component takes configuration.
	if entry.desc.Configuration != nil {
		if configurable, ok := comp.(api.Configurable); ok {
			if err = configurable.Configure(entry.desc.Configuration); err != nil {
				return nil, startError(err)
			}
		} else {
			err = fmt.Errorf("component %v does not accept configuration yet one was provided (%v)", entry.key, entry)
			return nil, startError(err)
		}
	}

	// Start the component.
	if err := comp.Start(m.ctx, s); err != nil {
		// Close the component if we can.
		if cl, ok := comp.(io.Closer); ok {
			err = multierror.Append(err, cl.Close())
		}
		return nil, startError(err)
	}

	// Store the component entry in the manager.
	cEntry := compEntry{
		key:  entry.key,
		comp: comp,
	}
	m.compMap[entry.key] = &cEntry
	if entry.key.Variant != "" {
		// If there is not yet a default variant stored, store one.
		baseKey := entry.key.ID.GetKey()
		if _, ok := m.compMap[baseKey]; !ok {
			m.compMap[baseKey] = &cEntry
		}
	}
	m.all = append(m.all, &cEntry)

	return comp, nil
}

// GetComponent implements the component.Repository interface
func (m *Manager) GetComponent(req component.Requirement) component.Instance {
	return m.getComponentByKey(req.GetKey())
}

// GetComponentOrFail implements the component.Repository interface
func (m *Manager) GetComponentOrFail(req component.Requirement, t testing.TB) component.Instance {
	t.Helper()
	c := m.GetComponent(req)
	if c == nil {
		t.Fatalf("component %s has not been created", req)
	}
	return c
}

func (m *Manager) getComponentByKey(key component.Key) component.Instance {
	if compEntry, ok := m.compMap[key]; ok {
		return compEntry.comp
	}
	return nil
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
			delete(m.compMap, c.key)
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
		scopes.Framework.Debugf("Cleaning up state for dependency: %s", c.Descriptor().GetKey())
		if err := cl.Close(); err != nil {
			scopes.Framework.Errorf("Error cleaning up dependency state: %s: %v", c.Descriptor().GetKey(), err)
			return err
		}
	}
	return nil
}

func resetComponent(c api.Component) error {
	if cl, ok := c.(api.Resettable); ok {
		scopes.Framework.Debugf("Resetting state for component: %s", c.Descriptor().GetKey())
		if err := cl.Reset(); err != nil {
			scopes.Framework.Errorf("Error resetting component state: %s: %v", c.Descriptor().GetKey(), err)
			return err
		}
	}
	return nil
}
