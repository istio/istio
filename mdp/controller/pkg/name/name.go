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

package name

const (
	MDPNamespace = "mdp-system"
)

const (
	IstioRevisionLabel = "istio.io/rev"
)

const (
	// IstioSystemNamespace is the default Istio system namespace.
	IstioSystemNamespace string = "istio-system"

	// KubeSystemNamespace is the system namespace where we place kubernetes system components.
	KubeSystemNamespace string = "kube-system"

	// KubePublicNamespace is the namespace where we place kubernetes public info (ConfigMaps).
	KubePublicNamespace string = "kube-public"

	// KubeNodeLeaseNamespace is the namespace for the lease objects associated with each kubernetes node.
	KubeNodeLeaseNamespace string = "kube-node-lease"
)

var systemNamespaces = map[string]struct{}{
	IstioSystemNamespace:   {},
	KubeSystemNamespace:    {},
	KubePublicNamespace:    {},
	KubeNodeLeaseNamespace: {},
}

func IsSystemNamespace(ns string) bool {
	_, ok := systemNamespaces[ns]
	return ok
}
