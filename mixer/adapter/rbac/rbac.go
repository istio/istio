// Copyright 2018 Istio Authors
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

// nolint: lll
//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -a mixer/adapter/rbac/config/config.proto -x "-n rbac -t authorization"

// Package rbac is deprecated by native RBAC implemented in Envoy proxy.
package rbac

import (
	"context"

	"istio.io/istio/mixer/adapter/metadata"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/authorization"
)

type (
	builder struct{}
)

const (
	prompt = "Mixer RBAC adapter is deprecated by native RBAC implemented in Envoy proxy. See " +
		"https://istio.io/docs/concepts/security/#enabling-authorization for enabling the native RBAC with " +
		"your existing service role and service role binding."
)

///////////////// Configuration-time Methods ///////////////

func (b *builder) SetAdapterConfig(cfg adapter.Config) {}

func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	return
}

func (b *builder) SetAuthorizationTypes(types map[string]*authorization.Type) {}

func (b *builder) Build(ctx context.Context, env adapter.Env) (adapter.Handler, error) {
	return nil, env.Logger().Errorf(prompt)
}

////////////////// Bootstrap //////////////////////////

// GetInfo returns the adapter.Info specific to this adapter.
func GetInfo() adapter.Info {
	info := metadata.GetInfo("rbac")
	info.NewBuilder = func() adapter.HandlerBuilder { return &builder{} }
	return info
}
