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

package denyChecker

import (
	"istio.io/mixer/adapters"
)

// Config is used to configure an adapter
type Config struct {
}

type adapter struct{}

// NewAdapter returns an Adapter
func NewAdapter() adapters.Adapter {
	return &adapter{}
}

func (a *adapter) Name() string {
	return "DenyChecker"
}

func (a *adapter) Description() string {
	return "Deny every check request"
}

func (a *adapter) DefaultConfig() adapters.Config {
	return &Config{}
}

func (a *adapter) ValidateConfig(config adapters.Config) error {
	_ = config.(*Config)
	return nil
}

func (a *adapter) Activate(config adapters.Config) error {
	// nothing to do for this adapter...
	return a.ValidateConfig(config)
}

func (a *adapter) Deactivate() {
}

func (a *adapter) DefaultInstanceConfig() adapters.InstanceConfig {
	return &InstanceConfig{}
}

func (a *adapter) ValidateInstanceConfig(config adapters.InstanceConfig) error {
	_ = config.(*InstanceConfig)
	return nil
}

func (a *adapter) NewInstance(config adapters.InstanceConfig) (adapters.Instance, error) {
	if err := a.ValidateInstanceConfig(config); err != nil {
		return nil, err
	}
	c := config.(*InstanceConfig)
	return newInstance(c)
}
