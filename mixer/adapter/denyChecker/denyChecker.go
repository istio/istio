// Copyright 2016 Istio Authors
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
	rpc "github.com/googleapis/googleapis/google/rpc"

	"istio.io/mixer/adapter/denyChecker/config"
	"istio.io/mixer/pkg/adapter"
)

type (
	builder struct{ adapter.DefaultBuilder }
	denier  struct{ status rpc.Status }
)

var (
	name = "denyChecker"
	desc = "Denies every check request"
	conf = &config.Params{Error: rpc.Status{Code: int32(rpc.FAILED_PRECONDITION)}}
)

// Register records the builders exposed by this adapter.
func Register(r adapter.Registrar) {
	r.RegisterDenialsBuilder(newBuilder())
}

func newBuilder() builder {
	return builder{adapter.NewDefaultBuilder(name, desc, conf)}
}

func (builder) NewDenialsAspect(_ adapter.Env, c adapter.Config) (adapter.DenialsAspect, error) {
	return newDenyChecker(c.(*config.Params))
}

func newDenyChecker(c *config.Params) (*denier, error) {
	return &denier{status: c.Error}, nil
}

func (d *denier) Close() error     { return nil }
func (d *denier) Deny() rpc.Status { return d.status }
