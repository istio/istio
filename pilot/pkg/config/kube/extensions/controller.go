// Copyright Istio Authors
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

// Package extensions provides a translation controller that converts WasmPlugin
// resources into synthetic ExtensionFilter configs, consolidating both resource
// types into a single internal code path.
package extensions

import (
	"fmt"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/util/sets"
)

var errUnsupportedOp = fmt.Errorf("unsupported operation: the extensions config store is a read-only view")

// Controller translates WasmPlugin resources into synthetic ExtensionFilter configs.
// It implements model.ConfigStoreController so it can be appended to s.ConfigStores
// alongside the underlying crdclient.
type Controller struct {
	outputs    Outputs
	schema     collection.Schemas
	handlers   []krt.HandlerRegistration
	xdsUpdater model.XDSUpdater
	stop       chan struct{}
}

// Inputs holds the KRT collections that the translation controller depends on.
// This is constructed internally from the input ConfigStoreController.
type Inputs struct {
	WasmPlugins krt.Collection[config.Config]
}

// Outputs holds the collections produced by the translation controller.
type Outputs struct {
	ExtensionFilters krt.Collection[config.Config]
}

// NewController constructs a Controller that reads WasmPlugin objects from
// inputStore and emits synthetic ExtensionFilter configs.
// The krtDebugger may be nil.
func NewController(inputStore model.ConfigStoreController, xdsUpdater model.XDSUpdater, krtDebugger *krt.DebugHandler) *Controller {
	stop := make(chan struct{})
	opts := krt.NewOptionsBuilder(stop, "extensions", krtDebugger)

	// Construct inputs from the config store (following gateway controller pattern)
	inputs := Inputs{
		WasmPlugins: inputStore.KrtCollection(gvk.WasmPlugin),
	}

	extensionFilters := krt.NewCollection(inputs.WasmPlugins,
		func(_ krt.HandlerContext, obj config.Config) *config.Config {
			return translateWasmPlugin(obj)
		},
		opts.WithName("extensions/WasmPlugin->ExtensionFilter")...,
	)

	c := &Controller{
		outputs: Outputs{
			ExtensionFilters: extensionFilters,
		},
		schema:     collection.SchemasFor(collections.ExtensionFilter),
		xdsUpdater: xdsUpdater,
		stop:       stop,
	}

	// Register a batch handler so changes to translated ExtensionFilters trigger XDS pushes.
	handler := extensionFilters.RegisterBatch(func(events []krt.Event[config.Config]) {
		if xdsUpdater == nil {
			return
		}
		cu := sets.New[model.ConfigKey]()
		for _, e := range events {
			for _, item := range e.Items() {
				cu.Insert(model.ConfigKey{
					Kind:      kind.ExtensionFilter,
					Name:      item.Name,
					Namespace: item.Namespace,
				})
			}
		}
		if len(cu) == 0 {
			return
		}
		xdsUpdater.ConfigUpdate(&model.PushRequest{
			Full:           true,
			ConfigsUpdated: cu,
			Reason:         model.NewReasonStats(model.ConfigUpdate),
		})
	}, false)
	c.handlers = append(c.handlers, handler)

	return c
}

// Schemas returns the set of resource types this controller emits.
func (c *Controller) Schemas() collection.Schemas {
	return c.schema
}

// Get is not supported for the translation controller.
func (c *Controller) Get(typ config.GroupVersionKind, name, namespace string) *config.Config {
	return nil
}

// List returns all synthetic ExtensionFilter configs. The namespace parameter is
// ignored; the aggregate layer handles namespace filtering.
func (c *Controller) List(typ config.GroupVersionKind, namespace string) []config.Config {
	if typ != gvk.ExtensionFilter {
		return nil
	}
	return c.outputs.ExtensionFilters.List()
}

// Create is not supported.
func (c *Controller) Create(cfg config.Config) (string, error) {
	return "", errUnsupportedOp
}

// Update is not supported.
func (c *Controller) Update(cfg config.Config) (string, error) {
	return "", errUnsupportedOp
}

// UpdateStatus is not supported.
func (c *Controller) UpdateStatus(cfg config.Config) (string, error) {
	return "", errUnsupportedOp
}

// Delete is not supported.
func (c *Controller) Delete(typ config.GroupVersionKind, name, namespace string, resourceVersion *string) error {
	return errUnsupportedOp
}

// RegisterEventHandler is not used; XDS updates are pushed directly via the batch handler.
func (c *Controller) RegisterEventHandler(typ config.GroupVersionKind, handler model.EventHandler) {}

// KrtCollection returns the krt collection for a given GVK. Only ExtensionFilter is supported.
func (c *Controller) KrtCollection(typ config.GroupVersionKind) krt.Collection[config.Config] {
	if typ == gvk.ExtensionFilter {
		return c.outputs.ExtensionFilters
	}
	return nil
}

// Run blocks until the stop channel is closed, then closes the internal stop channel.
func (c *Controller) Run(stop <-chan struct{}) {
	<-stop
	close(c.stop)
}

// HasSynced returns true once the underlying ExtensionFilter collection has synced.
func (c *Controller) HasSynced() bool {
	if !c.outputs.ExtensionFilters.HasSynced() {
		return false
	}
	for _, h := range c.handlers {
		if !h.HasSynced() {
			return false
		}
	}
	return true
}
