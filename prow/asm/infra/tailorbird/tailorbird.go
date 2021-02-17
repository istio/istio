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

package tailorbird

import (
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"text/template"
	"time"

	"istio.io/istio/prow/asm/infra/exec"
)

const (
	// the relative dir to find the tailorbird config files
	configRelDir = "tailorbird/config"
)

func tailorbirdDeployerBaseFlags() []string {
	return []string{"--down", "--tbenv=int", "--status-check-interval=60", "--verbose"}
}

// InstallTools installs the required tools to enable interacting with Tailorbird.
func InstallTools(clusterType string) error {
	log.Println("Installing kubetest2 tailorbird deployer...")
	cookieFile := "/secrets/cookiefile/cookies"
	exec.Run("git config --global http.cookiefile " + cookieFile)
	goPath := os.Getenv("GOPATH")
	clonePath := goPath + "/src/gke-internal/test-infra"
	exec.Run(fmt.Sprintf("git clone https://gke-internal.googlesource.com/test-infra %s", clonePath))
	if err := exec.Run(fmt.Sprintf("bash -c 'cd %s &&"+
		" go install %s/anthos/tailorbird/cmd/kubetest2-tailorbird'", clonePath, clonePath)); err != nil {
		return fmt.Errorf("error installing kubetest2 tailorbird deployer: %w", err)
	}
	exec.Run("rm -r " + clonePath)

	if clusterType == "gke-on-prem" {
		log.Println("Installing herc CLI...")
		if err := exec.Run(fmt.Sprintf("bash -c 'gsutil cp gs://anthos-hercules-public-artifacts/herc/latest/herc /usr/local/bin/ &&" +
			" chmod 755 /usr/local/bin/herc'")); err != nil {
			return fmt.Errorf("error installing the herc CLI: %w", err)
		}
	}

	return nil
}

// DeployerFlags returns the deployer flags needed for the given cluster type,
// cluster topology and the feature to test
func DeployerFlags(clusterType, clusterTopology, featureToTest string) ([]string, error) {
	flags := tailorbirdDeployerBaseFlags()

	fp, err := configFilePath(clusterType, clusterTopology)
	if err != nil {
		return nil, fmt.Errorf("error getting the config file for tailorbird: %w", err)
	}
	flags = append(flags, "--tbconfig="+fp)
	return flags, nil
}

// configFilePath returns the config file path used for creating Tailorbird resources.
func configFilePath(clusterType, clusterTopology string) (string, error) {
	fn := fmt.Sprintf("%s-%s.yaml", clusterType, clusterTopology)
	tmplRelPath := filepath.Join(configRelDir, fn)
	if _, err := os.Stat(tmplRelPath); err != nil {
		return "", fmt.Errorf("%q does not exist in %q", fn, configRelDir)
	}

	newPath, err := execTmpl(tmplRelPath)
	if err != nil {
		return "", err
	}

	return newPath, nil
}

// execTmpl executes the config file as a template and returns the updated
// config file path.
func execTmpl(tmplFile string) (string, error) {
	type Replace struct {
		Random6 string
	}
	rep := Replace{Random6: randSeq(6)}

	tmpl, err := template.New(path.Base(tmplFile)).ParseFiles(tmplFile)
	if err != nil {
		return "", fmt.Errorf("error parsing the template: %w", err)
	}

	tmpFile, err := ioutil.TempFile("", "tailorbird-config-*.yaml")
	if err != nil {
		return "", fmt.Errorf("error creating the temporary file: %w", err)
	}

	if err := tmpl.Execute(tmpFile, rep); err != nil {
		return "", fmt.Errorf("error executing the template: %w", err)
	}

	return tmpFile.Name(), nil
}

var letters = []rune("abcdefghijklmnopqrstuvwxyz")

func randSeq(n int) string {
	rand.Seed(time.Now().UnixNano())
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
