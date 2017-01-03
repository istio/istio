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
	"fmt"

	"github.com/golang/protobuf/proto"

	"istio.io/mixer/pkg/aspect/listChecker"
	"istio.io/mixer/pkg/aspectsupport"
)

const (
	// ImplName is the canonical name of this implementation
	ImplName = "istio/genericListChecker"
)

// Register records the existence of this adapter
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
	return "Checks whether a string is present in a list."
}

func (a *adapterState) DefaultConfig() proto.Message {
	return &Config{}
}

func (a *adapterState) ValidateConfig(cfg proto.Message) (err error) {
	_, ok := cfg.(*Config)
	if !ok {
		return fmt.Errorf("Invalid message type %#v", cfg)
	}
	return nil
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
