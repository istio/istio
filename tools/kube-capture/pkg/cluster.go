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

package cluster

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type ResourceType int

const (
	Namespace ResourceType = iota
	Deployment
	Pod
	Label
	Annotation
	Container
)

// Cluster defines a tree of cluster resource names.
type Resources struct {
	// Root is the first level in the cluster resource hierarchy.
	// Each level in the hierarchy is a map[string]interface{} to the next level.
	// The levels are: namespaces/deployments/pods/containers.
	Root map[string]interface{}
	// Labels maps a pod name to a map of labels key-values.
	Labels map[string]map[string]string
	// Annotations maps a pod name to a map of annotation key-values.
	Annotations map[string]map[string]string
}

// GetClusterResources returns cluster resources for the given REST config and k8s Clientset.
func GetClusterResources(*rest.Config, *kubernetes.Clientset) (*Resources, error) {
	// TODO: implement
	return nil, nil
}
