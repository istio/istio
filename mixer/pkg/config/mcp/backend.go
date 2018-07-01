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

package mcp

import (
	"context"
	"fmt"
	"net/url"
	"sync"

	"google.golang.org/grpc"

	"istio.io/istio/galley/pkg/kube/converter/legacy"

	mcp "istio.io/api/config/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/mcp/client"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
)

var scope = log.RegisterScope("mcp", "Mixer MCP client stack", 0)

const (
	mixerNodeID      = ""
	eventChannelSize = 4096
)

// Register registers this module as a StoreBackend.
// Do not use 'init()' for automatic registration; linker will drop
// the whole module because it looks unused.
func Register(builders map[string]store.Builder) {
	builders["mcp"] = func(u *url.URL) (store.Backend, error) { return newStore(u, nil) }
}

// NewStore creates a new Store instance.
func newStore(u *url.URL, fn updateHookFn) (store.Backend, error) {
	return &backend{
		addr:       u.Host,
		Probe:      probe.NewProbe(),
		updateHook: fn,
	}, nil
}

// updateHookFn is a testing hook function
type updateHookFn func()

// Store offers store.StoreBackend interface through kubernetes custom resource definitions.
type backend struct {
	mapping *mapping
	kinds   []string

	addr string

	*probe.Probe

	cancel context.CancelFunc

	state *state

	chLock sync.Mutex
	ch     chan store.BackendEvent

	updateHook updateHookFn
}

var _ store.Backend = &backend{}
var _ probe.SupportsProbe = &backend{}
var _ client.Updater = &backend{}

// state is the in-memory cache.
type state struct {
	sync.Mutex

	// items stored by kind, then by key.
	items map[string]map[store.Key]*store.BackEndResource
}

// Init implements store.Backend.Init.
func (b *backend) Init(kinds []string) error {
	// stash kinds in backend, to ensure that we can filter out any unsupported kinds.
	b.kinds = kinds

	b.mapping = constructMapping(kinds)

	messageNames := b.mapping.messageNames()

	ctx, cancel := context.WithCancel(context.Background())

	conn, err := grpc.DialContext(ctx, b.addr, grpc.WithInsecure())
	if err != nil {
		cancel()
		scope.Errorf("Error connecting to server: %v\n", err)
		return err
	}

	cl := mcp.NewAggregatedMeshConfigServiceClient(conn)

	c := client.New(cl, messageNames, b, mixerNodeID, map[string]string{})
	b.state = &state{
		items: make(map[string]map[store.Key]*store.BackEndResource),
	}

	go c.Run(ctx)

	b.cancel = cancel

	return nil
}

// Stop implements store.backend.Stop.
func (b *backend) Stop() {
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
}

// Watch creates a channel to receive the events.
func (b *backend) Watch() (<-chan store.BackendEvent, error) {
	b.chLock.Lock()
	defer b.chLock.Unlock()

	if b.ch != nil {
		return nil, fmt.Errorf("watch was already called")
	}

	b.ch = make(chan store.BackendEvent, eventChannelSize)

	return b.ch, nil
}

// Get returns a resource's spec to the key.
func (b *backend) Get(key store.Key) (*store.BackEndResource, error) {
	b.state.Lock()
	defer b.state.Unlock()

	perTypeState, found := b.state.items[key.Kind]
	if !found {
		return nil, store.ErrNotFound
	}

	item, found := perTypeState[key]
	if !found {
		return nil, store.ErrNotFound
	}

	return item, nil
}

// List returns the whole mapping from key to resource specs in the store.
func (b *backend) List() map[store.Key]*store.BackEndResource {
	b.state.Lock()
	defer b.state.Unlock()

	result := make(map[store.Key]*store.BackEndResource)
	for _, perTypeItems := range b.state.items {
		for k, v := range perTypeItems {
			result[k] = v
		}
	}

	return result
}

// Update implements client.Updater.Update.
func (b *backend) Update(change *client.Change) error {
	b.state.Lock()
	defer b.state.Unlock()
	defer b.callUpdateHook()

	newTypeStates := make(map[string]map[store.Key]*store.BackEndResource)
	// TODO: Make sure TypeURL v.s. MessageName works ok.
	typeUrl := fmt.Sprintf("type.googleapis.com/%s", change.MessageName)

	for _, o := range change.Objects {
		// Assume non-legacy first.
		kind := b.mapping.kind(typeUrl)
		pb := o.Resource
		if isLegacyTypeUrl(typeUrl) {
			// If it is legacy, extract the kind from payload.
			l := o.Resource.(*legacy.LegacyMixerResource)
			kind = l.Kind
			pb = l.Contents

			if !b.mapping.isSupportedLegacyKind(kind) {
				continue
			}
		}

		collection, found := newTypeStates[kind]
		if !found {
			collection = make(map[store.Key]*store.BackEndResource)
			newTypeStates[kind] = collection
		}

		key := toKey(kind, o.Metadata.Name)

		resource, err := toBackendResource(key, pb, o.Version)
		if err != nil {
			return err
		}
		collection[key] = resource
	}

	b.chLock.Lock()
	defer b.chLock.Unlock()

	for kind, newTypeState := range newTypeStates {
		oldTypeState, found := b.state.items[kind]

		b.state.items[kind] = newTypeState

		if b.ch == nil {
			continue
		}

		// If the watch channel available, then start pumping events
		if found {
			for k := range oldTypeState {
				if _, exists := newTypeState[k]; !exists {
					b.ch <- store.BackendEvent{Key: k, Type: store.Delete}
					continue
				}
			}
		}

		for k, v := range newTypeState {
			b.ch <- store.BackendEvent{Key: k, Type: store.Update, Value: v}
		}
	}

	return nil
}

func (b *backend) callUpdateHook() {
	if b.updateHook != nil {
		b.updateHook()
	}
}
