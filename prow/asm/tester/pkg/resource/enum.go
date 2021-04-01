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

import "k8s.io/apimachinery/pkg/util/sets"

// ClusterType is the type of the clusters used for running the tests
type ClusterType string

const (
	GKEOnGCP ClusterType = "gke"
	OnPrem   ClusterType = "gke-on-prem"
	// TODO: update to "gke-on-aws"
	GKEOnAWS  ClusterType = "aws"
	BareMetal ClusterType = "bare-metal"
)

// ClusterTopology is the topology of the clusters
type ClusterToplology string

const (
	MultiProject              ClusterToplology = "mp"
	MultiCluster              ClusterToplology = "mc"
	SingleCluster             ClusterToplology = "sc"
	MultiClusterMultiNetwork  ClusterToplology = "mcmn"
	MultiClusterSingleNetwork ClusterToplology = "mcsn"
)

// ControlPlaneType is the type of the ASM control plane
type ControlPlaneType string

const (
	Unmanaged ControlPlaneType = "UNMANAGED"
	Managed   ControlPlaneType = "MANAGED"
)

var validControlPlaneTypes = sets.NewString(string(Unmanaged), string(Managed))

// CAType is the type of the Certificate Authority to use
type CAType string

const (
	Citadel   CAType = "CITADEL"
	MeshCA    CAType = "MESHCA"
	PrivateCA CAType = "PRIVATECA"
)

var validCATypes = sets.NewString(string(Citadel), string(MeshCA), string(PrivateCA))

// WIPType is the type of the Workload Identity Pool to use
type WIPType string

const (
	GKE WIPType = "GKE"
	HUB WIPType = "HUB"
)

var validWIPTypes = sets.NewString(string(GKE), string(HUB))
