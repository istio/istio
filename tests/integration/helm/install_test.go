// +build integ
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

package helm

import (
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
)

const (
	IstioNamespace = "istio-system"
	retryDelay     = time.Second
	retryTimeOut   = 20 * time.Minute
)

// TestInstall tests Istio installation using Helm
func TestInstall(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			switch ctx.Environment().EnvironmentName() {
			case environment.Kube:
			default:
				t.Errorf("Provided environment is not supported for this test.")
			}
		})
}
