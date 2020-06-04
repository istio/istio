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

	"google.golang.org/grpc"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"istio.io/pkg/ctrlz"

	"istio.io/istio/galley/pkg/config/util/kuberesource"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/snapshots"
	"istio.io/istio/pkg/webhooks/validation/controller"
	"istio.io/istio/pkg/webhooks/validation/server"
)

const (
	defaultProbeCheckInterval     = 2 * time.Second
	defaultLivenessProbeFilePath  = "/healthLiveness"
	defaultReadinessProbeFilePath = "/healthReadiness"

	defaultConfigMapFolder  = "/etc/config/"
	defaultMeshConfigFolder = "/etc/mesh-config/"
	defaultAccessListFile   = defaultConfigMapFolder + "accesslist.yaml"
	defaultMeshConfigFile   = defaultMeshConfigFolder + "mesh"
)

// Args contains the startup arguments to instantiate Galley.
type Args struct { // nolint:maligned
	// The path to kube configuration file.
	KubeConfig string

	// KubeInterface has an already created K8S interface, will be reused instead of creating a new one
	KubeInterface *kubernetes.Clientset

	// List of namespaces watched, separated by comma; if not set, watch all namespaces.
	WatchedNamespaces string

	// InsecureGRPC is an existing GRPC server, will be used by Galley instead of creating its own
	InsecureGRPC *grpc.Server

	// SecureGRPC is an existing GRPC server, will be used by Galley instead of creating its own
	SecureGRPC *grpc.Server

	// KubeRestConfig has a rest config, common with other components
	KubeRestConfig *rest.Config

	// resync period to be passed to the K8s machinery.
	ResyncPeriod time.Duration

	// Initial Connection Window Size for gRPC connections
	InitialConnectionWindowSize uint

	// The introspection options to use
	IntrospectionOptions *ctrlz.Options

	// ConfigPath is the path for Galley specific config files
	ConfigPath string

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

	// DisableResourceReadyCheck disables the CRD readiness check. This
	// allows Galley to start when not all supported CRD are
	// registered with the kube-apiserver.
	// DEPRECATED
	DisableResourceReadyCheck bool

	// WatchConfigFiles if set to true, enables Fsnotify watcher for watching and signaling config file changes.
	// Default is false
	WatchConfigFiles bool

	// Enable the validating webhook server.
	EnableValidationServer bool

	// Enable a controller to manage the lifecycle of the validatingwebhookconfiguration.
	EnableValidationController bool

	ValidationWebhookServerArgs     server.Options
	ValidationWebhookControllerArgs controller.Options

	Snapshots       []string
	TriggerSnapshot string
}

// DefaultArgs allocates an Args struct initialized with Galley's default configuration.
func DefaultArgs() *Args {
	return &Args{
		ResyncPeriod:                    0,
		KubeConfig:                      "",
		WatchedNamespaces:               metav1.NamespaceAll,
		InitialConnectionWindowSize:     1024 * 1024 * 16,
		IntrospectionOptions:            ctrlz.DefaultOptions(),
		MeshConfigFile:                  defaultMeshConfigFile,
		ConfigPath:                      "",
		DomainSuffix:                    constants.DefaultKubernetesDomain,
		ExcludedResourceKinds:           kuberesource.DefaultExcludedResourceKinds(),
		ValidationWebhookServerArgs:     server.DefaultArgs(),
		ValidationWebhookControllerArgs: controller.DefaultArgs(),
		EnableValidationController:      true,
		EnableValidationServer:          true,
		WatchConfigFiles:                false,
		EnableConfigAnalysis:            false,
		Snapshots:       []string{snapshots.Default},
		TriggerSnapshot: snapshots.Default,
	}
}

// String produces a stringified version of the arguments for debugging.
func (a *Args) String() string {
	buf := &bytes.Buffer{}

	_, _ = fmt.Fprintf(buf, "KubeConfig: %s\n", a.KubeConfig)
	_, _ = fmt.Fprintf(buf, "WatchedNamespaces: %s\n", a.WatchedNamespaces)
	_, _ = fmt.Fprintf(buf, "ResyncPeriod: %v\n", a.ResyncPeriod)
	_, _ = fmt.Fprintf(buf, "InitialConnectionWindowSize: %v\n", a.InitialConnectionWindowSize)
	_, _ = fmt.Fprintf(buf, "IntrospectionOptions: %+v\n", *a.IntrospectionOptions)
	_, _ = fmt.Fprintf(buf, "ConfigFilePath: %s\n", a.ConfigPath)
	_, _ = fmt.Fprintf(buf, "MeshConfigFile: %s\n", a.MeshConfigFile)
	_, _ = fmt.Fprintf(buf, "DomainSuffix: %s\n", a.DomainSuffix)
	_, _ = fmt.Fprintf(buf, "ExcludedResourceKinds: %v\n", a.ExcludedResourceKinds)

	return buf.String()
}
