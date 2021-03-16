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

import (
	"github.com/spf13/pflag"
)

func BindFlags(settings *Settings) *pflag.FlagSet {
	flags := pflag.NewFlagSet("asm pipeline tester", pflag.ExitOnError)
	flags.StringVar(&settings.ControlPlane, "control-plane", "UNMANAGED", "type of the control plane, can be one of UNMANAGED or MANAGED")
	flags.StringVar(&settings.CA, "ca", "MESHCA", "Certificate Authority to use, can be one of CITADEL, MESHCA or PRIVATECA")
	flags.StringVar(&settings.WIP, "wip", "GKE", "Workload Identity Pool, can be one of GKE or HUB")
	flags.StringVar(&settings.RevisionConfig, "revision-config", "", "path to the revision config file (see revision-deployer/README.md)")
	flags.StringVar(&settings.TestTarget, "test", "test.integration.multicluster.kube.presubmit", "test target for the make command to run the tests, e.g. test.integration.asm.security")
	flags.StringVar(&settings.DisabledTests, "disabled-tests", "", "tests to disable, should be a regex that matches the test and test suite names")

	flags.BoolVar(&settings.UseVMs, "vm", false, "whether to use VM in the control plane setup")
	flags.StringVar(&settings.VMStaticConfigDir, "vm-static-config-dir", "", "a directory in echo-vm-provisioner/configs that contains config files for provisioning the VM test environment")
	// TODO(chizhg): delete after we update the Prow jobs to use --vm-static-config-dir
	flags.StringVar(&settings.VMStaticConfigDir, "static-vms", "", "a directory in echo-vm-provisioner/configs that contains config files for provisioning the VM test environment")
	flags.StringVar(&settings.VMImageFamily, "vm-image-family", "debian-10", "VM distribution that will be used as the `--image-family` flag value when using `gcloud compute instance-templates create` to create the VMs.")
	// TODO(chizhg): delete after we update the Prow jobs to use --vm-image-family
	flags.StringVar(&settings.VMImageFamily, "vm-distro", "debian-10", "VM distribution that will be used as the `--image-family` flag value when using `gcloud compute instance-templates create` to create the VMs.")
	flags.StringVar(&settings.VMImageProject, "vm-image-project", "debian-cloud", "VM image project that will be used as the `--image-project` flag value when using `gcloud compute instance-templates create` to create the VMs.")
	// TODO(chizhg): delete after we update the Prow jobs to use --vm-image-project
	flags.StringVar(&settings.VMImageProject, "image-project", "debian-cloud", "VM image project that will be used as the `--image-project` flag value when using `gcloud compute instance-templates create` to create the VMs.")

	return flags
}
