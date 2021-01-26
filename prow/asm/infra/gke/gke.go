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

// SingleClusterFlags returns the kubetest2 flags for single-cluster setup.
func SingleClusterFlags() []string {
	flags := gkeDeployerBaseFlags()
	flags = append(flags, "--cluster-name=prow-test", "--machine-type=e2-standard-4", "--num-nodes=2", "--region=us-central1")
	flags = append(flags, "--network=default", "--release-channel=regular", "--version=latest", "--enable-workload-identity")

	return flags
}

// MultiClusterFlags returns the kubetest2 flags for single-project
// multi-cluster setup.
func MultiClusterFlags() []string {
	flags := gkeDeployerBaseFlags()
	flags = append(flags, "--cluster-name=prow-test1,prow-test2", "--machine-type=e2-standard-4", "--num-nodes=2", "--region=us-central1")
	flags = append(flags, "--network=default", "--release-channel=regular", "--version=latest", "--enable-workload-identity")

	return flags
}

// ExtraVPCSCClusterFlags returns the extra kubetest2 flags for creating the clusters for
// VPC-SC testing, as per the instructions in https://docs.google.com/document/d/11yYDxxI-fbbqlpvUYRtJiBmGdY_nIKPJLbssM3YQtKI/edit#heading=h.e2laig460f1d
func ExtraVPCSCClusterFlags() ([]string, error) {
	project, err := boskos.AcquireBoskosResource(vpcSCBoskosResource)
	if err != nil {
		return nil, fmt.Errorf("error acquiring the GCP project for testing with VPC-SC")
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

	flags := []string{"--project="+project}
	//  TODO: yonggangl@ tairan@ restrict the access to limited after the job is tested successfully
	flags = append(flags, "--private-cluster-access-level=unrestricted")
	flags = append(flags, "--private-cluster-master-ip-range=173.16.0.32/28,172.16.0.32/28")

	return flags, nil
}

// MultiProjectMultiClusterFlags returns the kubetest2 flags for multi-project
// multi-cluster setup.
func MultiProjectMultiClusterFlags() ([]string, error) {
	projects, err := acquireMultiGCPProjects()
	if err != nil {
		return nil, fmt.Errorf("error acquiring GCP projects for multi-project multi-cluster setup: %w", err)
	}

	flags := gkeDeployerBaseFlags()
	flags = append(flags, "--create-command='beta container clusters create --quiet'")
	flags = append(flags, "--cluster-name=prow-test1:1,prow-test2:2", "--machine-type=e2-standard-4", "--num-nodes=2", "--region=us-central1")
	flags = append(flags, "--network=test-network", "--subnetwork-ranges='172.16.4.0/22 172.16.16.0/20 172.20.0.0/14,10.0.4.0/22 10.0.32.0/20 10.4.0.0/14'")
	flags = append(flags, "--release-channel=regular", "--version=latest", "--enable-workload-identity")
	flags = append(flags, "--project="+projects)

	return flags, nil
}

// acquire GCP projects for multi-project multi-cluster setup.
// These projects are mananged by the boskos project rental pool as configured in
// https://gke-internal.googlesource.com/istio/test-infra-internal/+/refs/heads/master/boskos/config/resources.yaml#105
func acquireMultiGCPProjects() (string, error) {
	// Acquire a host project from the project rental pool.
	hostProject, err := boskos.AcquireBoskosResource(sharedVPCHostBoskosResource)
	if err != nil {
		return "", fmt.Errorf("error acquiring a host project: %w", err)
	}
	// Remove all projects that are currently associated with this host project.
	associatedProjects, err := exec.RunWithOutput(fmt.Sprintf("gcloud beta compute shared-vpc"+
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
		associatedHostProject, err := exec.RunWithOutput(fmt.Sprintf("gcloud beta compute shared-vpc"+
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
	os.Setenv("HOST_PROJECT", hostProject)

	return strings.Join(append([]string{hostProject}, serviceProjects...), ","), nil
}
