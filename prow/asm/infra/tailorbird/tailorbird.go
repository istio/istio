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
	return []string{"--down", "--status-check-interval=60", "--verbose"}
}

// InstallTools installs the required tools to enable interacting with Tailorbird.
func InstallTools(clusterType string) error {
	clonePath := os.Getenv("GOPATH") + "/src/gke-internal/test-infra"
	if _, err := os.Stat(clonePath); !os.IsNotExist(err) {
		if err := exec.Run(fmt.Sprintf("bash -c 'cd %s && go install ./anthos/tailorbird/cmd/kubetest2-tailorbird'", clonePath)); err != nil {
			return fmt.Errorf("error installing kubetest2 tailorbird deployer: %w", err)
		}
	} else {
		return fmt.Errorf("path %q does not seem to exist, please double check", clonePath)
	}

	// GKE-on-AWS needs terraform for generation of kubeconfigs
	// TODO(chizhg): remove the terraform installation after b/171729099 is solved.
	if clusterType == "aws" {
		terraformVersion := "0.13.6"
		if err := exec.Run("bash -c 'apt-get update && apt-get install unzip -y'"); err != nil {
			return err
		}
		installTerraformCmd := fmt.Sprintf(`wget --no-verbose https://releases.hashicorp.com/terraform/%s/terraform_%s_linux_amd64.zip \
		&& unzip terraform_%s_linux_amd64.zip \
		&& mv terraform /usr/local/bin/terraform \
		&& rm terraform_%s_linux_amd64.zip`, terraformVersion, terraformVersion, terraformVersion, terraformVersion)
		if err := exec.Run("bash -c '" + installTerraformCmd + "'"); err != nil {
			return fmt.Errorf("error installing terraform for testing with aws")
		}
	}

	if clusterType == "eks" {
		installawsIamAuthenticatorCmd := `curl -o aws-iam-authenticator https://amazon-eks.s3.us-west-2.amazonaws.com/1.19.6/2021-01-05/bin/linux/amd64/aws-iam-authenticator \
			&& chmod +x ./aws-iam-authenticator \
			&& mv ./aws-iam-authenticator /usr/local/bin/aws-iam-authenticator`

		if err := exec.Run("bash -c '" + installawsIamAuthenticatorCmd + "'"); err != nil {
			return fmt.Errorf("error installing aws-iam-authenticator for testing with eks")
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
