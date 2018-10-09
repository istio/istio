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

package deployment

import kubeCore "k8s.io/api/core/v1"

// ReplicaSettings for a deployment.
type ReplicaSettings struct {
	ReplicaCount uint
	AutoscaleMin uint
	AutoscaleMax uint
}

// ImageSettings for docker images.
type ImageSettings struct {
	Hub             string
	Tag             string
	ImagePullPolicy kubeCore.PullPolicy
}

// Settings for deploying Istio.
type Settings struct {
	// KubeConfig is the kube configuration file to use when calling kubectl.
	KubeConfig string

	// WorkDir is an output folder for storing intermediate artifacts (i.e. generated yaml etc.)
	WorkDir string

	EnableCoreDump    bool
	GlobalMtlsEnabled bool
	GalleyEnabled     bool

	// Namespace is the target deployment namespace (i.e. "istio-system").
	Namespace string

	Images         ImageSettings
	IngressGateway ReplicaSettings
}
