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
	"github.com/golang/protobuf/proto"
	"google.golang.org/genproto/googleapis/rpc/code"

	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/aspect/denyChecker"
	"istio.io/mixer/pkg/registry"

	"google.golang.org/genproto/googleapis/rpc/status"

	pb "istio.io/mixer/adapter/denyChecker/config_proto"
)

func Register(r registry.Registrar) error {
	return r.RegisterDeny(newAdapter())
}

type adapterState struct{}

func newAdapter() denyChecker.Adapter                                              { return &adapterState{} }
func (a *adapterState) Name() string                                               { return "istio/denyChecker" }
func (a *adapterState) Description() string                                        { return "Deny every check request" }
func (a *adapterState) Close() error                                               { return nil }
func (a *adapterState) ValidateConfig(cfg proto.Message) (ce *aspect.ConfigErrors) { return }

func (a *adapterState) DefaultConfig() proto.Message {
	return &pb.Config{&status.Status{Code: int32(code.Code_FAILED_PRECONDITION)}}
}

func (a *adapterState) NewAspect(env aspect.Env, cfg proto.Message) (denyChecker.Aspect, error) {
	return newAspect(cfg.(*pb.Config))
}
