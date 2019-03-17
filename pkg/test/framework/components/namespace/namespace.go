// Copyright 2019 Istio Authors
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

package namespace

import (
	"testing"

	"istio.io/istio/pkg/test/framework/core"
)

// Instance represents an allocated namespace that can be used to create config, or deploy components in.
type Instance interface {
	Name() string
}

// Claim an existing namespace, or create a new one if doesn't exist.
func Claim(ctx core.Context, name string) (i Instance, err error) {
	err = core.UnsupportedEnvironment(ctx.Environment())
	ctx.Environment().Case(core.Native, func() {
		i, err = claimNative(ctx, name)
	})
	ctx.Environment().Case(core.Kube, func() {
		i, err = claimKube(ctx, name)
	})
	return
}

// ClaimOrFail calls Claim and fails test if it returns error
func ClaimOrFail(t *testing.T, ctx core.Context, name string) Instance {
	i, err := Claim(ctx, name)
	if err != nil {
		t.Fatalf("namespace.ClaimOrFail:: %v", err)
	}
	return i
}

// New creates a new Namespace.
func New(ctx core.Context, prefix string, inject bool) (i Instance, err error) {
	err = core.UnsupportedEnvironment(ctx.Environment())
	ctx.Environment().Case(core.Native, func() {
		i, err = newNative(ctx, prefix, inject)
	})
	ctx.Environment().Case(core.Kube, func() {
		i, err = newKube(ctx, prefix, inject)
	})
	return
}

// NewOrFail calls New and fails test if it returns error
func NewOrFail(t *testing.T, ctx core.Context, prefix string, inject bool) Instance {
	i, err := New(ctx, prefix, inject)
	if err != nil {
		t.Fatalf("namespace.NewOrFail:: %v", err)
	}
	return i
}
