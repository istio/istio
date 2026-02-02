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

package remotesecretfs

import (
	"fmt"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
)

const (
	remoteKubeconfigSecret    = "istio-remote-kubeconfigs"
	remoteKubeconfigMountPath = "/var/run/remote-secrets"
)

var i istio.Instance

func TestMain(m *testing.M) {
	// nolint: staticcheck
	framework.
		NewSuite(m).
		SkipIf("requires at least 2 clusters", func(ctx resource.Context) bool {
			return ctx.Clusters().Len() < 2
		}).
		Setup(istio.Setup(&i, func(_ resource.Context, cfg *istio.Config) {
			cfg.SkipDeployCrossClusterSecrets = true
			cfg.ControlPlaneValues = fmt.Sprintf(`values:
  pilot:
    env:
      PILOT_REMOTE_CLUSTER_SECRET_PATH: "%s"
    volumeMounts:
    - name: remote-kubeconfigs
      mountPath: "%s"
    volumes:
    - name: remote-kubeconfigs
      secret:
        secretName: %s
`, remoteKubeconfigMountPath, remoteKubeconfigMountPath, remoteKubeconfigSecret)
		})).
		Run()
}
