// Copyright 2016 Google Inc.
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

package genericListChecker

import (
	"istio.io/mixer/adapters"
)

// AdapterConfig is used to configure an adapter.
type AdapterConfig struct {
	adapters.AdapterConfig

	// The set of entries in the list to check against. This overrides any builder-level
	// entries. If this is not supplied, then the builder's list is used instead.
	ListEntries []string
}

type adapter struct {
	entries       map[string]string
	whitelistMode bool
}

// newAdapter returns a new adapter.
func newAdapter(config *AdapterConfig, entries map[string]string, whitelistMode bool) (adapters.ListChecker, error) {
	if config.ListEntries != nil {
		// override the builder-level entries
		entries = make(map[string]string, len(config.ListEntries))
		for _, entry := range config.ListEntries {
			entries[entry] = entry
		}
	}

	return &adapter{entries: entries, whitelistMode: whitelistMode}, nil
}

func (a *adapter) Close() error {
	a.entries = nil
	return nil
}

func (a *adapter) CheckList(symbol string) (bool, error) {
	_, ok := a.entries[symbol]
	if a.whitelistMode {
		return ok, nil
	}
	return !ok, nil
}
