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

package kube

import "strings"

type GKEClusterSpec struct {
	Name      string
	ProjectID string
	Location  string
}

// GKEClusterSpecsFromContexts parses the GCP project IDs from the contexts.
// It will return an empty list if the current contexts are not for GKE-on-GCP.
func GKEClusterSpecsFromContexts(kubectlContexts string) []*GKEClusterSpec {
	contexts := strings.Split(kubectlContexts, ",")
	res := make([]*GKEClusterSpec, 0)
	for _, context := range contexts {
		cp := GKEClusterSpecFromContext(context)
		if cp != nil {
			res = append(res, cp)
		}
	}
	return res
}

// GKEClusterSpecFromContext parses a GKEClusterSpec struct from a kubecontext.
// It will return nil if the provided context is not for GKE-on-GCP.
func GKEClusterSpecFromContext(kubectlContext string) *GKEClusterSpec {
	parts := strings.Split(kubectlContext, "_")
	if len(parts) == 4 && parts[0] == "gke" {
		return &GKEClusterSpec{
			ProjectID: parts[1],
			Location:  parts[2],
			Name:      parts[3],
		}
	}
	return nil
}
