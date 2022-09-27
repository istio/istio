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
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega"
	"istio.io/istio/operator/pkg/util/httpserver"
	"istio.io/istio/operator/pkg/util/tgz"
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
	expectedProfiles := []string{"default", "demo", "empty", "minimal", "openshift", "preview", "remote", "external"}
	for _, prof := range expectedProfiles {
		g.Expect(output).To(gomega.ContainSubstring(prof))
	}
}

func TestProfileListByurl(t *testing.T) {
	g := gomega.NewWithT(t)
	tempChars, err := os.MkdirTemp("/tmp/", "profilelist-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempChars)
	manifestsPath := filepath.Join(tempChars, "istio", "istio-1.15.0", "manifests")
	mkCmd := exec.Command("mkdir", "-p", manifestsPath)
	if err = mkCmd.Run(); err != nil {
		t.Fatal(err)
	}
	cpCmd := exec.Command("cp", "-r", string(liveCharts), manifestsPath)
	if err = cpCmd.Run(); err != nil {
		t.Fatal(err)
	}
	if err = tgz.Create(filepath.Join(tempChars, "istio"), filepath.Join(tempChars, "istio-1.15.0-linux.tar.gz")); err != nil {
		t.Fatal(err)
	}
	svr := httpserver.NewServer(tempChars)
	defer svr.Close()
	args := []string{"profile", "list", "--dry-run", "--manifests", svr.URL() + "/" + "istio-1.15.0-linux.tar.gz"}

	rootCmd := GetRootCmd(args)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	err = rootCmd.Execute()
	if err != nil {
		t.Fatalf("failed to execute istioctl profile command: %v", err)
	}
	output := out.String()
	expectedProfiles := []string{"default", "demo", "empty", "minimal", "openshift", "preview", "external"}
	for _, prof := range expectedProfiles {
		g.Expect(output).To(gomega.ContainSubstring(prof))
	}
}
