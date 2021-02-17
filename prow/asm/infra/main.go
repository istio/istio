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
	baseDeployerFlags = []string{"--up"}
)

type options struct {
	kubetest2WorkingDir string
	deployerName        string
	clusterType         string
	extraDeployerFlags  string
	testScript          string
	testFlags           string
	clusterTopology     string
	featureToTest       string
}

func main() {
	o := options{}
	flag.StringVar(&o.kubetest2WorkingDir, "kubetest2-working-dir", "", "the working directory for running the kubetest2 command")
	flag.StringVar(&o.deployerName, "deployer", "", "kubetest2 deployer name, can be gke or tailorbird. Will be deprecated, use --cluster-type instead.")
	flag.StringVar(&o.clusterType, "cluster-type", "gke", "the cluster type, can be one of gke, gke-on-prem, bare-metal, etc")
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

	baseDeployerFlags = append(baseDeployerFlags, extraDeployerFlagArr...)
	if err := o.runKubetest2(baseDeployerFlags, testFlagArr); err != nil {
		log.Fatal("Error running the test flow with kubetest2: ", err)
	}
}

func (o *options) initSetup() error {
	if o.clusterType == "gke" {
		o.deployerName = gkeDeployerName
	} else {
		o.deployerName = tailorbirdDeployerName
	}

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
	os.Setenv("CLUSTER_TYPE", o.clusterType)
}

func (o *options) installTools() error {
	if o.deployerName == tailorbirdDeployerName {
		if err := tailorbird.InstallTools(o.clusterType); err != nil {
			return fmt.Errorf("")
		}
	}

	return nil
}

func (o *options) runKubetest2(deployerFlags, testFlags []string) error {
	switch o.deployerName {
	case gkeDeployerName:
		log.Println("Will run kubetest2 gke deployer to create the clusters...")

		extraFlags, err := gke.DeployerFlags(o.clusterTopology, o.featureToTest)
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
		var buf bytes.Buffer
		// Retry creating the cluster in different regions to reduce the test
		// flakniess caused by GCE_STOCKOUT and other recoverable GKE errors.
		// This is a temporary workaround before b/162609408 is solved upstream.
		// TODO(chizhg): remove the retry logic here after b/162609408 is solved
		//    upstream in kubetest2.
		for _, region := range []string{"us-central1", "us-west1", "us-east1"} {
			newKubetest2Flags := make([]string, len(kubetest2Flags))
			copy(newKubetest2Flags, kubetest2Flags)
			newKubetest2Flags = append(newKubetest2Flags, "--region="+region)
			newKubetest2Flags = append(newKubetest2Flags, "--test=exec", "--", o.testScript)
			newKubetest2Flags = append(newKubetest2Flags, testFlags...)
			if err := exec.Run(fmt.Sprintf("kubetest2 %s", strings.Join(newKubetest2Flags, " ")),
				exec.WithWorkingDir(o.kubetest2WorkingDir),
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
		if err := exec.Run(fmt.Sprintf("kubetest2 %s", strings.Join(kubetest2Flags, " ")), exec.WithWorkingDir(o.kubetest2WorkingDir)); err != nil {
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
