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
	"encoding/json"
	"fmt"

	"istio.io/istio/pilot/pkg/config/file"
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
	name           resource.FullName
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
		log.Warnf("collection %s could not be found", col.String())
		return nil
	}
	cfg := i.store.Get(colschema.Resource().GroupVersionKind(), name.Name.String(), name.Namespace.String())
	if cfg == nil {
		log.Debugf(" %s resource [%s/%s] could not be found", colschema.Resource().GroupVersionKind(), name.Namespace.String(), name.Name.String())
		return nil
	}
	result, err := cfgToInstance(*cfg, col, colschema)
	if err != nil {
		log.Errorf("failed converting found config %s %s/%s to instance: %s, ",
			cfg.Meta.GroupVersionKind.Kind, cfg.Meta.Namespace, cfg.Meta.Namespace, err)
		return nil
	}
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
		k := key{
			col, resource.FullName{
				Name:      resource.LocalName(cfg.Name),
				Namespace: resource.Namespace(cfg.Namespace),
			},
		}
		if res, ok := i.found[k]; ok {
			if !broken && !fn(res) {
				broken = true
			}
			cache[res.Metadata.FullName] = res
			continue
		}
		res, err := cfgToInstance(cfg, col, colschema)
		if err != nil {
			// TODO: demote this log before merging
			log.Error(err)
			// TODO: is continuing the right thing here?
			continue
		}
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

func cfgToInstance(cfg config.Config, col collection.Name, colschema collection.Schema) (*resource.Instance, error) {
	res := resource.PilotConfigToInstance(&cfg, colschema.Resource())
	fmstring := cfg.Meta.Annotations[file.FieldMapKey]
	var out map[string]int
	if fmstring != "" {
		err := json.Unmarshal([]byte(fmstring), &out)
		if err != nil {
			return nil, fmt.Errorf("error parsing fieldmap: %s", err)
		}
	}
	refstring := cfg.Meta.Annotations[file.ReferenceKey]
	var outref resource.Reference
	if refstring != "" {
		outref = &kube.Position{}
		err := json.Unmarshal([]byte(refstring), outref)
		if err != nil {
			return nil, fmt.Errorf("error parsing reference: %s", err)
		}
	}
	res.Origin = &kube.Origin{
		Collection: col,
		Kind:       colschema.Resource().Kind(),
		FullName:   res.Metadata.FullName,
		Version:    resource.Version(cfg.ResourceVersion),
		Ref:        outref,
		FieldsMap:  out,
	}
	// MCP is not aware of generation, add that here.
	res.Metadata.Generation = cfg.Generation
	return res, nil
}
