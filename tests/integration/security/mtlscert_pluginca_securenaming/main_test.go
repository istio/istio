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

package mtlscertplugincasecurenaming

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/tests/integration/security/util/cert"
)

var (
	inst istio.Instance
)

func TestMain(m *testing.M) {
	// This test verifies:
	// - The certificate issued by CA to the sidecar is as expected and that strict mTLS works as expected.
	// - The plugin CA certs are correctly used in workload mTLS.
	// - The CA certificate in the configmap of each namespace is as expected, which
	//   is used for data plane to control plane TLS authentication.
	// - Secure naming information is respected in the mTLS handshake.
	framework.
		NewSuite(m).
		// k8s is required because the plugin CA key and certificate are stored in a k8s secret.

		RequireSingleCluster().
		Label(label.CustomSetup).
		Setup(istio.Setup(&inst, nil, cert.CreateCASecret)).
		Run()
}
