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
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/analysis/legacy/source/kube"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collections"
	sresource "istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/log"
)

// NewContext allows tests to use istiodContext without exporting it.  returned context is not threadsafe.
func NewContext(stores map[cluster.ID]model.ConfigStore, cancelCh <-chan struct{}, collectionReporter CollectionReporterFn) analysis.Context {
	return &istiodContext{
		stores:             stores,
		cancelCh:           cancelCh,
		messages:           map[string]*diag.Messages{},
		collectionReporter: collectionReporter,
		found:              map[key]*resource.Instance{},
		foundCollections:   map[config.GroupVersionKind]map[key]*resource.Instance{},
	}
}

type istiodContext struct {
	stores             map[cluster.ID]model.ConfigStore
	cancelCh           <-chan struct{}
	messages           map[string]*diag.Messages
	collectionReporter CollectionReporterFn
	found              map[key]*resource.Instance
	foundCollections   map[config.GroupVersionKind]map[key]*resource.Instance
	currentAnalyzer    string
}

type key struct {
	collectionName config.GroupVersionKind
	name           resource.FullName
	cluster        cluster.ID
}

func (i *istiodContext) Report(c config.GroupVersionKind, m diag.Message) {
	msgs := i.messages[i.currentAnalyzer]
	if msgs == nil {
		msgs = &diag.Messages{}
		i.messages[i.currentAnalyzer] = msgs
	}
	msgs.Add(m)
}

func (i *istiodContext) SetAnalyzer(analyzerName string) {
	i.currentAnalyzer = analyzerName
}

func (i *istiodContext) GetMessages(analyzerNames ...string) diag.Messages {
	result := diag.Messages{}
	if len(analyzerNames) == 0 {
		// no AnalyzerNames is equivalent to a wildcard, requesting all messages.
		for _, msgs := range i.messages {
			result.Add(*msgs...)
		}
	} else {
		for _, name := range analyzerNames {
			if msgs, ok := i.messages[name]; ok {
				result.Add(*msgs...)
			}
		}
	}
	return result
}

func (i *istiodContext) Find(col config.GroupVersionKind, name resource.FullName) *resource.Instance {
	i.collectionReporter(col)
	k := key{col, name, "default"}
	if result, ok := i.found[k]; ok {
		return result
	}
	if cache, ok := i.foundCollections[col]; ok {
		if result, ok2 := cache[k]; ok2 {
			return result
		}
	}
	colschema, ok := collections.All.FindByGroupVersionKind(col)
	if !ok {
		log.Warnf("collection %s could not be found", col.String())
		return nil
	}
	for id, store := range i.stores {
		cfg := store.Get(colschema.GroupVersionKind(), name.Name.String(), name.Namespace.String())
		if cfg == nil {
			continue
		}
		result, err := cfgToInstance(*cfg, col, colschema, id)
		if err != nil {
			log.Errorf("failed converting found config %s %s/%s to instance: %s, ",
				cfg.Meta.GroupVersionKind.Kind, cfg.Meta.Namespace, cfg.Meta.Namespace, err)
			return nil
		}
		i.found[k] = result
		return result
	}
	return nil
}

func (i *istiodContext) Exists(col config.GroupVersionKind, name resource.FullName) bool {
	i.collectionReporter(col)
	return i.Find(col, name) != nil
}

func (i *istiodContext) ForEach(col config.GroupVersionKind, fn analysis.IteratorFn) {
	if i.collectionReporter != nil {
		i.collectionReporter(col)
	}
	if cached, ok := i.foundCollections[col]; ok {
		for _, res := range cached {
			if !fn(res) {
				break
			}
		}
		return
	}
	colschema, ok := collections.All.FindByGroupVersionKind(col)
	if !ok {
		// TODO: demote this log before merging
		log.Errorf("collection %s could not be found", col.String())
		return
	}
	cache := map[key]*resource.Instance{}
	for id, store := range i.stores {
		// TODO: this needs to include file source as well
		cfgs := store.List(colschema.GroupVersionKind(), "")
		broken := false
		for _, cfg := range cfgs {
			k := key{
				col, resource.FullName{
					Name:      resource.LocalName(cfg.Name),
					Namespace: resource.Namespace(cfg.Namespace),
				}, id,
			}
			if res, ok := i.found[k]; ok {
				if !broken && !fn(res) {
					broken = true
				}
				cache[k] = res
				continue
			}
			res, err := cfgToInstance(cfg, col, colschema, id)
			if err != nil {
				// TODO: demote this log before merging
				log.Error(err)
				// TODO: is continuing the right thing here?
				continue
			}
			if !broken && !fn(res) {
				broken = true
			}
			cache[k] = res
		}
		if len(cache) > 0 {
			i.foundCollections[col] = cache
		}
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

func cfgToInstance(cfg config.Config, col config.GroupVersionKind, colschema sresource.Schema, cluster cluster.ID) (*resource.Instance,
	error,
) {
	res := resource.PilotConfigToInstance(&cfg, colschema)
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
		Type:            col,
		FullName:        res.Metadata.FullName,
		ResourceVersion: resource.Version(cfg.ResourceVersion),
		Ref:             outref,
		FieldsMap:       out,
		Cluster:         cluster,
	}
	// MCP is not aware of generation, add that here.
	res.Metadata.Generation = cfg.Generation
	return res, nil
}
