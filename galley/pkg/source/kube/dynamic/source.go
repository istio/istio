// Copyright 2018 Istio Authors
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

package dynamic

import (
	"time"

	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/source/kube/client"
	"istio.io/istio/galley/pkg/source/kube/dynamic/converter"
	"istio.io/istio/galley/pkg/source/kube/log"
	"istio.io/istio/galley/pkg/source/kube/schema"
	"istio.io/istio/galley/pkg/source/kube/stats"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// source is an implementation of runtime.Source.
type sourceImpl struct {
	cfg    *converter.Config
	ifaces client.Interfaces
	ch     chan resource.Event

	listeners []*listener
}

var _ runtime.Source = &sourceImpl{}

// New returns a Kubernetes implementation of runtime.Source.
func New(k client.Interfaces, resyncPeriod time.Duration, schema *schema.Instance, cfg *converter.Config) (runtime.Source, error) {
	s := &sourceImpl{
		cfg:    cfg,
		ifaces: k,
		ch:     make(chan resource.Event, 1024),
	}

	log.Scope.Infof("Registering the following resources:")
	for i, spec := range schema.All() {
		log.Scope.Infof("[%d]", i)
		log.Scope.Infof("  Source:    %s", spec.CanonicalResourceName())
		log.Scope.Infof("  Type URL:  %s", spec.Target.Collection)

		l, err := newListener(k, resyncPeriod, spec, s.process)
		if err != nil {
			log.Scope.Errorf("Error registering listener: %v", err)
			return nil, err
		}

		s.listeners = append(s.listeners, l)
	}

	return s, nil
}

// Start implements runtime.Source
func (s *sourceImpl) Start() (chan resource.Event, error) {
	for _, l := range s.listeners {
		l.start()
	}

	// Wait in a background go-routine until all listeners are synced and send a full-sync event.
	go func() {
		for _, l := range s.listeners {
			l.waitForCacheSync()
		}
		s.ch <- resource.Event{Kind: resource.FullSync}
	}()

	return s.ch, nil
}

// Stop implements runtime.Source
func (s *sourceImpl) Stop() {
	for _, a := range s.listeners {
		a.stop()
	}
}

func (s *sourceImpl) process(l *listener, kind resource.EventKind, key resource.FullName, resourceVersion string, u *unstructured.Unstructured) {
	ProcessEvent(s.cfg, l.spec, kind, key, resourceVersion, u, s.ch)
}

// ProcessEvent process the incoming message and convert it to event
func ProcessEvent(cfg *converter.Config, spec schema.ResourceSpec, kind resource.EventKind, key resource.FullName, resourceVersion string,
	u *unstructured.Unstructured, ch chan resource.Event) {

	var event resource.Event

	entries, err := spec.Converter(cfg, spec.Target, key, spec.Kind, u)
	if err != nil {
		log.Scope.Errorf("Unable to convert unstructured to proto: %s/%s: %v", key, resourceVersion, err)
		stats.RecordConverterResult(false, spec.Version, spec.Group, spec.Kind)
		return
	}
	stats.RecordConverterResult(true, spec.Version, spec.Group, spec.Kind)

	if len(entries) == 0 {
		log.Scope.Debugf("Did not receive any entries from converter: kind=%v, key=%v, rv=%s", kind, key, resourceVersion)
		return
	}

	switch kind {
	case resource.Added, resource.Updated:
		event = resource.Event{
			Kind: kind,
		}

		rid := resource.VersionedKey{
			Key: resource.Key{
				Collection: spec.Target.Collection,
				FullName:   entries[0].Key,
			},
			Version: resource.Version(resourceVersion),
		}

		event.Entry = resource.Entry{
			ID:       rid,
			Item:     entries[0].Resource,
			Metadata: entries[0].Metadata,
		}

	case resource.Deleted:
		rid := resource.VersionedKey{
			Key: resource.Key{
				Collection: spec.Target.Collection,
				FullName:   entries[0].Key,
			},
			Version: resource.Version(resourceVersion),
		}

		event = resource.Event{
			Kind: kind,
			Entry: resource.Entry{
				ID: rid,
			},
		}
	}

	log.Scope.Debugf("Dispatching source event: %v", event)
	ch <- event
}
