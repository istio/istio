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

package gke

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"istio.io/istio/prow/asm/infra/boskos"
	"istio.io/istio/prow/asm/infra/exec"
)

const (
	// These names correspond to the resources configured in
	// https://gke-internal.googlesource.com/istio/test-infra-internal/+/refs/heads/master/boskos/config/resources.yaml
	sharedVPCHostBoskosResource = "shared-vpc-host-gke-project"
	sharedVPCSVCBoskosResource  = "shared-vpc-svc-gke-project"
	vpcSCBoskosResource         = "vpc-sc-gke-project"
	commonBoskosResource        = "gke-project"
)

func gkeDeployerBaseFlags() []string {
	return []string{"--ignore-gcp-ssh-key=true", "-v=2", "--gcp-service-account=" + os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")}
}

// DeployerFlags returns the deployer flags needed for the given cluster
// topology and the feature to test
func DeployerFlags(clusterTopology, featureToTest string) ([]string, error) {
	var flags []string
	var err error
	switch clusterTopology {
	case "SINGLECLUSTER", "sc":
		flags, err = singleClusterFlags()
	case "MULTICLUSTER", "mc":
		boskosResourceType := commonBoskosResource
		// Testing with VPC-SC requires a different project type.
		if featureToTest == "VPC_SC" {
			boskosResourceType = vpcSCBoskosResource
		}
		flags, err = multiClusterFlags(boskosResourceType)
	case "MULTIPROJECT_MULTICLUSTER", "mp":
		flags, err = multiProjectMultiClusterFlags()
	default:
		err = fmt.Errorf("cluster topology %q is not supported", clusterTopology)
	}

	if featureToTest != "" {
		var featureFlags []string
		switch featureToTest {
		case "VPC_SC":
			featureFlags, err = featureVPCSCClusterFlags(flags)
		default:
			err = fmt.Errorf("feature %q is not supported", featureToTest)
		}
		flags = append(flags, featureFlags...)
	}

	return flags, err
}

// singleClusterFlags returns the kubetest2 flags for single-cluster setup.
func singleClusterFlags() ([]string, error) {
	flags := gkeDeployerBaseFlags()

	if err := configureProjectFlag(&flags, func() (string, error) {
		return acquireBoskosProjectAndSetBilling(commonBoskosResource)
	}); err != nil {
		return nil, fmt.Errorf("error acquiring GCP projects for singlecluster setup: %w", err)
	}
	flags = append(flags, "--cluster-name=prow-test", "--machine-type=e2-standard-4", "--num-nodes=2")
	flags = append(flags, "--network=default", "--release-channel=regular", "--version=latest", "--enable-workload-identity")
	// TODO(chizhg): uncomment after b/162609408 is fixed upstream
	// flags = append(flags, "--region=us-central1")

	return flags, nil
}

// multiClusterFlags returns the kubetest2 flags for single-project
// multi-cluster setup.
func multiClusterFlags(boskosResourceType string) ([]string, error) {
	flags := gkeDeployerBaseFlags()

	if err := configureProjectFlag(&flags, func() (string, error) {
		return acquireBoskosProjectAndSetBilling(boskosResourceType)
	}); err != nil {
		return nil, fmt.Errorf("error acquiring GCP projects for multicluster setup: %w", err)
	}
	flags = append(flags, "--cluster-name=prow-test1,prow-test2", "--machine-type=e2-standard-4", "--num-nodes=2")
	flags = append(flags, "--network=default", "--release-channel=regular", "--version=latest", "--enable-workload-identity")
	// TODO(chizhg): uncomment after b/162609408 is fixed upstream
	// flags = append(flags, "--region=us-central1")

	return flags, nil
}

// featureVPCSCClusterFlags returns the extra kubetest2 flags for creating the clusters for
// VPC-SC testing, as per the instructions in https://docs.google.com/document/d/11yYDxxI-fbbqlpvUYRtJiBmGdY_nIKPJLbssM3YQtKI/edit#heading=h.e2laig460f1d
func featureVPCSCClusterFlags(existingFlags []string) ([]string, error) {
	// Parse the project ID from existing flags.
	var project string
	for _, flag := range existingFlags {
		if strings.HasPrefix(flag, "--project=") {
			project = strings.TrimLeft(flag, "--project=")
		}
	}
	if project == "" {
		return nil, errors.New("project is not provided, cannot configure cluster flags for VPC-SC testing")
	}

	// Create the route as per the user guide above.
	// Currently only the route needs to be recreated since only it will be cleaned
	// up by Boskos janitor.
	// TODO(chizhg): create everything else from scratch here after we are able to
	// use Boskos janitor to clean them up as well, as per the long-term plan in go/asm-vpc-sc-testing-plan
	if err := exec.Run("gcloud compute routes create restricted-vip" +
		" --network=default --destination-range=199.36.153.4/30" +
		" --next-hop-gateway=default-internet-gateway --project=" + project); err != nil {
		return nil, fmt.Errorf("error creating the route in the default network to restricted.googleapis.com")
	}

	//  TODO: yonggangl@ tairan@ restrict the access to limited after the job is tested successfully
	flags := []string{"--private-cluster-access-level=unrestricted", "--private-cluster-master-ip-range=173.16.0.32/28,172.16.0.32/28"}
	return flags, nil
}

// multiProjectMultiClusterFlags returns the kubetest2 flags for multi-project
// multi-cluster setup.
func multiProjectMultiClusterFlags() ([]string, error) {
	flags := gkeDeployerBaseFlags()

	if err := configureProjectFlag(&flags, acquireMultiGCPProjects); err != nil {
		return nil, fmt.Errorf("error acquiring GCP projects for multi-project multi-cluster setup: %w", err)
	}
	flags = append(flags, "--create-command='beta container clusters create --quiet'")
	flags = append(flags, "--cluster-name=prow-test1:1,prow-test2:2", "--machine-type=e2-standard-4", "--num-nodes=2")
	flags = append(flags, "--network=test-network", "--subnetwork-ranges='172.16.4.0/22 172.16.16.0/20 172.20.0.0/14,10.0.4.0/22 10.0.32.0/20 10.4.0.0/14'")
	flags = append(flags, "--release-channel=regular", "--version=latest", "--enable-workload-identity")
	// TODO(chizhg): uncomment after b/162609408 is fixed upstream
	// flags = append(flags, "--region=us-central1")

	return flags, nil
}

// configureProjectFlag configures the --project flag value for the kubetest2
// command to create GKE clusters.
func configureProjectFlag(baseFlags *[]string, acquireBoskosProject func() (string, error)) error {
	// Only acquire the GCP project from Boskos if it's running in CI.
	if os.Getenv("CI") == "true" {
		project, err := acquireBoskosProject()
		if err != nil {
			return fmt.Errorf("error acquiring GCP projects from Boskos: %w", err)
		}

		*baseFlags = append(*baseFlags, "--project="+project)
	}
	return nil
}

// acquire GCP projects for multi-project multi-cluster setup.
// These projects are mananged by the boskos project rental pool as configured in
// https://gke-internal.googlesource.com/istio/test-infra-internal/+/refs/heads/master/boskos/config/resources.yaml#105
func acquireMultiGCPProjects() (string, error) {
	// Acquire a host project from the project rental pool and set it as the
	// billing project.
	hostProject, err := acquireBoskosProjectAndSetBilling(sharedVPCHostBoskosResource)
	if err != nil {
		return "", fmt.Errorf("error acquiring a host project: %w", err)
	}
	// Remove all projects that are currently associated with this host project.
	associatedProjects, err := exec.Output(fmt.Sprintf("gcloud beta compute shared-vpc"+
		" associated-projects list %s --format=value(RESOURCE_ID)", hostProject))
	if err != nil {
		return "", fmt.Errorf("error getting the associated projects for %q: %w", hostProject, err)
	}
	associatedProjectsStr := strings.TrimSpace(string(associatedProjects))
	if associatedProjectsStr != "" {
		for _, project := range strings.Split(associatedProjectsStr, "\n") {
			// Sometimes this command returns error like below:
			// 	ERROR: (gcloud.beta.compute.shared-vpc.associated-projects.remove) Could not disable resource [asm-boskos-shared-vpc-svc-144] as an associated resource for project [asm-boskos-shared-vpc-host-69]:
			//    - Invalid resource usage: 'The resource 'projects/asm-boskos-shared-vpc-svc-144' is not linked to shared VPC host 'projects/asm-boskos-shared-vpc-host-69'.'.
			// but it's uncertain why this happens. Ignore the error for now
			// since the error says the project has already been dissociated.
			// TODO(chizhg): enable the error check after figuring out the cause.
			exec.Run(fmt.Sprintf("gcloud beta compute shared-vpc"+
				" associated-projects remove %s --host-project=%s", project, hostProject))
		}
	}

	// Acquire two service projects from the project rental pool.
	serviceProjects := make([]string, 2)
	for i := 0; i < len(serviceProjects); i++ {
		sp, err := boskos.AcquireBoskosResource(sharedVPCSVCBoskosResource)
		if err != nil {
			return "", fmt.Errorf("error acquiring a service project: %w", err)
		}
		serviceProjects[i] = sp
	}
	// gcloud requires one service project can only be associated with one host
	// project, so if the acquired service projects have already been associated
	// with one host project, remove the association.
	for _, sp := range serviceProjects {
		associatedHostProject, err := exec.Output(fmt.Sprintf("gcloud beta compute shared-vpc"+
			" get-host-project %s --format=value(name)", sp))
		if err != nil {
			return "", fmt.Errorf("error getting the associated host project for %q: %w", sp, err)
		}
		associatedHostProjectStr := strings.TrimSpace(string(associatedHostProject))
		if associatedHostProjectStr != "" {
			// TODO(chizhg): enable the error check after figuring out the cause.
			exec.Run(fmt.Sprintf("gcloud beta compute shared-vpc"+
				" associated-projects remove %s --host-project=%s", sp, associatedHostProjectStr))
		}
	}
	// HOST_PROJECT is needed for the firewall setup after cluster creations.
	// TODO(chizhg): remove it after we convert all the bash script into Go.
	os.Setenv("HOST_PROJECT", hostProject)

	return strings.Join(append([]string{hostProject}, serviceProjects...), ","), nil
}

func acquireBoskosProjectAndSetBilling(projectType string) (string, error) {
	project, err := boskos.AcquireBoskosResource(projectType)
	if err != nil {
		return "", fmt.Errorf("error acquiring a project with type %q: %w", projectType, err)
	}
	if err = exec.Run("gcloud config set billing/quota_project " + project); err != nil {
		return "", fmt.Errorf("error setting billing/quota_project to %q: %w", project, err)
	}

	return project, nil
}
