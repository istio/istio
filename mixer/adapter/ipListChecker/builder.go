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

// BuilderConfig is used to configure a builder.
type BuilderConfig struct {
}

type builder struct{}

// NewBuilder returns a Builder
func NewBuilder() adapter.Builder {
	return &builder{}
}

func (b *builder) Name() string {
	return "IPListChecker"
}

func (b *builder) Description() string {
	return "Checks whether an IP address is present in an IP address list."
}

func (b *builder) DefaultBuilderConfig() adapter.BuilderConfig {
	return &BuilderConfig{}
}

func (b *builder) ValidateBuilderConfig(config adapter.BuilderConfig) error {
	_ = config.(*BuilderConfig)
	return nil
}

func (b *builder) Configure(config adapter.BuilderConfig) error {
	return b.ValidateBuilderConfig(config)
}

func (b *builder) Close() error {
	return nil
}

func (b *builder) DefaultAspectConfig() adapter.AspectConfig {
	return &AspectConfig{
		ProviderURL:     "http://localhost",
		RefreshInterval: time.Minute,
		TimeToLive:      time.Minute * 10,
	}
}

func (b *builder) ValidateAspectConfig(config adapter.AspectConfig) error {
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

func (b *builder) NewAspect(config adapter.AspectConfig) (adapter.Aspect, error) {
	if err := b.ValidateAspectConfig(config); err != nil {
		return nil, err
	}
	c := config.(*AspectConfig)
	return newAspect(c)
}
