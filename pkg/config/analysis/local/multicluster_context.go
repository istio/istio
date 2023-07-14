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
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/resource"
)

// NewMultiClusterContext allows tests to use istiodContext without exporting it.  returned context is not threadsafe.
func NewMultiClusterContext(stores map[cluster.ID]model.ConfigStore, cancelCh <-chan struct{}) analysis.Context {
	ctxs := make([]analysis.Context, 0)
	for c, store := range stores {
		ctxs = append(ctxs, NewContext(store, c, cancelCh, nil))
	}
	return &multiClusterContext{
		cancelCh: cancelCh,
		messages: diag.Messages{},
		ctxs:     ctxs,
	}
}

type multiClusterContext struct {
	ctxs     []analysis.Context
	cancelCh <-chan struct{}
	messages diag.Messages
}

func (i *multiClusterContext) Report(c config.GroupVersionKind, m diag.Message) {
	i.messages.Add(m)
}

func (i *multiClusterContext) Find(col config.GroupVersionKind, name resource.FullName) *resource.Instance {
	// not used in multicluster
	return nil
}

func (i *multiClusterContext) Exists(col config.GroupVersionKind, name resource.FullName) bool {
	existsInAll := true
	for _, ctx := range i.ctxs {
		existsInAll = existsInAll && ctx.Exists(col, name)
	}
	return existsInAll
}

func (i *multiClusterContext) ForEach(col config.GroupVersionKind, fn analysis.IteratorFn) {
	for _, ctx := range i.ctxs {
		ctx.ForEach(col, func(r *resource.Instance) bool {
			fn(r)
			return true
		})
	}
}

func (i *multiClusterContext) Canceled() bool {
	for _, ctx := range i.ctxs {
		if ctx.Canceled() {
			return true
		}
	}
	return false
}
