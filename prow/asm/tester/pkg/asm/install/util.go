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
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"istio.io/istio/prow/asm/tester/pkg/asm/install/revision"
	"istio.io/istio/prow/asm/tester/pkg/exec"
	"istio.io/istio/prow/asm/tester/pkg/kube"
)

const scriptaroRepoBase = "https://raw.githubusercontent.com/GoogleCloudPlatform/anthos-service-mesh-packages"

func downloadScriptaro(rev *revision.Config, cluster *kube.GKEClusterSpec) (string, error) {
	scriptaroURL := fmt.Sprintf("%s/master/scripts/asm-installer/install_asm", scriptaroRepoBase)
	if rev.Version != "" {
		scriptaroURL = fmt.Sprintf("%s/staging-%s-asm/scripts/asm-installer/install_asm", scriptaroRepoBase, rev.Version)
	}
	scriptaroName := fmt.Sprintf("install_asm_%s_%s_%s",
		cluster.ProjectID, cluster.Location, cluster.Name)

	log.Printf("Downloading scriptaro from %s...", scriptaroURL)
	resp, err := http.Get(scriptaroURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("scriptaro not found at URL: %s", scriptaroURL)
	}

	f, err := os.OpenFile(scriptaroName, os.O_WRONLY|os.O_CREATE, 0o555)
	if err != nil {
		return "", err
	}

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		return "", err
	}
	f.Close()

	path, err := filepath.Abs(scriptaroName)
	if err != nil {
		return "", err
	}

	return path, nil
}

// revisionLabel generates a revision label name from the istioctl version.
func revisionLabel() string {
	istioVersion, _ := exec.RunWithOutput(
		"bash -c \"istioctl version --remote=false -o json | jq -r '.clientVersion.tag'\"")
	versionParts := strings.Split(istioVersion, "-")
	version := fmt.Sprintf("asm-%s-%s", versionParts[0], versionParts[1])
	return strings.ReplaceAll(version, ".", "")
}
