// Copyright 2019 Istio Authors
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

package apiserver

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/schema"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver/stats"
	"istio.io/istio/galley/pkg/config/source/kube/apiserver/tombstone"
	"istio.io/istio/galley/pkg/config/source/kube/rt"
	"istio.io/istio/galley/pkg/util"
)

type watcher struct {
	adapter  *rt.Adapter
	resource schema.KubeResource

	worker *util.Worker

	handler event.Handler

	informer cache.SharedIndexInformer
}

func newWatcher(r schema.KubeResource, a *rt.Adapter) *watcher {
	return &watcher{
		resource: r,
		adapter:  a,
		worker:   util.NewWorker("watcher", scope),
	}
}

func (w *watcher) start() {
	_ = w.worker.Start(nil, func(ctx context.Context) {
		scope.Debugf("Starting watcher for %q (%q)", w.resource.Collection.Name, w.resource.CanonicalResourceName())

		var err error
		w.informer, err = w.adapter.NewInformer()
		if err != nil {
			// TODO
		}

		w.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) { w.handleEvent(event.Added, obj) },
			UpdateFunc: func(old, new interface{}) {
				if w.adapter.IsEqual(old, new) {
					// Periodic resync will send update events for all known resources.
					// Two different versions of the same resource will always have different RVs.
					return
				}

				w.handleEvent(event.Updated, new)
			},
			DeleteFunc: func(obj interface{}) { w.handleEvent(event.Deleted, obj) },
		})

		// Send the FullSync event after the cache syncs.
		go func() {
			_ = cache.WaitForCacheSync(ctx.Done(), w.informer.HasSynced)
			w.handler.Handle(event.FullSyncFor(w.resource.Collection.Name))
		}()

		// Start CRD shared informer and wait for it to exit.
		w.informer.Run(ctx.Done())
	})
}

func (w *watcher) stop() {
	w.worker.Stop()
}

func (w *watcher) dispatch(h event.Handler) {
	w.handler = event.CombineHandlers(w.handler, h)
}

func (w *watcher) handleEvent(c event.Kind, obj interface{}) {
	object, ok := obj.(metav1.Object)
	if !ok {
		if object = tombstone.RecoverResource(obj); object != nil {
			// Tombstone recovery failed.
			scope.Warnf("Unable to extract object for event: %v", obj)
			return
		}
		obj = object
	}

	object = w.adapter.ExtractObject(obj)
	res, err := w.adapter.ExtractResource(obj)
	if err != nil {
		scope.Warnf("unable to extract resource: %v: %e", obj, err)
		return
	}

	r := rt.ToResourceEntry(object, res)

	e := event.Event{
		Kind:   c,
		Source: w.resource.Collection.Name,
		Entry:  r,
	}

	if w.handler != nil {
		scope.Debugf("Sending event: [%v] from: %s", e, w.resource.Collection.Name)
		w.handler.Handle(e)
	}
	stats.RecordEventSuccess()
}
