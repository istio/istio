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

package builtin

import (
	"fmt"
	"reflect"
	"sync"

	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/source/kube/log"
	"istio.io/istio/galley/pkg/source/kube/schema"
	"istio.io/istio/galley/pkg/source/kube/stats"
	"istio.io/istio/galley/pkg/source/kube/tombstone"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

var _ runtime.Source = &source{}

// New creates a new built-in source. If the type is not built-in, returns an error.
func New(sharedInformers informers.SharedInformerFactory, spec schema.ResourceSpec) (runtime.Source, error) {
	t := types[spec.Kind]
	if t == nil {
		return nil, fmt.Errorf("unknown resource type: name='%s', gv='%v'",
			spec.Singular, spec.GroupVersion())
	}
	return newSource(sharedInformers, t), nil
}

func newSource(sharedInformers informers.SharedInformerFactory, t *Type) runtime.Source {
	return &source{
		t:               t,
		sharedInformers: sharedInformers,
	}
}

type source struct {
	// Lock for changing the running state of the source
	stateLock sync.Mutex

	// The built-in type that is processed by this source.
	t *Type

	sharedInformers informers.SharedInformerFactory
	informer        cache.SharedIndexInformer

	// stopCh is used to quiesce the background activity during shutdown
	stopCh  chan struct{}
	handler resource.EventHandler
}

// Start the source. This will commence listening and dispatching of events.
func (s *source) Start(handler resource.EventHandler) error {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	if s.stopCh != nil {
		return fmt.Errorf("already synchronizing resources: name='%s', gv='%v'",
			s.t.GetSpec().Singular, s.t.GetSpec().GroupVersion())
	}
	if handler == nil {
		return fmt.Errorf("invalid handler")
	}

	log.Scope.Debugf("Starting source for %s(%v)", s.t.GetSpec().Singular, s.t.GetSpec().GroupVersion())

	s.stopCh = make(chan struct{})
	s.handler = handler

	s.informer = s.t.NewInformer(s.sharedInformers)
	s.informer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				s.handleEvent(resource.Added, obj)
			},
			UpdateFunc: func(old, new interface{}) {
				if s.t.IsEqual(old, new) {
					// Periodic resync will send update events for all known resources.
					// Two different versions of the same resource will always have different RVs.
					return
				}
				s.handleEvent(resource.Updated, new)
			},
			DeleteFunc: func(obj interface{}) {
				s.handleEvent(resource.Deleted, obj)
			},
		})

	// Start CRD shared informer background process.
	go s.informer.Run(s.stopCh)

	// Send the an event after the cache syncs.
	go func() {
		_ = cache.WaitForCacheSync(s.stopCh, s.informer.HasSynced)
		handler(resource.FullSyncEvent)
	}()

	return nil
}

// Stop the source. This will stop publishing of events.
func (s *source) Stop() {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()

	if s.stopCh == nil {
		log.Scope.Warn("built-in kubernetes source already stopped")
		return
	}

	close(s.stopCh)
	s.stopCh = nil
}

func (s *source) handleEvent(kind resource.EventKind, obj interface{}) {
	object := s.t.ExtractObject(obj)
	if object == nil {
		if object = tombstone.RecoverResource(obj); object != nil {
			// Tombstone recovery failed.
			return
		}
	}

	fullName := resource.FullNameFromNamespaceAndName(object.GetNamespace(), object.GetName())

	log.Scope.Debugf("Sending event: [%v] from: %s", kind, s.t.GetSpec().CanonicalResourceName())

	event := resource.Event{
		Kind: kind,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Key: resource.Key{
					Collection: s.t.GetSpec().Target.Collection,
					FullName:   fullName,
				},
				Version: resource.Version(object.GetResourceVersion()),
			},
			Metadata: resource.Metadata{
				CreateTime:  object.GetCreationTimestamp().Time,
				Labels:      object.GetLabels(),
				Annotations: object.GetAnnotations(),
			},
		},
	}

	switch kind {
	case resource.Added, resource.Updated:
		// Convert the object to a protobuf message.
		item := s.t.ExtractResource(obj)
		if item == nil {
			msg := fmt.Sprintf("failed casting object to proto: %v", reflect.TypeOf(obj))
			log.Scope.Error(msg)
			stats.RecordEventError(msg)
			return
		}

		event.Entry.Item = item
	}

	log.Scope.Debugf("Dispatching source event: %v", event)
	s.handler(event)
	stats.RecordEventSuccess()
}
