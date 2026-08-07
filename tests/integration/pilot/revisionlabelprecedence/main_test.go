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

package revisionlabelprecedence

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

// TestMain installs two revisions, rev-a and rev-b, both configured with
// sidecarInjectorWebhook.revisionLabelPrecedence=pod. These are deliberately kept in
// their own package/cluster rather than added to tests/integration/pilot/revisions:
// co-installing a revisionLabelPrecedence=pod revision alongside a (default)
// revisionLabelPrecedence=namespace revision like that package's stable/canary trips
// istioctl's own webhook-overlap validation (IST0139) at install time, since the
// namespace-precedence revision's Case 1 claims any pod in its labeled namespace
// unconditionally while the pod-precedence revision's Case 2 claims any pod with a
// matching explicit label regardless of namespace - the two structurally overlap for
// any hypothetical pod caught between them, even though rev-a/rev-b alone do not
// overlap with each other.
func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		RequireMultiPrimary().
		// Requires two CPs with specific names to be configured.
		Label(label.CustomSetup).
		Setup(istio.Setup(nil, func(_ resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = `
revision: rev-a
values:
  sidecarInjectorWebhook:
    revisionLabelPrecedence: pod
`
		})).
		Setup(istio.Setup(nil, func(_ resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = `
profile: empty
revision: rev-b
components:
  pilot:
    enabled: true
values:
  sidecarInjectorWebhook:
    revisionLabelPrecedence: pod
`
		})).
		Run()
}
