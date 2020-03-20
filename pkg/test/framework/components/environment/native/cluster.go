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

package native

import (
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	// Cluster used for the native environment.
	Cluster = cluster{}
)

var _ resource.Cluster = cluster{}

type cluster struct{}

func (c cluster) String() string {
	return "nativeCluster"
}

func (c cluster) Index() resource.ClusterIndex {
	// Multicluster not supported natively.
	return 0
}
