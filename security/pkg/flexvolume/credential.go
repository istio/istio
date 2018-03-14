// Copyright 2018 Istio Authors
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

package flexvolume

// CredentialFileExtension of the credential file written by the driver.
const CredentialFileExtension = ".json"

// Credential of the workload published by the Flex volume driver
type Credential struct {
	// UID is the unique identifier for the workload.
	UID string
	// Workload is the name of the workload.
	Workload string
	// Namespace is the namespace of the workload.
	Namespace string
	// ServiceAccount is the service account of the workload.
	ServiceAccount string
}
