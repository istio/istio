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

package manifest

// Names not found in the istio gvk package
const (
	ClusterRole                 = "ClusterRole"
	ClusterRoleBinding          = "ClusterRoleBinding"
	HorizontalPodAutoscaler     = "HorizontalPodAutoscaler"
	NetworkAttachmentDefinition = "NetworkAttachmentDefinition"
	PodDisruptionBudget         = "PodDisruptionBudget"
	Role                        = "Role"
	RoleBinding                 = "RoleBinding"
)

const (
	// OwningResourceName represents the name of the owner to which the resource relates
	OwningResourceName = "install.operator.istio.io/owning-resource"
	// OwningResourceNamespace represents the namespace of the owner to which the resource relates
	OwningResourceNamespace = "install.operator.istio.io/owning-resource-namespace"
	// OwningResourceNotPruned indicates that the resource should not be pruned during reconciliation cycles,
	// note this will not prevent the resource from being deleted if the owning resource is deleted.
	OwningResourceNotPruned = "install.operator.istio.io/owning-resource-not-pruned"
	// OperatorManagedLabel indicates Istio operator is managing this resource.
	OperatorManagedLabel = "operator.istio.io/managed"
	// IstioComponentLabel indicates which Istio component a resource belongs to.
	IstioComponentLabel = "operator.istio.io/component"
	// OperatorVersionLabel indicates the Istio version of the installation.
	OperatorVersionLabel = "operator.istio.io/version"
)
