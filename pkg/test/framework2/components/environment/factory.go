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

package environment

import (
	"fmt"

	"istio.io/istio/pkg/test/framework2/components/environment/kube"
	"istio.io/istio/pkg/test/framework2/components/environment/native"
	"istio.io/istio/pkg/test/framework2/core"
)

type FactoryFn func(name string, ctx core.Context) (core.Environment, error)

// New returns a new environment instance.
func New(name string, ctx core.Context) (core.Environment, error) {
	switch name {
	case core.Native.String():
		return native.New(ctx)
	case core.Kube.String():
		return kube.New(ctx)
	default:
		return nil, fmt.Errorf("unknown environment: %q", name)
	}
}
