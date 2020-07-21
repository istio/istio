// Copyright Istio Authors
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
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/kube"
	"istio.io/istio/pkg/test/framework/resource"
)

// NewBuilder for Echo Instances.
func NewBuilder(ctx resource.Context) echo.Builder {
	return kube.NewBuilder(ctx)
}

// NewBuilder for Echo Instances.
func NewBuilderOrFail(t test.Failer, ctx resource.Context) echo.Builder {
	t.Helper()
	return NewBuilder(ctx)
}
