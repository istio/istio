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
	"fmt"
	"net/url"

	"github.com/golang/protobuf/proto"

	"istio.io/mixer/pkg/aspect/listChecker"
	"istio.io/mixer/pkg/aspectsupport"
)

const (
	// ImplName is the canonical name of this implementation
	ImplName = "istio/ipListChecker"
)

// Register registration entry point
func Register(r aspectsupport.Registry) error {
	return r.RegisterCheckList(newAdapter())
}

type adapterState struct{}

func newAdapter() listChecker.Adapter {
	return &adapterState{}
}

func (a *adapterState) Name() string {
	return ImplName
}

func (a *adapterState) Description() string {
	return "Checks whether an IP address is present in an IP address list."
}

func (a *adapterState) DefaultConfig() proto.Message {
	return &Config{
		ProviderUrl:     "http://localhost",
		RefreshInterval: 60,
		Ttl:             120,
	}
}

func (a *adapterState) ValidateConfig(cfg proto.Message) (err error) {
	c, ok := cfg.(*Config)
	if !ok {
		return fmt.Errorf("Invalid message type %#v", cfg)
	}
	var u *url.URL

	if u, err = url.Parse(c.ProviderUrl); err == nil {
		if u.Scheme == "" || u.Host == "" {
			err = fmt.Errorf("Scheme (%s) and Host (%s) cannot be empty", u.Scheme, u.Host)
		}
	}
	return err
}

func (a *adapterState) Close() error {
	return nil
}

func (a *adapterState) NewAspect(cfg proto.Message) (listChecker.Aspect, error) {
	if err := a.ValidateConfig(cfg); err != nil {
		return nil, err
	}

	return newAspect(cfg.(*Config))
}
