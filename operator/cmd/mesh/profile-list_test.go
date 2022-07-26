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

package mesh

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega"

	"istio.io/istio/pkg/test/env"
)

func TestProfileList(t *testing.T) {
	g := gomega.NewWithT(t)
	args := []string{"profile", "list", "--dry-run", "--manifests", filepath.Join(env.IstioSrc, "manifests")}

	rootCmd := GetRootCmd(args)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("failed to execute istioctl profile command: %v", err)
	}
	output := out.String()
	expectedProfiles := []string{"default", "demo", "empty", "minimal", "openshift", "preview", "external"}
	for _, prof := range expectedProfiles {
		g.Expect(output).To(gomega.ContainSubstring(prof))
	}
}
