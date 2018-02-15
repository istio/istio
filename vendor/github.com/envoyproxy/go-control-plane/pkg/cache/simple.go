// Copyright 2017 Envoyproxy Authors
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

package cache

import (
	"sync"

	"github.com/envoyproxy/go-control-plane/api"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/glog"
)

// SimpleCache is a snapshot-based cache that maintains a single versioned
// snapshot of responses per node group, with no canary updates.  SimpleCache
// does not track status of remote nodes and consistently replies with the
// latest snapshot. For the protocol to work correctly, EDS/RDS requests are
// responded only when all resources in the snapshot xDS response are named as
// part of the request. It is expected that the CDS response names all EDS
// clusters, and the LDS response names all RDS routes in a snapshot, to ensure
// that Envoy makes the request for all EDS clusters or RDS routes eventually.
//
// SimpleCache can also be used as a config cache for distinct xDS requests.
// The snapshots are required to contain only the responses for the particular
// type of the xDS service that the cache serves. Synchronization of multiple
// caches for different response types is left to the configuration producer.
type SimpleCache struct {
	// snapshots are cached resources
	snapshots map[Key]Snapshot

	// watches keeps track of open watches
	watches map[Key]map[int64]Watch

	// callback requests missing responses
	callback func(Key)

	// watchCount is the ID generator for watches
	watchCount int64

	// groups is the hashing function for proxy nodes
	groups NodeGroup

	mu sync.Mutex
}

// Snapshot is an internally consistent snapshot of xDS resources.
// Snapshots should have distinct versions per node group.
type Snapshot struct {
	version   string
	resources map[ResponseType][]proto.Message
}

// NewSnapshot creates a snapshot from response types and a version.
func NewSnapshot(version string,
	endpoints []proto.Message,
	clusters []proto.Message,
	routes []proto.Message,
	listeners []proto.Message) Snapshot {
	return Snapshot{
		version: version,
		resources: map[ResponseType][]proto.Message{
			EndpointResponse: endpoints,
			ClusterResponse:  clusters,
			RouteResponse:    routes,
			ListenerResponse: listeners,
		},
	}
}

// NewSimpleCache initializes a simple cache.
// callback function is called on every new cache key and response type if there is no response available.
// callback is executed in a go-routine and can be called multiple times prior to receiving a snapshot.
func NewSimpleCache(groups NodeGroup, callback func(Key)) Cache {
	return &SimpleCache{
		snapshots:  make(map[Key]Snapshot),
		watches:    make(map[Key]map[int64]Watch),
		callback:   callback,
		watchCount: 0,
		groups:     groups,
	}
}

// SetSnapshot updates the simple cache with a snapshot for a node group.
func (cache *SimpleCache) SetSnapshot(group Key, snapshot Snapshot) error {
	// TODO(kuat) validate snapshot for types and internal consistency

	cache.mu.Lock()
	defer cache.mu.Unlock()

	// update the existing entry
	cache.snapshots[group] = snapshot

	// trigger existing watches
	if watches, ok := cache.watches[group]; ok {
		for _, watch := range watches {
			respond(watch, snapshot, group)
		}
		// discard all watches; the client must request a new watch to receive updates and ACK/NACK
		delete(cache.watches, group)
	}

	return nil
}

// Respond to a watch with the snapshot value. The value channel should have capacity not to block.
// TODO(kuat) this responds immediately and can cause the remote node to spin if it consistently fails to ACK the update
func respond(watch Watch, snapshot Snapshot, group Key) {
	typ := watch.Type
	resources := snapshot.resources[typ]
	version := snapshot.version

	// remove clean-up as the watch is discarded immediately
	watch.stop = nil

	// the request names must match the snapshot names
	// if they do not, then the watch is never responded, and it is expected that envoy makes another request
	if len(watch.Names) != 0 {
		// convert to a set
		names := make(map[string]bool)
		for _, name := range watch.Names {
			names[name] = true
		}

		// check that every snapshot resource name is present in the request
		for _, resource := range resources {
			resourceName := GetResourceName(resource)
			if _, exists := names[resourceName]; !exists {
				glog.V(10).Infof("not responding for %s from %q at %q since %q not requested %v",
					typ.String(), group, version, resourceName, watch.Names)
				return
			}
		}
	}

	watch.Value <- Response{
		Version:   version,
		Resources: resources,
	}
}

// Watch returns a watch for an xDS request.
func (cache *SimpleCache) Watch(typ ResponseType, node *api.Node, version string, names []string) Watch {
	out := Watch{
		Type:  typ,
		Names: names,
	}
	group, err := cache.groups.Hash(node)

	// do nothing case
	if err != nil {
		return out
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()

	// allocate capacity 1 to allow one-time non-blocking use
	out.Value = make(chan Response, 1)

	// if the requested version is up-to-date or missing a response, leave an open watch
	snapshot, exists := cache.snapshots[group]
	if !exists || version == snapshot.version {
		// invoke callback in a go-routine
		if !exists && cache.callback != nil {
			glog.V(10).Infof("callback %q at %q", group, version)
			go cache.callback(group)
		}

		if _, ok := cache.watches[group]; !ok {
			cache.watches[group] = make(map[int64]Watch)
		}

		glog.V(10).Infof("open watch for %s%v from key %q from version %q", typ.String(), names, group, version)
		cache.watchCount++
		id := cache.watchCount
		cache.watches[group][id] = out
		out.stop = func() {
			cache.mu.Lock()
			defer cache.mu.Unlock()
			if _, ok := cache.watches[group]; ok {
				delete(cache.watches[group], id)
			}
		}
		return out
	}

	// otherwise, the watch may be responded immediately
	respond(out, snapshot, group)
	return out
}
