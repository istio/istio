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

// Package genericListChecker defines an adapter that checks the existence of a symbol in a configured list of symbols.
package genericListChecker

import (
	"istio.io/mixer/adapter/genericListChecker/config"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/adapter/listChecker"
	"istio.io/mixer/pkg/registry"
)

// Register records the existence of this adapter
func Register(r registry.Registrar) error {
	return r.RegisterCheckList(newAdapter())
}

type adapterState struct{}

func newAdapter() listChecker.Adapter                                                    { return &adapterState{} }
func (a *adapterState) Name() string                                                     { return "istio/genericListChecker" }
func (a *adapterState) Description() string                                              { return "Checks whether a string is present in a list." }
func (a *adapterState) Close() error                                                     { return nil }
func (a *adapterState) ValidateConfig(c adapter.AspectConfig) (ce *adapter.ConfigErrors) { return }
func (a *adapterState) DefaultConfig() adapter.AspectConfig                              { return &config.Params{} }

func (a *adapterState) NewListChecker(env adapter.Env, c adapter.AspectConfig) (listChecker.Aspect, error) {
	return newAspect(c.(*config.Params))
}
