// Copyright 2017 Istio Authors
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

// Package store provides the interface to the backend storage
// for the config and the default fsstore implementation.
package store

import (
	"fmt"
	"net/url"
)

// IndexNotSupported will be used as the returned index value when
// the KeyValueStore implementation does not support index.
const IndexNotSupported = -1

// Builder is the type of function to build a KeyValueStore.
type Builder func(u *url.URL) (KeyValueStore, error)

// RegisterFunc is the type to register a builder for URL scheme.
type RegisterFunc func(map[string]Builder)

// ChangeType denotes the type of a change
type ChangeType int

const (
	// Update - change was an update or a create to a key.
	Update ChangeType = iota
	// Delete - key was removed.
	Delete
)

// Change - A record of mutation to the underlying KeyValueStore.
type Change struct {
	// Key that was affected
	Key string `json:"key"`
	// Type how did the key change
	Type ChangeType `json:"change_type"`
	// change log index number of the change
	Index int `json:"index"`
}

// KeyValueStore defines the key value store back end interface used by mixer
// and Mixer config API server.
//
// It should support back ends like redis, etcd and NFS
// All commands should return a change log index number which can be used
// to Read changes. If a KeyValueStore does not support it,
// it should return -1
type KeyValueStore interface {
	// Get value at a key, false if not found.
	Get(key string) (value string, index int, found bool)

	// Set a value.
	Set(key string, value string) (index int, err error)

	// List keys with the prefix.
	List(key string, recurse bool) (keys []string, index int, err error)

	// Delete a key.
	Delete(key string) error

	// Close the storage.
	Close()

	fmt.Stringer
}

// ChangeLogReader read change log from the KV Store
type ChangeLogReader interface {
	// Read reads change events >= index
	Read(index int) ([]Change, error)
}

// ChangeNotifier implements change notification machinery for the KeyValueStore.
type ChangeNotifier interface {
	// Register StoreListener
	// KeyValueStore should call this method when there is a change
	// The client should issue ReadChangeLog to see what has changed if the call is available.
	// else it should re-read the store, perform diff and apply changes.
	RegisterListener(s Listener)
}

// Listener listens for calls from the store that some keys have changed.
type Listener interface {
	// NotifyStoreChanged notify listener that a new change is available.
	NotifyStoreChanged(index int)
}

// Registry keeps the relationship between the URL scheme and the storage
// implementation.
type Registry struct {
	builders map[string]Builder
}

// NewRegistry creates a new Registry instance for the inventory.
func NewRegistry(inventory ...RegisterFunc) *Registry {
	b := map[string]Builder{}
	for _, rf := range inventory {
		rf(b)
	}
	return &Registry{builders: b}
}

// URL types supported by the config store
const (
	// example fs:///tmp/testdata/configroot
	FSUrl = "fs"
)

// NewStore create a new store based on the config URL.
func (r *Registry) NewStore(configURL string) (KeyValueStore, error) {
	u, err := url.Parse(configURL)

	if err != nil {
		return nil, fmt.Errorf("invalid config URL %s %v", configURL, err)
	}

	if u.Scheme == FSUrl {
		return newFSStore(u.Path)
	}
	if builder, ok := r.builders[u.Scheme]; ok {
		return builder(u)
	}

	return nil, fmt.Errorf("unknown config URL %s %v", configURL, u)
}
