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

// Package denyChecker defines an adapter that always fails check requests.
package denyChecker

import (
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/genproto/googleapis/rpc/status"

	"istio.io/mixer/adapter/denyChecker/config"
	"istio.io/mixer/pkg/adapter"
)

// Register records the builders exposed by this adapter.
func Register(r adapter.Registrar) {
	r.RegisterDenyChecker(newBuilder())
}

type builderState struct{}

func newBuilder() adapter.DenyCheckerBuilder                                          { return builderState{} }
func (builderState) Name() string                                                     { return "istio/denyChecker" }
func (builderState) Description() string                                              { return "Denies every check request" }
func (builderState) Close() error                                                     { return nil }
func (builderState) ValidateConfig(c adapter.AspectConfig) (ce *adapter.ConfigErrors) { return }

func (builderState) DefaultConfig() adapter.AspectConfig {
	return &config.Params{Error: &status.Status{Code: int32(code.Code_FAILED_PRECONDITION)}}
}

func (builderState) NewDenyChecker(env adapter.Env, c adapter.AspectConfig) (adapter.DenyCheckerAspect, error) {
	return newAspect(c.(*config.Params))
}
