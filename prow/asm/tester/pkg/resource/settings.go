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
	"fmt"

	"go.uber.org/multierr"
	"k8s.io/apimachinery/pkg/util/sets"
)

type Settings struct {
	// UNMANAGED or MANAGED
	ControlPlane string

	// Certificate Authority to use, can be one of CITADEL, MESHCA or PRIVATECA
	CA string

	// Workload Identity Pool, can be one of GKE or HUB
	WIP string

	// Path to the revision config file (see revision-deployer/README.md)
	RevisionConfig string

	// Test target for the make command to run the tests, e.g. test.integration.asm.security
	TestTarget string

	// Test to disable
	DisabledTests string

	VMSettings
}

type VMSettings struct {
	// Whether to use VM in the control plane setup
	UseVMs bool

	// A directory in echo-vm-provisioner/configs that contains config files for
	// provisioning the VM test environment
	VMStaticConfigDir string

	// VM image family. This will be used as the `--image-family` flag value
	// when using `gcloud compute instance-templates create` to create the VMs.
	VMImageFamily string

	// VM image project. This will be used as the `--image-project` flag value
	// when using `gcloud compute instance-templates create` to create the VMs.
	VMImageProject string
}

func ValidateSettings(settings *Settings) error {
	var errs []error
	if !isValid(settings.ControlPlane, validControlPlaneTypes) {
		errs = append(errs, fmt.Errorf("%q is not a valid control plane type %v", settings.ControlPlane, validControlPlaneTypes.List()))
	}
	if !isValid(settings.CA, validCATypes) {
		errs = append(errs, fmt.Errorf("%q is not a valid CA type in %v", settings.CA, validCATypes.List()))
	}
	if !isValid(settings.WIP, validWIPTypes) {
		errs = append(errs, fmt.Errorf("%q is not a valid WIP type in %v", settings.WIP, validWIPTypes.List()))
	}

	return multierr.Combine(errs...)
}

func isValid(val string, validVals sets.String) bool {
	return validVals.Has(val)
}
