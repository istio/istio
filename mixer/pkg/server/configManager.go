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

package server

import (
	"istio.io/mixer/adapter/jsonLogger"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/config/listChecker"
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

// GetListCheckerConfigBlocks returns a list of ListCheckerConfigBlocks for the given dispatchKey.
func (manager *ConfigManager) GetListCheckerConfigBlocks(dispatchKey DispatchKey) ([]*listChecker.ConfigBlock, error) {
	// TODO: return an empty list for now, until we have support for configuration reading/evaluating.
	return make([]*listChecker.ConfigBlock, 0), nil
}

// GetLoggerAdapterConfigs returns a list of AdapterConfigs for the given dispatchKey.
func (manager *ConfigManager) GetLoggerAdapterConfigs(dispatchKey DispatchKey) ([]*adapter.AdapterConfig, error) {
	// TODO: Create an adapter config in an extremely hacky way.
	adapterConfig := jsonLogger.NewBuilder().DefaultAdapterConfig()
	result := make([]*adapter.AdapterConfig, 1)

	result[0] = &adapterConfig
	return result, nil
}
