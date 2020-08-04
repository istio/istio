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

package settings

import (
	"bytes"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/galley/pkg/config/util/kuberesource"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/snapshots"
)

const (
	defaultMeshConfigFolder = "/etc/mesh-config/"
	defaultMeshConfigFile   = defaultMeshConfigFolder + "mesh"
)

// Args contains the startup arguments to instantiate Galley.
type Args struct { // nolint:maligned
	// The path to kube configuration file.
	KubeConfig string

	// List of namespaces watched, separated by comma; if not set, watch all namespaces.
	WatchedNamespaces string

	// resync period to be passed to the K8s machinery.
	ResyncPeriod time.Duration

	// ExcludedResourceKinds is a list of resource kinds for which no source events will be triggered.
	// DEPRECATED
	ExcludedResourceKinds []string

	// MeshConfigFile is the path for mesh config
	MeshConfigFile string

	// DNS Domain suffix to use while constructing Ingress based resources.
	DomainSuffix string

	// Enable service discovery / endpoint processing.
	EnableServiceDiscovery bool

	// Enable Config Analysis service, that will analyze and update CRD status. UseOldProcessor must be set to false.
	EnableConfigAnalysis bool

	Snapshots       []string
	TriggerSnapshot string
}

// DefaultArgs allocates an Args struct initialized with Galley's default configuration.
func DefaultArgs() *Args {
	return &Args{
		ResyncPeriod:          0,
		KubeConfig:            "",
		WatchedNamespaces:     metav1.NamespaceAll,
		MeshConfigFile:        defaultMeshConfigFile,
		DomainSuffix:          constants.DefaultKubernetesDomain,
		ExcludedResourceKinds: kuberesource.DefaultExcludedResourceKinds(),
		EnableConfigAnalysis:  false,
		Snapshots:             []string{snapshots.Default},
		TriggerSnapshot:       snapshots.Default,
	}
}

// String produces a stringified version of the arguments for debugging.
func (a *Args) String() string {
	buf := &bytes.Buffer{}

	_, _ = fmt.Fprintf(buf, "KubeConfig: %s\n", a.KubeConfig)
	_, _ = fmt.Fprintf(buf, "WatchedNamespaces: %s\n", a.WatchedNamespaces)
	_, _ = fmt.Fprintf(buf, "ResyncPeriod: %v\n", a.ResyncPeriod)
	_, _ = fmt.Fprintf(buf, "MeshConfigFile: %s\n", a.MeshConfigFile)
	_, _ = fmt.Fprintf(buf, "DomainSuffix: %s\n", a.DomainSuffix)
	_, _ = fmt.Fprintf(buf, "ExcludedResourceKinds: %v\n", a.ExcludedResourceKinds)

	return buf.String()
}
