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

package resource

type Settings struct {
	// Root directory of the repository
	RepoRootDir string

	// A list of kubeconfig files that can be used to connnect to the test clusters
	Kubeconfig string

	// Type of the cluster
	ClusterType ClusterType

	// Topology of the cluster
	ClusterTopology ClusterToplology

	// The feature to test for this test flow
	FeatureToTest Feature

	// UNMANAGED or MANAGED
	ControlPlane ControlPlaneType

	// Certificate Authority to use, can be one of CITADEL, MESHCA or PRIVATECA
	CA CAType

	// Workload Identity Pool, can be one of GKE or HUB
	WIP WIPType

	// Path to the revision config file (see revision-deployer/README.md)
	RevisionConfig string

	// Test target for the make command to run the tests, e.g. test.integration.asm.security
	TestTarget string

	// Test to disable
	DisabledTests string

	VMSettings

	RuntimeSettings
}

type VMSettings struct {
	// Whether to use VM in the control plane setup
	UseVMs bool

	// A directory in echo-vm-provisioner/configs that contains config files for
	// provisioning the VM test environment
	VMStaticConfigDir string

	// If set, the Istio Go test framework will spin up GCE VMs based on the
	// configuration in the integration tests.
	UseGCEVMs bool

	// VM image family. This will be used as the `--image-family` flag value
	// when using `gcloud compute instance-templates create` to create the VMs.
	VMImageFamily string

	// VM image project. This will be used as the `--image-project` flag value
	// when using `gcloud compute instance-templates create` to create the VMs.
	VMImageProject string
}

// RuntimeSettings contains fields that are only populated and shared during the
// test runtime.
type RuntimeSettings struct {
	// The kubectl contexts string for the current test clusters.
	KubectlContexts string

	// The directory that stores configuration files for running the tests flows.
	ConfigDir string

	// A list of GCP projects for where the GKE clusters are created.
	// They can be used in the test flow for e.g. hosting the test images with the GCRs
	GCPProjects []string

	// The host GCP project when ASM is testing the multi-project profile.
	HostGCPProject string

	// The project for the GCR that will be used to host the test images
	GCRProject string
}
