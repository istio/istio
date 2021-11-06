/*
 Copyright Istio Authors

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package local

import (
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/legacy/source/kube"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/pkg/log"
)

// NewContext allows tests to use istiodContext without exporting it.  returned context is not threadsafe.
func NewContext(store model.ConfigStore, cancelCh <-chan struct{}, collectionReporter CollectionReporterFn) analysis.Context {
	return &istiodContext{
		store:              store,
		cancelCh:           cancelCh,
		messages:           diag.Messages{},
		collectionReporter: collectionReporter,
		found:              map[key]*resource.Instance{},
		foundCollections:   map[collection.Name]map[resource.FullName]*resource.Instance{},
	}
}

type istiodContext struct {
	store              model.ConfigStore
	cancelCh           <-chan struct{}
	messages           diag.Messages
	collectionReporter CollectionReporterFn
	found              map[key]*resource.Instance
	foundCollections   map[collection.Name]map[resource.FullName]*resource.Instance
}

type key struct {
	collectionName collection.Name
	name resource.FullName
}

func (i *istiodContext) Report(c collection.Name, m diag.Message) {
	i.messages.Add(m)
}

func (i *istiodContext) Find(col collection.Name, name resource.FullName) *resource.Instance {
	i.collectionReporter(col)
	if result, ok := i.found[key{col, name}]; ok {
		return result
	}
	if cache, ok := i.foundCollections[col]; ok {
		if result, ok2 := cache[name]; ok2 {
			return result
		}
	}
	colschema, ok := collections.All.Find(col.String())
	if !ok {
		// TODO: demote this log before merging
		log.Errorf("collection %s could not be found", col.String())
		return nil
	}
	cfg := i.store.Get(colschema.Resource().GroupVersionKind(), name.Name.String(), name.Namespace.String())
	if cfg == nil {
		// TODO: demote this log before merging
		log.Errorf("collection %s does not have a member named", col.String(), name)
		return nil
	}
	mcpr, err := config.PilotConfigToResource(cfg)
	if err != nil {
		// TODO: demote this log before merging
		log.Errorf("failed converting cfg %s to mcp resource: %s", cfg.Name, err)
		return nil
	}
	result, err := resource.Deserialize(mcpr, colschema.Resource())
	if err != nil {
		// TODO: demote this log before merging
		log.Errorf("failed deserializing mcp resource %s to instance: %s", cfg.Name, err)
		return nil
	}
	result.Origin = &kube.Origin{
		Collection: col,
		Kind:       colschema.Resource().Kind(),
		FullName:   result.Metadata.FullName,
		Version:    resource.Version(cfg.ResourceVersion),
		Ref:        nil,
		FieldsMap:  nil,
	}
	// MCP is not aware of generation, add that here.
	result.Metadata.Generation = cfg.Generation
	i.found[key{col, name}] = result
	return result
}

func (i *istiodContext) Exists(col collection.Name, name resource.FullName) bool {
	i.collectionReporter(col)
	return i.Find(col, name) != nil
}

func (i *istiodContext) ForEach(col collection.Name, fn analysis.IteratorFn) {
	i.collectionReporter(col)
	if cached, ok := i.foundCollections[col]; ok {
		for _, res := range cached {
			if !fn(res) {
				break
			}
		}
		return
	}
	colschema, ok := collections.All.Find(col.String())
	if !ok {
		// TODO: demote this log before merging
		log.Errorf("collection %s could not be found", col.String())
		return
	}
	// TODO: this needs to include file source as well
	cfgs, err := i.store.List(colschema.Resource().GroupVersionKind(), "")
	if err != nil {
		// TODO: demote this log before merging
		log.Errorf("collection %s could not be listed: %s", col.String(), err)
		return
	}
	broken := false
	cache := map[resource.FullName]*resource.Instance{}
	for _, cfg := range cfgs {
		k := key{col, resource.FullName{
			Name: resource.LocalName(cfg.Name),
			Namespace: resource.Namespace(cfg.Namespace)},
		}
		if res, ok := i.found[k]; ok {
			if !broken && !fn(res) {
				broken = true
			}
			cache[res.Metadata.FullName] = res
			continue
		}
		mcpr, err := config.PilotConfigToResource(&cfg)
		if err != nil {
			// TODO: demote this log before merging
			log.Errorf("failed converting cfg %s to mcp resource: %s", cfg.Name, err)
			// TODO: is continuing the right thing here?
			continue
		}
		res, err := resource.Deserialize(mcpr, colschema.Resource())
		// TODO: why does this leave origin empty?
		if err != nil {
			// TODO: demote this log before merging
			log.Errorf("failed deserializing mcp resource %s to instance: %s", cfg.Name, err)
			// TODO: is continuing the right thing here?
			continue
		}
		res.Origin = &kube.Origin{
			Collection: col,
			Kind:       colschema.Resource().Kind(),
			FullName:   res.Metadata.FullName,
			Version:    resource.Version(cfg.ResourceVersion),
			Ref:        nil,
			FieldsMap:  nil,
		}
		// MCP is not aware of generation, add that here.
		res.Metadata.Generation = cfg.Generation
		if !broken && !fn(res) {
			broken = true
		}
		cache[res.Metadata.FullName] = res
	}
	if len(cache) > 0 {
		i.foundCollections[col] = cache
	}
}

func (i *istiodContext) Canceled() bool {
	select {
	case <-i.cancelCh:
		return true
	default:
		return false
	}
}
