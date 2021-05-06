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
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"istio.io/istio/prow/asm/infra/exec"
	"istio.io/istio/prow/asm/infra/gke"
	"istio.io/istio/prow/asm/infra/tailorbird"

	shell "github.com/kballard/go-shellquote"
)

const (
	gkeDeployerName        = "gke"
	tailorbirdDeployerName = "tailorbird"
)

var (
	baseDeployerFlags = []string{"--up", "--skip-test-junit-report"}
	baseTesterFlags   = []string{"--setup-env", "--teardown-env", "--setup-system", "--teardown-system", "--setup-tests", "--teardown-tests", "--run-tests"}
)

type options struct {
	repoRootDir        string
	deployerName       string
	clusterType        string
	extraDeployerFlags string
	testScript         string
	testFlags          string
	clusterTopology    string
	featureToTest      string
}

func main() {
	o := options{}
	flag.StringVar(&o.repoRootDir, "repo-root-dir", "", "the repo's root directory, will be used as the working directory for running the kubetest2 command")
	flag.StringVar(&o.clusterType, "cluster-type", "gke", "the cluster type, can be one of gke, gke-autopilot, gke-on-prem, bare-metal, etc")
	flag.StringVar(&o.extraDeployerFlags, "deployer-flags", "", "extra flags corresponding to the deployer being used, supported flags can be"+
		" checked by running `kubetest2 [deployer] --help`")
	flag.StringVar(&o.testScript, "test-script", "", "the script to run the tests after clusters are created")
	flag.StringVar(&o.testFlags, "test-flags", "", "flags to pass through to the test script")
	flag.StringVar(&o.clusterTopology, "topology", "SINGLECLUSTER", "cluster topology for the SUT, can be one of SINGLECLUSTER, MULTICLUSTER and MULTIPROJECT_MULTICLUSTER")
	flag.StringVar(&o.featureToTest, "feature", "", "The feature to test for ASM, for now can be VPC_SC, ADDON, or USER_AUTH if not empty")
	flag.Parse()

	if err := o.initSetup(); err != nil {
		log.Fatal("Error initializing the setups: ", err)
	}

	var extraDeployerFlagArr, extraTestFlagArr []string
	var err error
	if o.extraDeployerFlags != "" {
		extraDeployerFlagArr, err = shell.Split(o.extraDeployerFlags)
		if err != nil {
			log.Fatalf("Error parsing the deployer flags %q: %v", o.extraDeployerFlags, err)
		}
	}
	if o.testFlags != "" {
		extraTestFlagArr, err = shell.Split(o.testFlags)
		if err != nil {
			log.Fatalf("Error parsing the test flags %q: %v", o.testFlags, err)
		}
	}

	deployerFlags := append(baseDeployerFlags, extraDeployerFlagArr...)

	testerFlags := append(baseTesterFlags, "--repo-root-dir="+o.repoRootDir)
	testerFlags = append(testerFlags, "--cluster-type="+o.clusterType, "--cluster-topology="+o.clusterTopology, "--feature="+o.featureToTest)
	testerFlags = append(testerFlags, extraTestFlagArr...)

	if err := o.runTestFlow(deployerFlags, testerFlags); err != nil {
		log.Fatal("Error running the test flow: ", err)
	}
}

func (o *options) initSetup() error {
	if o.clusterType == "gke" || o.clusterType == "gke-autopilot" {
		o.deployerName = gkeDeployerName
	} else {
		o.deployerName = tailorbirdDeployerName
	}

	if err := o.installTools(); err != nil {
		return fmt.Errorf("error installing tools for running %s deployer: %w", o.deployerName, err)
	}

	return nil
}

func (o *options) installTools() error {
	if o.deployerName == tailorbirdDeployerName {
		if err := tailorbird.InstallTools(o.clusterType); err != nil {
			return fmt.Errorf("error installing tools for testing with Tailorbird: %w", err)
		}
	}

	return nil
}

func (o *options) runTestFlow(deployerFlags, testFlags []string) error {
	defer postprocessTestArtifacts()

	switch o.deployerName {
	case gkeDeployerName:
		log.Println("Will run kubetest2 gke deployer to create the clusters...")

		extraFlags, err := gke.DeployerFlags(o.clusterType, o.clusterTopology, o.featureToTest)
		if err != nil {
			return fmt.Errorf("error getting deployer flags for gke: %w", err)
		}
		deployerFlags = append(deployerFlags, extraFlags...)
	case tailorbirdDeployerName:
		log.Println("Will run kubetest2 tailorbird deployer to create the clusters...")

		extraFlags, err := tailorbird.DeployerFlags(o.clusterType, o.clusterTopology, o.featureToTest)
		if err != nil {
			return fmt.Errorf("error getting deployer flags for tailorbird: %w", err)
		}
		deployerFlags = append(deployerFlags, extraFlags...)
	default:
		return fmt.Errorf("unsupported deployer: %q", o.deployerName)
	}

	kubetest2Flags := []string{o.deployerName}
	kubetest2Flags = append(kubetest2Flags, deployerFlags...)

	if o.deployerName == gkeDeployerName {
		// Retry creating the cluster in different regions to reduce the test
		// flakniess caused by GCE_STOCKOUT and other recoverable GKE errors.
		// This is a temporary workaround before b/162609408 is solved upstream.
		// TODO(chizhg): remove the retry logic here after b/162609408 is solved
		//    upstream in kubetest2.
		for _, region := range []string{"us-central1", "us-west1", "us-east1"} {
			var buf bytes.Buffer
			newKubetest2Flags := make([]string, len(kubetest2Flags))
			copy(newKubetest2Flags, kubetest2Flags)
			newKubetest2Flags = append(newKubetest2Flags, "--region="+region)
			newKubetest2Flags = append(newKubetest2Flags, "--test=exec", "--", o.testScript)
			newKubetest2Flags = append(newKubetest2Flags, testFlags...)
			if err := exec.Run(fmt.Sprintf("kubetest2 %s", strings.Join(newKubetest2Flags, " ")),
				exec.WithWorkingDir(o.repoRootDir),
				exec.WithWriter(io.MultiWriter(os.Stdout, &buf), io.MultiWriter(os.Stderr, &buf))); err != nil {
				if !isRetryableError(buf.String()) {
					return err
				}
			} else {
				return nil
			}
		}
	} else {
		kubetest2Flags = append(kubetest2Flags, "--test=exec", "--", o.testScript)
		kubetest2Flags = append(kubetest2Flags, testFlags...)
		if err := exec.Run(fmt.Sprintf("kubetest2 %s", strings.Join(kubetest2Flags, " ")), exec.WithWorkingDir(o.repoRootDir)); err != nil {
			return err
		}
	}

	return nil
}

// If one of the error patterns below is matched, it would be recommended to
// retry creating the cluster in a different region.
// - stockout
// - nodes fail to start
// - component is unhealthy
var retryableCreationErrors = []*regexp.Regexp{
	regexp.MustCompile(`.*does not have enough resources available to fulfill.*`),
	regexp.MustCompile(`.*only \d+ nodes out of \d+ have registered; this is likely due to Nodes failing to start correctly.*`),
	regexp.MustCompile(`.*All cluster resources were brought up, but: component .+ from endpoint .+ is unhealthy.*`),
}

// isRetryableError checks if the error happens during cluster creation can be potentially solved by retrying or not.
func isRetryableError(errMsg string) bool {
	for _, regx := range retryableCreationErrors {
		if regx.MatchString(errMsg) {
			return true
		}
	}
	return false
}

// postprocessTestArtifacts will process the test artifacts after the test flow
// is finished.
func postprocessTestArtifacts() {
	if os.Getenv("CI") == "true" {
		log.Println("Postprocessing JUnit XML files to support aggregated view on Testgrid...")
		exec.Run("git config --global http.cookiefile /secrets/cookiefile/cookies")
		clonePath := os.Getenv("GOPATH") + "/src/gke-internal/knative/cloudrun-test-infra"
		exec.Run(fmt.Sprintf("git clone --single-branch --branch main https://gke-internal.googlesource.com/knative/cloudrun-test-infra %s", clonePath))
		defer os.RemoveAll(clonePath)
		exec.Run(fmt.Sprintf("bash -c 'cd %s && go install ./tools/crtest/cmd/crtest'", clonePath))

		filepath.Walk(os.Getenv("ARTIFACTS"), func(path string, info os.FileInfo, err error) error {
			if matched, _ := regexp.MatchString(`^junit.*\.xml`, info.Name()); matched {
				log.Printf("Update file %q", path)
				exec.Run(fmt.Sprintf("crtest xmlpost --file=%s --save --aggregate-subtests", path))
			}
			return nil
		})
	}
}
