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

	"k8s.io/apimachinery/pkg/util/sets"
)

// ClusterType is the type of the clusters used for running the tests
type ClusterType string

const (
	GKEOnGCP ClusterType = "gke"
	OnPrem   ClusterType = "gke-on-prem"
	// TODO: update to "gke-on-aws"
	GKEOnAWS  ClusterType = "aws"
	BareMetal ClusterType = "bare-metal"
)

// Set converts the value string to ClusterType
func (ct *ClusterType) Set(value string) error {
	*ct = ClusterType(value)
	return nil
}

func (ct *ClusterType) String() string { return string(*ct) }

func (ct *ClusterType) Type() string { return "cluster_type" }

// ClusterTopology is the topology of the clusters
type ClusterToplology string

const (
	MultiProject              ClusterToplology = "mp"
	MultiCluster              ClusterToplology = "mc"
	SingleCluster             ClusterToplology = "sc"
	MultiClusterMultiNetwork  ClusterToplology = "mcmn"
	MultiClusterSingleNetwork ClusterToplology = "mcsn"
)

// Set converts the value string to ClusterToplology
func (ct *ClusterToplology) Set(value string) error {
	*ct = ClusterToplology(value)
	return nil
}

func (ct *ClusterToplology) String() string { return string(*ct) }

func (ct *ClusterToplology) Type() string { return "cluster_topology" }

// ControlPlaneType is the type of the ASM control plane
type ControlPlaneType string

const (
	Unmanaged ControlPlaneType = "UNMANAGED"
	Managed   ControlPlaneType = "MANAGED"
)

var validControlPlaneTypes = sets.NewString(string(Unmanaged), string(Managed))

// Set converts the value string to ControlPlaneType
func (cpt *ControlPlaneType) Set(value string) error {
	if !validControlPlaneTypes.Has(value) {
		return fmt.Errorf("%q is not a valid control plane type in %v", value, validControlPlaneTypes)
	}

	*cpt = ControlPlaneType(value)
	return nil
}

func (cpt *ControlPlaneType) String() string { return string(*cpt) }

func (cpt *ControlPlaneType) Type() string { return "control_plane" }

// CAType is the type of the Certificate Authority to use
type CAType string

const (
	Citadel   CAType = "CITADEL"
	MeshCA    CAType = "MESHCA"
	PrivateCA CAType = "PRIVATECA"
)

var validCATypes = sets.NewString(string(Citadel), string(MeshCA), string(PrivateCA))

// Set converts the value string to ControlPlaneType
func (ca *CAType) Set(value string) error {
	if !validCATypes.Has(value) {
		return fmt.Errorf("%q is not a valid CA type in %v", value, validCATypes)
	}

	*ca = CAType(value)
	return nil
}

func (ca *CAType) String() string { return string(*ca) }

func (ca *CAType) Type() string { return "ca" }

// WIPType is the type of the Workload Identity Pool to use
type WIPType string

const (
	GKEWorkloadIdentityPool WIPType = "GKE"
	HUBWorkloadIdentityPool WIPType = "HUB"
)

var validWIPTypes = sets.NewString(string(GKEWorkloadIdentityPool), string(HUBWorkloadIdentityPool))

// Set converts the value string to WIPType
func (wip *WIPType) Set(value string) error {
	if !validWIPTypes.Has(value) {
		return fmt.Errorf("%q is not a valid WIP type in %v", value, validWIPTypes)
	}

	*wip = WIPType(value)
	return nil
}

func (wip *WIPType) String() string { return string(*wip) }

func (wip *WIPType) Type() string { return "wip" }
