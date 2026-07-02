//go:build integ

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

package dynamics

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

var ist istio.Instance

// setupConfig configures the mesh with ALLOW_ANY_DYNAMIC_DNS outbound traffic policy.
// This mode is only valid at the MeshConfig level (rejected by validation in Sidecar CRD),
// so it must be applied via the istio install rather than per-namespace.
func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}
	cfg.ControlPlaneValues = `
meshConfig:
  outboundTrafficPolicy:
    mode: ALLOW_ANY_DYNAMIC_DNS
`
	cfg.RemoteClusterValues = cfg.ControlPlaneValues
}

func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		Label(label.CustomSetup).
		Setup(istio.Setup(&ist, setupConfig)).
		Run()
}
