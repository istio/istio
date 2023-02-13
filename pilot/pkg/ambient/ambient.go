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

package ambient

type NodeType = string

const (
	// AnnotationType determines the type of workload this is.
	AnnotationType = "ambient.istio.io/type"
	// TypeMesh indicates this workload is in the mesh and has redirection to ztunnel.
	// This is added by the CNI when redirection is configured, not by the user.
	TypeMesh NodeType = "mesh"
	// TypeDisabled indicates this workload does NOT have redirection enabled. Workloads without any
	// value should be assumed to be "disabled" as well, but explicitly setting this acts as an opt-out.
	TypeDisabled NodeType = "disabled"
	// TypeZtunnel indicates this workload is a ztunnel.
	TypeZtunnel NodeType = "ztunnel"
	// TypeWaypoint indicates this workload is a waypoint.
	TypeWaypoint NodeType = "waypoint"
)
