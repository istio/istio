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

package echoboot

import (
	"testing"

	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/kube"
	"istio.io/istio/pkg/test/framework/components/echo/native"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/resource"
)

// New returns a new instance of echo.
func New(ctx resource.Context, cfg echo.Config) (i echo.Instance, err error) {
	err = resource.UnsupportedEnvironment(ctx.Environment())

	ctx.Environment().Case(environment.Native, func() {
		i, err = native.New(ctx, cfg)
	})

	ctx.Environment().Case(environment.Kube, func() {
		i, err = kube.New(ctx, cfg)
	})
	return
}

// NewOrFail returns a new instance of echo, or fails t if there is an error.
func NewOrFail(t *testing.T, ctx resource.Context, cfg echo.Config) echo.Instance {
	t.Helper()
	i, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("echo.NewOrFail: %v", err)
	}

	return i
}
