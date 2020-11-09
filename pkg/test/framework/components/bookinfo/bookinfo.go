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

package bookinfo

import (
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

type Config struct {
	Namespace namespace.Instance
	Cfg       bookInfoConfig
}

// Deploy returns a new instance of deployed BookInfo
func Deploy(ctx resource.Context, cfg Config) (undeploy func(), err error) {
	return deploy(ctx, cfg)
}

// DeployOrFail returns a new instance of deployed BookInfo or fails test
func DeployOrFail(t test.Failer, ctx resource.Context, cfg Config) (undeploy func()) {
	t.Helper()

	var err error
	undeploy, err = Deploy(ctx, cfg)
	if err != nil {
		t.Fatalf("bookinfo.DeployOrFail: %v", err)
	}

	return
}
