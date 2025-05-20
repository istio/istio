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

package config

import (
	"fmt"
	"strings"
)

type Config struct {
	InstallConfig InstallConfig
	RepairConfig  RepairConfig
}

// InstallConfig struct defines the Istio CNI installation options
type InstallConfig struct {
	// Location of the CNI config files in the container's filesystem (mount location of the CNINetDir)
	MountedCNINetDir string
	// Location of the node agent writable path on the node (used for sockets, etc)
	CNIAgentRunDir string
	// Name of the CNI config file
	CNIConfName string
	// Whether to install CNI plugin as a chained or standalone
	ChainedCNIPlugin bool

	// Logging level for the CNI plugin
	// Since it runs out-of-process, it has to be separately configured
	PluginLogLevel string
	// The file mode to set when creating the kubeconfig file
	KubeconfigMode int
	// CA file for kubeconfig
	KubeCAFile string
	// Whether to use insecure TLS in the kubeconfig file
	SkipTLSVerify bool

	// Comma-separated list of K8S namespaces that CNI should ignore
	ExcludeNamespaces string

	// Singular namespace that the istio CNI node agent resides in
	PodNamespace string

	// KUBERNETES_SERVICE_PROTOCOL
	K8sServiceProtocol string
	// KUBERNETES_SERVICE_HOST
	K8sServiceHost string
	// KUBERNETES_SERVICE_PORT
	K8sServicePort string
	// KUBERNETES_NODE_NAME
	K8sNodeName string
	// Path where service account secrets live, e.g. "/var/run/secrets/kubernetes.io/serviceaccount"
	// Tests may override.
	K8sServiceAccountPath string

	// Directory from where the CNI binaries should be copied
	CNIBinSourceDir string
	// Directories into which to copy the CNI binaries
	CNIBinTargetDirs []string

	// The HTTP port for monitoring
	MonitoringPort int

	// The ztunnel server socket address that the ztunnel will connect to.
	ZtunnelUDSAddress string

	// Whether ambient is enabled
	AmbientEnabled bool

	// The labelSelector to enable ambient for specific pods or namespaces
	AmbientEnablementSelector string

	// Whether ambient DNS capture is enabled
	AmbientDNSCapture bool

	// Whether ipv6 is enabled for ambient capture
	AmbientIPv6 bool

	// Feature flag to disable safe upgrade. Will be removed in future releases.
	AmbientDisableSafeUpgrade bool

	// Whether reconciliation of iptables at post startup is enabled for Ambient workloads
	AmbientReconcilePodRulesOnStartup bool
}

// RepairConfig struct defines the Istio CNI race repair configuration
type RepairConfig struct {
	// Whether to enable CNI race repair
	Enabled bool

	// The node name that the CNI DaemonSet runs on
	NodeName string

	// Key and value for broken pod label
	LabelKey   string
	LabelValue string

	// Whether to fix race condition by repairing them
	RepairPods bool

	// Whether to fix race condition by delete broken pods
	DeletePods bool

	// Whether to label broken pods
	LabelPods bool

	// Filters for race repair, including name of sidecar annotation, name of init container,
	// init container termination message and exit code.
	SidecarAnnotation  string
	InitContainerName  string
	InitTerminationMsg string
	InitExitCode       int

	// Label and field selectors to select pods managed by race repair.
	LabelSelectors string
	FieldSelectors string
}

func (c InstallConfig) String() string {
	var b strings.Builder
	b.WriteString("MountedCNINetDir: " + c.MountedCNINetDir + "\n")
	b.WriteString("CNIConfName: " + c.CNIConfName + "\n")
	b.WriteString("ChainedCNIPlugin: " + fmt.Sprint(c.ChainedCNIPlugin) + "\n")
	b.WriteString("CNIAgentRunDir: " + fmt.Sprint(c.CNIAgentRunDir) + "\n")

	b.WriteString("PluginLogLevel: " + c.PluginLogLevel + "\n")
	b.WriteString("KubeconfigMode: " + fmt.Sprintf("%#o", c.KubeconfigMode) + "\n")
	b.WriteString("KubeCAFile: " + c.KubeCAFile + "\n")
	b.WriteString("SkipTLSVerify: " + fmt.Sprint(c.SkipTLSVerify) + "\n")

	b.WriteString("ExcludeNamespaces: " + fmt.Sprint(c.ExcludeNamespaces) + "\n")
	b.WriteString("PodNamespace: " + fmt.Sprint(c.PodNamespace) + "\n")
	b.WriteString("K8sServiceProtocol: " + c.K8sServiceProtocol + "\n")
	b.WriteString("K8sServiceHost: " + c.K8sServiceHost + "\n")
	b.WriteString("K8sServicePort: " + fmt.Sprint(c.K8sServicePort) + "\n")
	b.WriteString("K8sNodeName: " + c.K8sNodeName + "\n")

	b.WriteString("CNIBinSourceDir: " + c.CNIBinSourceDir + "\n")
	b.WriteString("CNIBinTargetDirs: " + strings.Join(c.CNIBinTargetDirs, ",") + "\n")

	b.WriteString("MonitoringPort: " + fmt.Sprint(c.MonitoringPort) + "\n")
	b.WriteString("ZtunnelUDSAddress: " + fmt.Sprint(c.ZtunnelUDSAddress) + "\n")

	b.WriteString("AmbientEnabled: " + fmt.Sprint(c.AmbientEnabled) + "\n")
	b.WriteString("AmbientEnablementSelector: " + c.AmbientEnablementSelector + "\n")
	b.WriteString("AmbientDNSCapture: " + fmt.Sprint(c.AmbientDNSCapture) + "\n")
	b.WriteString("AmbientIPv6: " + fmt.Sprint(c.AmbientIPv6) + "\n")
	b.WriteString("AmbientDisableSafeUpgrade: " + fmt.Sprint(c.AmbientDisableSafeUpgrade) + "\n")
	b.WriteString("AmbientReconcilePodRulesOnStartup: " + fmt.Sprint(c.AmbientReconcilePodRulesOnStartup) + "\n")
	return b.String()
}

func (c RepairConfig) String() string {
	var b strings.Builder
	b.WriteString("Enabled: " + fmt.Sprint(c.Enabled) + "\n")
	b.WriteString("NodeName: " + c.NodeName + "\n")
	b.WriteString("LabelKey: " + c.LabelKey + "\n")
	b.WriteString("LabelValue: " + c.LabelValue + "\n")
	b.WriteString("DeletePods: " + fmt.Sprint(c.DeletePods) + "\n")
	b.WriteString("LabelPods: " + fmt.Sprint(c.LabelPods) + "\n")
	b.WriteString("SidecarAnnotation: " + c.SidecarAnnotation + "\n")
	b.WriteString("InitContainerName: " + c.InitContainerName + "\n")
	b.WriteString("InitTerminationMsg: " + c.InitTerminationMsg + "\n")
	b.WriteString("InitExitCode: " + fmt.Sprint(c.InitExitCode) + "\n")
	b.WriteString("LabelSelectors: " + c.LabelSelectors + "\n")
	b.WriteString("FieldSelectors: " + c.FieldSelectors + "\n")
	return b.String()
}
