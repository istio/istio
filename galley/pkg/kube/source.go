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

package kube

import (
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/pkg/log"
)

// source is an implementation of runtime.Source.
type sourceImpl struct {
	ifaces Interfaces
	ch     chan resource.Event

	listeners map[resource.TypeURL]*listener
}

var _ runtime.Source = &sourceImpl{}

// NewSource returns a Kubernetes implementation of runtime.Source.
func NewSource(k Interfaces, resyncPeriod time.Duration) (runtime.Source, error) {
	return newSource(k, resyncPeriod, Types.All())
}

func newSource(k Interfaces, resyncPeriod time.Duration, specs []ResourceSpec) (runtime.Source, error) {
	s := &sourceImpl{
		ifaces:    k,
		listeners: make(map[resource.TypeURL]*listener),
	}

	for _, spec := range specs {
		l, err := newListener(k, resyncPeriod, spec, s.process)
		if err != nil {
			return nil, err
		}

		s.listeners[spec.Target.TypeURL] = l
	}

	return s, nil
}

// Start implements runtime.Source
func (s *sourceImpl) Start() (chan resource.Event, error) {
	s.ch = make(chan resource.Event, 1024)

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

func (s *sourceImpl) process(l *listener, kind resource.EventKind, key, version string, u *unstructured.Unstructured) {
	rid := resource.VersionedKey{
		Key: resource.Key{
			TypeURL:  l.spec.Target.TypeURL,
			FullName: key,
		},
		Version: resource.Version(version),
	}

	e := resource.Event{
		ID:   rid,
		Kind: kind,
	}
	if u != nil {
		item, err := l.spec.Converter(l.spec.Target, u)
		if err != nil {
			log.Errorf("Unable to convert unstructured to proto: %s/%s", key, version)
			return
		}
		e.Item = item
	}

	log.Debugf("Dispatching source event: %v", e)
	s.ch <- e
}
