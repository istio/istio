//  Copyright Istio Authors
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
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	kubeSchema "k8s.io/apimachinery/pkg/runtime/schema"

	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/pkg/log"
	"istio.io/pkg/probe"

	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/pkg/config/schema"
	configz "istio.io/istio/pkg/mcp/configz/client"
	"istio.io/istio/pkg/mcp/creds"
	"istio.io/istio/pkg/mcp/monitoring"
	"istio.io/istio/pkg/mcp/sink"
)

var scope = log.RegisterScope("mcp", "Mixer MCP client stack", 0)

const (
	// TODO(nmittler): Need to decide the correct NodeID for mixer.
	mixerNodeID = "default"

	eventChannelSize      = 4096
	requiredCertCheckFreq = 500 * time.Millisecond
)

// Register registers this module as a StoreBackend.
// Do not use 'init()' for automatic registration; linker will drop
// the whole module because it looks unused.
func Register(builders map[string]store.Builder) {
	var builder store.Builder = func(u *url.URL, _ *kubeSchema.GroupVersion, credOptions *creds.Options, _ []string) (
		store.Backend, error) {
		return newStore(u, credOptions, nil)
	}

	builders["mcp"] = builder
	builders["mcps"] = builder
}

// newStore creates a new Store instance.
func newStore(u *url.URL, credOptions *creds.Options, fn updateHookFn) (store.Backend, error) {
	insecure := true
	if u.Scheme == "mcps" {
		insecure = false
		if credOptions == nil {
			return nil, errors.New("no credentials specified with secure MCP scheme")
		}
	}

	return &backend{
		serverAddress: u.Host,
		insecure:      insecure,
		credOptions:   credOptions,
		Probe:         probe.NewProbe(),
		updateHook:    fn,
	}, nil
}

// updateHookFn is a testing hook function
type updateHookFn func()

// backend is StoreBackend implementation using MCP.
type backend struct {
	// mapping of CRD <> collections.
	mapping *schema.Mapping

	// Use insecure communication for gRPC.
	insecure bool

	// address of the MCP server.
	serverAddress string

	// MCP credential options
	credOptions *creds.Options

	// The cancellation function that is used to cancel gRPC/MCP operations.
	cancel context.CancelFunc

	mcpReporter monitoring.Reporter

	// The in-memory state, where resources are kept for out-of-band get and list calls.
	state *state

	// channel for publishing backend events. chLock is used to protect against ch lifecycle
	// events (e.g. creation of the channel).
	// It also protects against a race, where a channel could be created while there is incoming
	// data/changes.
	chLock sync.Mutex
	ch     chan store.BackendEvent

	// The update hook that was registered. This is for testing.
	updateHook updateHookFn

	*probe.Probe
}

var _ store.Backend = &backend{}
var _ probe.SupportsProbe = &backend{}
var _ sink.Updater = &backend{}

// state is the in-memory cache.
type state struct {
	sync.RWMutex

	// items stored by kind, then by key.
	items  map[string]map[store.Key]*store.BackEndResource
	synced map[string]bool // by collection
}

// Init implements store.Backend.Init.
func (b *backend) Init(kinds []string) error {
	m, err := schema.ConstructKindMapping(kinds, schema.MustGet())
	if err != nil {
		return err
	}
	b.mapping = m

	collections := b.mapping.Collections()

	scope.Infof("Requesting following collections:")
	for i, name := range collections {
		scope.Infof("  [%d] %s", i, name)
	}

	// nolint: govet
	ctx, cancel := context.WithCancel(context.Background())

	securityOption := grpc.WithInsecure()
	if !b.insecure {
		address := b.serverAddress
		if strings.Contains(address, ":") {
			address = strings.Split(address, ":")[0]
		}

		requiredFiles := []string{b.credOptions.CertificateFile, b.credOptions.KeyFile, b.credOptions.CACertificateFile}
		log.Infof("Secure MCP configured. Waiting for required certificate files to become available: %v", requiredFiles)
		for len(requiredFiles) > 0 {
			if _, err = os.Stat(requiredFiles[0]); os.IsNotExist(err) {
				log.Infof("%v not found. Checking again in %v", requiredFiles[0], requiredCertCheckFreq)
				select {
				case <-ctx.Done():
					// nolint: govet
					cancel()
					return ctx.Err()
				case <-time.After(requiredCertCheckFreq):
					// retry
				}
				continue
			}

			log.Infof("%v found", requiredFiles[0])
			requiredFiles = requiredFiles[1:]
		}

		watcher, er := creds.WatchFiles(ctx.Done(), b.credOptions)
		if er != nil {
			cancel()
			return er
		}
		credentials := creds.CreateForClient(address, watcher)
		securityOption = grpc.WithTransportCredentials(credentials)
	}

	conn, err := grpc.DialContext(ctx, b.serverAddress, securityOption)
	if err != nil {
		cancel()
		scope.Errorf("Error connecting to server: %v\n", err)
		return err
	}

	b.mcpReporter = monitoring.NewStatsContext("mixer")
	options := &sink.Options{
		CollectionOptions: sink.CollectionOptionsFromSlice(collections),
		Updater:           b,
		ID:                mixerNodeID,
		Reporter:          b.mcpReporter,
	}

	cl := mcp.NewResourceSourceClient(conn)
	c := sink.NewClient(cl, options)
	configz.Register(c)
	go c.Run(ctx)

	b.state = &state{
		items:  make(map[string]map[store.Key]*store.BackEndResource),
		synced: make(map[string]bool),
	}
	for _, collection := range collections {
		b.state.synced[collection] = false
	}

	b.cancel = cancel
	return nil
}

// WaitForSynced implements store.Backend interface.
func (b *backend) WaitForSynced(timeout time.Duration) error {
	stop := time.After(timeout)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-stop:
			return fmt.Errorf("exceeded timeout %v", timeout)
		case <-tick.C:
			ready := true
			b.state.RLock()
			for _, synced := range b.state.synced {
				if !synced {
					ready = false
					break
				}
			}
			b.state.RUnlock()

			if ready {
				return nil
			}
		}
	}
}

// Stop implements store.Backend.Stop.
func (b *backend) Stop() {
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
	_ = b.mcpReporter.Close()
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
	b.state.RLock()
	defer b.state.RUnlock()

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
	b.state.RLock()
	defer b.state.RUnlock()

	result := make(map[store.Key]*store.BackEndResource)
	for _, perTypeItems := range b.state.items {
		for k, v := range perTypeItems {
			result[k] = v
		}
	}

	return result
}

// Apply implements client.Updater.Apply
func (b *backend) Apply(change *sink.Change) error {
	b.state.Lock()
	defer b.state.Unlock()
	defer b.callUpdateHook()

	newTypeStates := make(map[string]map[store.Key]*store.BackEndResource)

	b.state.synced[change.Collection] = true

	scope.Debugf("Received update for: collection:%s, count:%d", change.Collection, len(change.Objects))

	for _, o := range change.Objects {
		if scope.DebugEnabled() {
			scope.Debugf("Processing incoming resource: %q @%s [%s]",
				o.Metadata.Name, o.Metadata.Version, o.TypeURL)
		}

		name := o.Metadata.Name
		kind := b.mapping.Kind(change.Collection)
		contents := o.Body
		labels := o.Metadata.Labels
		annotations := o.Metadata.Annotations

		collection, found := newTypeStates[kind]
		if !found {
			collection = make(map[store.Key]*store.BackEndResource)
			newTypeStates[kind] = collection
		}

		// Map it to Mixer's store model, and put it in the new collection.

		key := toKey(kind, name)
		resource, err := toBackendResource(key, labels, annotations, contents, o.Metadata.Version)
		if err != nil {
			return err
		}
		collection[key] = resource
	}

	// Lock the channel state, as we will start publishing events soon.
	b.chLock.Lock()
	defer b.chLock.Unlock()

	// Now, diff against the in-memory state and generate store events.
	for kind, newTypeState := range newTypeStates {
		oldTypeState, found := b.state.items[kind]

		// Replace the old collection with the new one.
		// We can do this, because there is no error that can be raised from now on.
		b.state.items[kind] = newTypeState

		// If the downstream users haven't started listening yet, we don't need to
		// send any events.
		if b.ch == nil {
			continue
		}

		// Otherwise, start pumping events by diffing old and new states.
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
