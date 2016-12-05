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

package main

import (
	"istio.io/mixer"
	"istio.io/mixer/config/listChecker"
)

// ConfigManager keeps track of the total configuration state of the mixer. It is responsible for containing the parsed
// configuration for both the adapters themselves, as well as the bindings of the adapters inputs to the set of incoming
// facts.
type ConfigManager struct {
}

// NewConfigManager returns a new ConfigManager instance
func NewConfigManager() (*ConfigManager, error) {
	var mgr = ConfigManager{}
	return &mgr, nil
}

// GetListCheckerConfigBlocks returns a list of ListCheckerConfigBlocks for the given serverID/peerID values.
func (manager *ConfigManager) GetListCheckerConfigBlocks(dispatchKey mixer.DispatchKey) ([]*listChecker.ConfigBlock, error) {
	// TODO: return an empty list for now, until we have support for configuration reading/evaluating.
	return make([]*listChecker.ConfigBlock, 0), nil
}
