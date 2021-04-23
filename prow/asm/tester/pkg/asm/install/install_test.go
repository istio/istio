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

package install

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"istio.io/istio/prow/asm/tester/pkg/asm/install/revision"
	"istio.io/istio/prow/asm/tester/pkg/resource"
)

func TestInstallationType(t *testing.T) {
	tcs := []struct {
		controlPlane resource.ControlPlaneType
		clusterType  resource.ClusterType
		want         installationType
	}{
		{
			controlPlane: resource.Unmanaged,
			clusterType:  resource.GKEOnGCP,
			want:         basic,
		},
		{
			controlPlane: resource.Managed,
			clusterType:  resource.GKEOnGCP,
			want:         mcp,
		},
		{
			controlPlane: resource.Unmanaged,
			clusterType:  resource.BareMetal,
			want:         bareMetal,
		},
		{
			controlPlane: resource.Unmanaged,
			clusterType:  resource.GKEOnAWS,
			want:         multiCloud,
		},
	}

	for _, tc := range tcs {
		i := installer{
			&resource.Settings{
				ControlPlane: tc.controlPlane,
				ClusterType:  tc.clusterType,
			},
		}
		got := i.installationType()
		if diff := cmp.Diff(tc.want, got); diff != "" {
			t.Errorf("got(+) is different from wanted(-)\n%v",
				diff)
		}
	}
}

func TestBasicInstallSettingsMerge(t *testing.T) {
	tcs := []struct {
		name           string
		settings       resource.Settings
		revisionConfig revision.RevisionConfig
		want           basicInstallConfig
	}{
		{
			name: "check empty revision",
			settings: resource.Settings{
				CA:  resource.Citadel,
				WIP: "wip-from-settings",
			},
			revisionConfig: revision.RevisionConfig{
				Overlay: "overlay",
			},
			want: basicInstallConfig{
				pkg:      filepath.Join(resource.ConfigDirPath, "kpt-pkg"),
				ca:       string(resource.Citadel),
				wip:      "wip-from-settings",
				overlay:  "overlay",
				flags:    "\"\"",
				revision: "\"\"",
			},
		},
		{
			name: "check CA overrides",
			settings: resource.Settings{
				CA:  resource.Citadel,
				WIP: resource.GKEWorkloadIdentityPool,
			},
			revisionConfig: revision.RevisionConfig{
				CA: string(resource.MeshCA),
			},
			want: basicInstallConfig{
				pkg:      filepath.Join(resource.ConfigDirPath, "kpt-pkg"),
				ca:       string(resource.MeshCA),
				wip:      string(resource.GKEWorkloadIdentityPool),
				overlay:  "\"\"",
				flags:    "\"\"",
				revision: "\"\"",
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			i := &installer{&tc.settings}
			got := i.generateBasicInstallConfig(&tc.revisionConfig)
			if diff := cmp.Diff(tc.want, *got, cmp.AllowUnexported(basicInstallConfig{})); diff != "" {
				t.Errorf("got(+) is different from wanted(-)\n%v", diff)
			}
		})
	}
}
