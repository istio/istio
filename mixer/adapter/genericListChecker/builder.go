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
)

// Register records the builders exposed by this adapter.
func Register(r adapter.Registrar) error {
	return r.RegisterListChecker(newBuilder())
}

type builderState struct{}

func newBuilder() adapter.ListCheckerBuilder                                          { return builderState{} }
func (builderState) Name() string                                                     { return "istio/genericListChecker" }
func (builderState) Description() string                                              { return "Checks whether a symbol is present in a list." }
func (builderState) Close() error                                                     { return nil }
func (builderState) ValidateConfig(c adapter.AspectConfig) (ce *adapter.ConfigErrors) { return }
func (builderState) DefaultConfig() adapter.AspectConfig                              { return &config.Params{} }

func (builderState) NewListChecker(env adapter.Env, c adapter.AspectConfig) (adapter.ListCheckerAspect, error) {
	return newAspect(c.(*config.Params))
}
