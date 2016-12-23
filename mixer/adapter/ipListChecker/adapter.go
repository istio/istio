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
	"time"

	"istio.io/mixer/pkg/adapter"
)

// AdapterConfig is used to configure a adapter.
type AdapterConfig struct {
}

type adapterState struct{}

// NewAdapter returns a Adapter
func NewAdapter() adapter.Adapter {
	return &adapterState{}
}

func (a *adapterState) Name() string {
	return "IPListChecker"
}

func (a *adapterState) Description() string {
	return "Checks whether an IP address is present in an IP address list."
}

func (a *adapterState) DefaultAdapterConfig() adapter.AdapterConfig {
	return &AdapterConfig{}
}

func (a *adapterState) ValidateAdapterConfig(config adapter.AdapterConfig) error {
	_ = config.(*AdapterConfig)
	return nil
}

func (a *adapterState) Configure(config adapter.AdapterConfig) error {
	return a.ValidateAdapterConfig(config)
}

func (a *adapterState) Close() error {
	return nil
}

func (a *adapterState) DefaultAspectConfig() adapter.AspectConfig {
	return &AspectConfig{
		ProviderURL:     "http://localhost",
		RefreshInterval: time.Minute,
		TimeToLive:      time.Minute * 10,
	}
}

func (a *adapterState) ValidateAspectConfig(config adapter.AspectConfig) error {
	c := config.(*AspectConfig)
	var err error
	var u *url.URL

	if u, err = url.Parse(c.ProviderURL); err == nil {
		if u.Scheme == "" || u.Host == "" {
			err = errors.New("Scheme and Host cannot be nil")
		}
	}
	return err
}

func (a *adapterState) NewAspect(config adapter.AspectConfig) (adapter.Aspect, error) {
	if err := a.ValidateAspectConfig(config); err != nil {
		return nil, err
	}
	c := config.(*AspectConfig)
	return newAspect(c)
}
