//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package istio

import (
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/environment/native"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/environment"
)

// ClaimSystemNamespace creates, or claims if existing, the namespace for the Istio system components from the environment.
func ClaimSystemNamespace(ctx resource.Context) (namespace.Instance, error) {
	switch ctx.Environment().EnvironmentName() {
	case environment.Kube:
		istioCfg, err := DefaultConfig(ctx)
		if err != nil {
			return nil, err
		}
		return namespace.Claim(ctx, istioCfg.SystemNamespace, false, istioCfg.CustomSidecarInjectorNamespace)
	case environment.Native:
		ns := ctx.Environment().(*native.Environment).SystemNamespace
		return namespace.Claim(ctx, ns, false, "")
	default:
		return nil, resource.UnsupportedEnvironment(ctx.Environment())
	}
}

// ClaimSystemNamespaceOrFail calls ClaimSystemNamespace, failing the test if an error occurs.
func ClaimSystemNamespaceOrFail(t test.Failer, ctx resource.Context) namespace.Instance {
	t.Helper()
	i, err := ClaimSystemNamespace(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return i
}
