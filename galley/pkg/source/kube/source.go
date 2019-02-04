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

package kube

import (
	"sync"
	"time"

	"istio.io/istio/galley/pkg/runtime"
	"istio.io/istio/galley/pkg/runtime/resource"
	"istio.io/istio/galley/pkg/source/kube/builtin"
	"istio.io/istio/galley/pkg/source/kube/client"
	"istio.io/istio/galley/pkg/source/kube/dynamic"
	kubeConverter "istio.io/istio/galley/pkg/source/kube/dynamic/converter"
	"istio.io/istio/galley/pkg/source/kube/log"
	"istio.io/istio/galley/pkg/source/kube/schema"

	kubeDynamic "k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

// New creates a new kube source for the given schema.
func New(interfaces client.Interfaces, resyncPeriod time.Duration, schema *schema.Instance,
	cfg *kubeConverter.Config) (runtime.Source, error) {

	var err error
	var cl kubernetes.Interface
	var dynClient kubeDynamic.Interface
	var sharedInformers informers.SharedInformerFactory

	log.Scope.Info("creating sources for kubernetes resources")
	sources := make([]runtime.Source, 0)
	for i, spec := range schema.All() {
		log.Scope.Infof("[%d]", i)
		log.Scope.Infof("  Source:      %s", spec.CanonicalResourceName())
		log.Scope.Infof("  Collection:  %s", spec.Target.Collection)

		// If it's a known type, use a custom (optimized) source.
		if builtin.IsBuiltIn(spec.Kind) {
			// Lazy create the kube client.
			if cl, err = getKubeClient(cl, interfaces); err != nil {
				return nil, err
			}
			sharedInformers = getSharedInformers(sharedInformers, cl, resyncPeriod)

			source, err := builtin.New(sharedInformers, spec)
			if err != nil {
				return nil, err
			}
			sources = append(sources, source)
		} else {
			// Lazy-create the dynamic client
			if dynClient, err = getDynamicClient(dynClient, interfaces); err != nil {
				return nil, err
			}
			// Unknown types use the dynamic source.
			source, err := dynamic.New(dynClient, resyncPeriod, spec, cfg)
			if err != nil {
				return nil, err
			}
			sources = append(sources, source)
		}
	}

	return &aggregate{
		sources: sources,
	}, nil
}

func getKubeClient(cl kubernetes.Interface, interfaces client.Interfaces) (kubernetes.Interface, error) {
	if cl != nil {
		return cl, nil
	}
	return interfaces.KubeClient()
}

func getSharedInformers(sharedInformers informers.SharedInformerFactory,
	cl kubernetes.Interface, resyncPeriod time.Duration) informers.SharedInformerFactory {
	if sharedInformers != nil {
		return sharedInformers
	}
	return informers.NewSharedInformerFactory(cl, resyncPeriod)
}

func getDynamicClient(cl kubeDynamic.Interface, interfaces client.Interfaces) (kubeDynamic.Interface, error) {
	if cl != nil {
		return cl, nil
	}
	return interfaces.DynamicInterface()
}

type aggregate struct {
	mu      sync.Mutex
	sources []runtime.Source
}

func (s *aggregate) Start(handler resource.EventHandler) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create an intermediate handler that captures sync events from each source.
	syncGroup := sync.WaitGroup{}
	syncGroup.Add(len(s.sources))
	syncHandler := func(e resource.Event) {
		if e.Kind == resource.FullSync {
			syncGroup.Done()
		} else {
			// Not a sync event, just pass on to the real handler.
			handler(e)
		}
	}

	for _, source := range s.sources {
		if err := source.Start(syncHandler); err != nil {
			return err
		}
	}

	// After all of the sources have synced, return a fullSync event.
	go func() {
		syncGroup.Wait()
		handler(resource.FullSyncEvent)
	}()

	return nil
}

func (s *aggregate) Stop() {
	for _, source := range s.sources {
		source.Stop()
	}
}
