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

// Package util Current setup is based on the httpbin is deployed by the asm-lib.sh.
// TODO: Install httpbin in Go code or use echo
package util

import (
	"istio.io/istio/pkg/test/framework"
	ingressutil "istio.io/istio/tests/integration/security/sds_ingress/util"
)

// SetupConfig Setup following items assuming httpbin is deployed to default namespace:
// 1. Create k8s secret using existing cert
func SetupConfig(ctx framework.TestContext) {
	credName := "userauth-tls-cert"

	// setup secrets
	ingressutil.CreateIngressKubeSecret(ctx, []string{credName}, ingressutil.TLS, ingressutil.IngressCredentialA, false)
	ctx.ConditionalCleanup(func() {
		ingressutil.DeleteKubeSecret(ctx, []string{credName})
	})
}
