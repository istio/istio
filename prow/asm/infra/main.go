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

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"istio.io/istio/prow/asm/infra/exec"
	"istio.io/istio/prow/asm/infra/gke"

	shell "github.com/kballard/go-shellquote"
)

var (
	baseDeployerFlags = []string{"--up"}
)

type options struct {
	kubetest2WorkingDir string
	deployerName        string
	extraDeployerFlags  string
	testScript          string
	testFlags           string
	clusterTopology     string
	featureToTest       string
}

func main() {
	o := options{}
	flag.StringVar(&o.kubetest2WorkingDir, "kubetest2-working-dir", "", "the working directory for running the kubetest2 command")
	flag.StringVar(&o.deployerName, "deployer", "", "kubetest2 deployer name, can be gke, tailorbird or kind")
	flag.StringVar(&o.extraDeployerFlags, "deployer-flags", "", "extra flags corresponding to the deployer being used, supported flags can be"+
		" checked by running `kubetest2 [deployer] --help`")
	flag.StringVar(&o.testScript, "test-script", "", "the script to run the tests after clusters are created")
	flag.StringVar(&o.testFlags, "test-flags", "", "flags to pass through to the test script")
	flag.StringVar(&o.clusterTopology, "topology", "SINGLECLUSTER", "cluster topology for the SUT, can be one of SINGLECLUSTER, MULTICLUSTER and MULTIPROJECT_MULTICLUSTER")
	flag.StringVar(&o.featureToTest, "feature", "", "The feature to test for ASM, for now can only be VPC_SC if not empty")
	flag.Parse()

	if err := o.initSetup(); err != nil {
		log.Fatal("Error initializing the setups: ", err)
	}

	var extraDeployerFlagArr, testFlagArr []string
	var err error
	if o.extraDeployerFlags != "" {
		extraDeployerFlagArr, err = shell.Split(o.extraDeployerFlags)
		if err != nil {
			log.Fatalf("Error parsing the deployer flags %q: %v", o.extraDeployerFlags, err)
		}
	}
	if o.testFlags != "" {
		testFlagArr, err = shell.Split(o.testFlags)
		if err != nil {
			log.Fatalf("Error parsing the test flags %q: %v", o.testFlags, err)
		}
	}

	if os.Getenv("CI") != "true" {
		// Also tear down the clusters in non-CI environment.
		baseDeployerFlags = append(baseDeployerFlags, "--down")
	}
	baseDeployerFlags = append(baseDeployerFlags, extraDeployerFlagArr...)
	if err := o.runKubetest2(baseDeployerFlags, testFlagArr); err != nil {
		log.Fatal("Error running the test flow with kubetest2: ", err)
	}
}

func (o *options) initSetup() error {
	o.setEnvVars()

	if err := o.installTools(); err != nil {
		return fmt.Errorf("error installing tools for running %s deployer: %w", o.deployerName, err)
	}

	return nil
}

func (o *options) setEnvVars() {
	// Run the Go tests with verbose logging.
	os.Setenv("T", "-v")

	os.Setenv("DEPLOYER", o.deployerName)
	os.Setenv("CLUSTER_TOPOLOGY", o.clusterTopology)
	os.Setenv("FEATURE_TO_TEST", o.featureToTest)
}

func (o *options) installTools() error {
	if o.deployerName == "tailorbird" {
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

		log.Println("Installing herc CLI...")
		if err := exec.Run(fmt.Sprintf("bash -c 'gsutil cp gs://anthos-hercules-public-artifacts/herc/latest/herc /usr/local/bin/ &&" +
			" chmod 755 /usr/local/bin/herc'")); err != nil {
			return fmt.Errorf("error installing the herc CLI: %w", err)
		}
	}

	return nil
}

func (o *options) runKubetest2(deployerFlags, testFlags []string) error {
	switch o.deployerName {
	case "gke":
		log.Println("Will run kubetest2 gke deployer to create the clusters...")
		switch o.clusterTopology {
		case "SINGLECLUSTER":
			deployerFlags = append(deployerFlags, gke.SingleClusterFlags()...)
		case "MULTICLUSTER":
			deployerFlags = append(deployerFlags, gke.MultiClusterFlags()...)
		case "MULTIPROJECT_MULTICLUSTER":
			extraFlags, err := gke.MultiProjectMultiClusterFlags()
			if err != nil {
				return fmt.Errorf("error constructing the flags for multi-project multi-cluster setup: %w", err)
			}
			deployerFlags = append(deployerFlags, extraFlags...)
		default:
			log.Fatalf("The cluster topology %q is not supported, please double check!", o.clusterTopology)
		}

		if o.featureToTest != "" {
			switch o.featureToTest {
			case "VPC_SC":
				extraFlags, err := gke.ExtraVPCSCClusterFlags()
				if err != nil {
					return fmt.Errorf("error getting the extra flags for testing with VPC-SC: %w", err)
				}
				deployerFlags = append(deployerFlags, extraFlags...)
			default:
				log.Fatalf("The feature %q is not supported, please double check!", o.featureToTest)
			}
		}
	case "tailorbird":
		log.Println("Will run kubetest2 tailorbird deployer to create the clusters...")
		// Always tear down the clusters created by Tailorbird after test is finished.
		deployerFlags = append(deployerFlags, "--down")
		deployerFlags = append(deployerFlags, "--tbenv=int", "--verbose")
	default:
		log.Fatalf("The deployer %q is not supported, please double check!", o.deployerName)
	}

	kubetest2Flags := []string{o.deployerName}
	kubetest2Flags = append(kubetest2Flags, deployerFlags...)
	kubetest2Flags = append(kubetest2Flags, "--test=exec", "--", o.testScript)
	kubetest2Flags = append(kubetest2Flags, testFlags...)
	if err := exec.Run(fmt.Sprintf("kubetest2 %s", strings.Join(kubetest2Flags, " ")), exec.WithWorkingDir(o.kubetest2WorkingDir)); err != nil {
		return err
	}

	return nil
}
