// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package destination

import (
	"reflect"
	"sort"
	"sync"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/kube/krt"
)

// EDSFilter selects resolved destinations whose endpoint lifecycle is owned by
// the destination index. Sources still published by a legacy registry must not
// be selected or their endpoints would be duplicated across shards.
type EDSFilter func(ResolvedDestination) bool

type edsServiceKey struct {
	hostname  host.Name
	namespace string
}

// EDSPublisher projects resolved destination endpoints into Pilot's existing
// EndpointIndex through XDSUpdater. Multiple consumer bindings and ports for
// the same runtime destination are collapsed into a single endpoint shard.
type EDSPublisher struct {
	resolved     krt.Collection[ResolvedDestination]
	updater      model.XDSUpdater
	shard        model.ShardKey
	filter       EDSFilter
	registration krt.HandlerRegistration

	mu      sync.Mutex
	current map[edsServiceKey][]*model.IstioEndpoint
}

func NewEDSPublisher(
	resolved krt.Collection[ResolvedDestination],
	clusterID cluster.ID,
	updater model.XDSUpdater,
	filter EDSFilter,
) *EDSPublisher {
	p := &EDSPublisher{
		resolved: resolved,
		updater:  updater,
		shard:    model.ShardKey{Cluster: clusterID, Provider: provider.Destination},
		filter:   filter,
		current:  map[edsServiceKey][]*model.IstioEndpoint{},
	}
	p.registration = resolved.RegisterBatch(p.reconcile, true)
	return p
}

func (p *EDSPublisher) HasSynced() bool {
	return p == nil || p.registration.HasSynced()
}

func (p *EDSPublisher) reconcile(_ []krt.Event[ResolvedDestination]) {
	if p.updater == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	next := map[edsServiceKey][]*model.IstioEndpoint{}
	seen := map[edsServiceKey]map[string]struct{}{}
	for _, resolved := range p.resolved.List() {
		if p.filter != nil && !p.filter(resolved) {
			continue
		}
		key := edsServiceKey{hostname: resolved.Binding.RuntimeName, namespace: resolved.Binding.Namespace}
		if seen[key] == nil {
			seen[key] = map[string]struct{}{}
		}
		for _, endpoint := range resolved.Endpoints {
			endpointKey := endpoint.Key()
			if _, found := seen[key][endpointKey]; found {
				continue
			}
			seen[key][endpointKey] = struct{}{}
			next[key] = append(next[key], endpoint)
		}
	}
	for key := range next {
		sort.Slice(next[key], func(i, j int) bool {
			return next[key][i].Key() < next[key][j].Key()
		})
	}

	previous := p.current
	p.current = next

	for key, endpoints := range next {
		if reflect.DeepEqual(previous[key], endpoints) {
			continue
		}
		p.updater.EDSUpdate(p.shard, key.hostname.String(), key.namespace, endpoints)
	}
	for key := range previous {
		if _, found := next[key]; found {
			continue
		}
		p.updater.EDSUpdate(p.shard, key.hostname.String(), key.namespace, nil)
		p.updater.SvcUpdate(p.shard, key.hostname.String(), key.namespace, model.EventDelete)
	}
}
