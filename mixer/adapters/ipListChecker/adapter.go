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

package ipListChecker

import (
	"errors"
	"net/url"

	"istio.io/mixer/adapters"
)

// Config is used to configure an adapter.
type Config struct {
}

type adapter struct{}

// NewAdapter returns an Adapter
func NewAdapter() adapters.Adapter {
	return &adapter{}
}

func (a *adapter) Name() string {
	return "IPListChecker"
}

func (a *adapter) Description() string {
	return "Checks whether an IP address is present in an IP address list"
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
	c := config.(*InstanceConfig)
	var err error
	var u *url.URL

	if u, err = url.Parse(c.ProviderURL); err == nil {
		if u.Scheme == "" || u.Host == "" {
			err = errors.New("Scheme and Host cannot be nil")
		}
	}
	return err
}

func (a *adapter) NewInstance(config adapters.InstanceConfig) (adapters.Instance, error) {
	if err := a.ValidateInstanceConfig(config); err != nil {
		return nil, err
	}
	c := config.(*InstanceConfig)
	return newInstance(c)
}
