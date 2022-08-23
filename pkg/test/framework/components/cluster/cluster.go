//  Copyright Istio Authors
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

package cluster

import (
	"fmt"

	"istio.io/istio/pkg/kube"
)

// Cluster in a multicluster environment.
type Cluster interface {
	fmt.Stringer
	kube.CLIClient

	// Name of this cluster. Use for interacting with the cluster or validation against clusters.
	// Use StableName instead of Name when creating subtests.
	Name() string

	// StableName gives a deterministic name for the cluster. Use this for test/subtest names to
	// allow test grid to compare runs, even when the underlying cluster names are dynamic.
	// Use Name for validation/interaction with the actual cluster.
	StableName() string

	// Kind of cluster
	Kind() Kind

	// NetworkName the cluster is on
	NetworkName() string

	// MinKubeVersion returns true if the cluster is at least the version specified,
	// false otherwise
	MinKubeVersion(minor uint) bool

	// MaxKubeVersion returns true if the cluster is at most the version specified,
	// false otherwise
	MaxKubeVersion(minor uint) bool

	// IsPrimary returns true if this is a primary cluster, containing an instance
	// of the Istio control plane.
	IsPrimary() bool

	// IsConfig returns true if this is a config cluster, used as the source of
	// Istio config for one or more control planes.
	IsConfig() bool

	// IsRemote returns true if this is a remote cluster, which uses a control plane
	// residing in another cluster.
	IsRemote() bool

	// IsExternalControlPlane returns true if this is a cluster containing an instance
	// of the Istio control plane but with its source of config in another cluster.
	IsExternalControlPlane() bool

	// Primary returns the primary cluster for this cluster. Will return itself if
	// IsPrimary.
	Primary() Cluster

	// PrimaryName returns the name of the primary cluster for this cluster.
	PrimaryName() string

	// Config returns the config cluster for this cluster. Will return itself if
	// IsConfig.
	Config() Cluster

	// ConfigName returns the name of the config cluster for this cluster.
	ConfigName() string

	// HTTPProxy returns the HTTP proxy config to connect to the cluster
	HTTPProxy() string

	// Metadata returns the value for a given metadata key for the cluster.
	// If the key is not found in the cluster metadata, an empty string is returned.
	MetadataValue(key string) string

	// ProxyKubectlOnly returns a boolean value to indicate whether all traffic
	// should route through the HTTP proxy or only Kubectl traffic. (Useful
	// in topologies where the API server is private but the ingress is public).
	ProxyKubectlOnly() bool
}
