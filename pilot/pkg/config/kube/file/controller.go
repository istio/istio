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

package file

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	krtfiles "istio.io/istio/pkg/kube/krt/files"
)

var errUnsupportedOp = fmt.Errorf("unsupported operation: the file config store is a read-only view")

type Controller struct {
	collections map[config.GroupVersionKind]krt.Collection[config.Config]
	indexes     map[config.GroupVersionKind]krt.Index[string, config.Config]
	schemas     collection.Schemas
	handlers    map[config.GroupVersionKind][]krt.HandlerRegistration
	stop        chan struct{}
}

func NewController(
	fileDir string,
	domainSuffix string,
	schemas collection.Schemas,
	options kubecontroller.Options,
) (*Controller, error) {
	stop := make(chan struct{})
	opts := krt.NewOptionsBuilder(stop, "file-monitor", options.KrtDebugger)
	watch, err := krtfiles.NewFolderWatch(fileDir, func(b []byte) ([]*config.Config, error) {
		return parseInputs(b, domainSuffix)
	}, stop)
	if err != nil {
		return nil, err
	}

	collectionMap := make(map[config.GroupVersionKind]krt.Collection[config.Config])
	indexes := make(map[config.GroupVersionKind]krt.Index[string, config.Config])
	for _, s := range schemas.All() {
		if _, ok := collections.Pilot.FindByGroupVersionKind(s.GroupVersionKind()); ok {
			collection := krtfiles.NewFileCollection(watch, func(c *config.Config) *config.Config {
				if c.GroupVersionKind == s.GroupVersionKind() {
					return c
				}

				return nil
			}, opts.WithName("FileMonitor")...)

			collectionMap[s.GroupVersionKind()] = collection

			if s.GroupVersionKind() == gvk.ServiceEntry || s.GroupVersionKind() == gvk.WorkloadEntry {
				indexes[s.GroupVersionKind()] = krt.NewNamespaceIndex(collection)
			}
		}
	}

	return &Controller{
		collections: collectionMap,
		indexes:     indexes,
		schemas:     schemas,
		stop:        stop,
		handlers:    make(map[config.GroupVersionKind][]krt.HandlerRegistration),
	}, nil
}

func (c *Controller) Schemas() collection.Schemas {
	return c.schemas
}

func (c *Controller) Get(typ config.GroupVersionKind, name, namespace string) *config.Config {
	if col, ok := c.collections[typ]; ok {
		if namespace == "" {
			return col.GetKey(name)
		}

		return col.GetKey(namespace + "/" + name)
	}

	return nil
}

func (c *Controller) List(typ config.GroupVersionKind, namespace string) []config.Config {
	if namespace == metav1.NamespaceAll {
		if col, ok := c.collections[typ]; ok {
			return col.List()
		}
	} else {
		if idx, ok := c.indexes[typ]; ok {
			return idx.Lookup(namespace)
		}
	}

	return nil
}

func (c *Controller) Create(config config.Config) (revision string, err error) {
	return "", errUnsupportedOp
}

func (c *Controller) Update(config config.Config) (newRevision string, err error) {
	return "", errUnsupportedOp
}

func (c *Controller) UpdateStatus(config config.Config) (newRevision string, err error) {
	return "", errUnsupportedOp
}

func (c *Controller) Patch(orig config.Config, patchFn config.PatchFunc) (string, error) {
	return "", errUnsupportedOp
}

func (c *Controller) Delete(typ config.GroupVersionKind, name, namespace string, _ *string) error {
	return errUnsupportedOp
}

func (c *Controller) RegisterEventHandler(typ config.GroupVersionKind, handler model.EventHandler) {
	if col, ok := c.collections[typ]; ok {
		c.handlers[typ] = append(
			c.handlers[typ],
			col.RegisterBatch(func(evs []krt.Event[config.Config]) {
				for _, event := range evs {
					switch event.Event {
					case controllers.EventAdd:
						handler(config.Config{}, *event.New, model.EventAdd)
					case controllers.EventUpdate:
						handler(*event.Old, *event.New, model.EventUpdate)
					case controllers.EventDelete:
						handler(config.Config{}, *event.Old, model.EventDelete)
					}
				}
			}, false),
		)
	}
}

func (c *Controller) Run(stop <-chan struct{}) {
	<-stop
	close(c.stop)
}

func (c *Controller) HasSynced() bool {
	for _, col := range c.collections {
		if !col.HasSynced() {
			return false
		}
	}
	for _, handlers := range c.handlers {
		for _, handler := range handlers {
			if !handler.HasSynced() {
				return false
			}
		}
	}
	return true
}

// parseInputs is identical to crd.ParseInputs, except that it returns an array of config pointers.
func parseInputs(data []byte, domainSuffix string) ([]*config.Config, error) {
	configs, _, err := crd.ParseInputs(string(data))

	// Convert to an array of pointers.
	refs := make([]*config.Config, len(configs))
	for i := range configs {
		refs[i] = &configs[i]
		refs[i].Domain = domainSuffix
	}
	return refs, err
}
