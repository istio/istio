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

// Package ipListChecker defines an adapter that checks the existence of an IP address in an online IP address list.
package ipListChecker

import (
	"net/url"

	"istio.io/mixer/adapter/ipListChecker/config"
	"istio.io/mixer/pkg/adapter"
)

// Register records the builders exposed by this adapter.
func Register(r adapter.Registrar) error {
	return r.RegisterListChecker(newBuilder())
}

type builderState struct{}

func newBuilder() adapter.ListCheckerBuilder { return builderState{} }
func (builderState) Name() string            { return "istio/ipListChecker" }

func (builderState) Description() string {
	return "Checks whether an IP address is present in an IP address list."
}

func (builderState) Close() error { return nil }

func (builderState) ValidateConfig(cfg adapter.AspectConfig) (ce *adapter.ConfigErrors) {
	c := cfg.(*config.Params)

	u, err := url.Parse(c.ProviderUrl)
	if err != nil {
		ce = ce.Append("ProviderUrl", err)
	} else {
		if u.Scheme == "" || u.Host == "" {
			ce = ce.Appendf("ProviderUrl", "URL scheme and host cannot be empty")
		}
	}

	return
}

func (builderState) DefaultConfig() adapter.AspectConfig {
	return &config.Params{
		ProviderUrl:     "http://localhost",
		RefreshInterval: 60,
		Ttl:             120,
	}
}

func (builderState) NewListChecker(env adapter.Env, c adapter.AspectConfig) (adapter.ListCheckerAspect, error) {
	return newAspect(env, c.(*config.Params))
}
