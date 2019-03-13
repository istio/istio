//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package kube

import (
	"fmt"
)

const (
	// HubValuesKey values key for the Docker image hub.
	HubValuesKey = "global.hub"

	// TagValuesKey values key for the Docker image tag.
	TagValuesKey = "global.tag"

	// ImagePullPolicyValuesKey values key for the Docker image pull policy.
	ImagePullPolicyValuesKey = "global.imagePullPolicy"

	// LatestTag value
	LatestTag = "latest"
)

// Settings provide kube-specific Settings from flags.
type Settings struct {
	// Path to kube config file. Required if the environment is kubernetes.
	KubeConfig string

	// Indicates that the Ingress Gateway is not available. This typically happens in Minikube. The Ingress
	// component will fall back to node-port in this case.
	MinikubeIngress bool

	// Hub value to use in Helm templates
	Hub string

	// Tag value to use in Helm templates
	Tag string
}

func (s *Settings) clone() *Settings {
	c := *s
	return &c
}

// String implements fmt.Stringer
func (s *Settings) String() string {
	result := ""

	result += fmt.Sprintf("KubeConfig:      %s\n", s.KubeConfig)

	return result
}
