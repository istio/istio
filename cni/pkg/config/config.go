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
	// Location of the CNI config files in the host's filesystem
	CNINetDir string
	// Location of the CNI config files in the container's filesystem (mount location of the CNINetDir)
	MountedCNINetDir string
	// Name of the CNI config file
	CNIConfName string
	// Whether to install CNI plugin as a chained or standalone
	ChainedCNIPlugin bool

	// CNI config template file
	CNINetworkConfigFile string
	// CNI config template string
	CNINetworkConfig string
	// Whether to install CNI configuration and binary files
	CNIEnableInstall bool
	// Whether to reinstall CNI configuration and binary files
	CNIEnableReinstall bool

	// Logging level
	LogLevel string
	// Name of the kubeconfig file used by the CNI plugin
	KubeconfigFilename string
	// The file mode to set when creating the kubeconfig file
	KubeconfigMode int
	// CA file for kubeconfig
	KubeCAFile string
	// Whether to use insecure TLS in the kubeconfig file
	SkipTLSVerify bool

	// KUBERNETES_SERVICE_PROTOCOL
	K8sServiceProtocol string
	// KUBERNETES_SERVICE_HOST
	K8sServiceHost string
	// KUBERNETES_SERVICE_PORT
	K8sServicePort string
	// KUBERNETES_NODE_NAME
	K8sNodeName string

	// Directory from where the CNI binaries should be copied
	CNIBinSourceDir string
	// Directories into which to copy the CNI binaries
	CNIBinTargetDirs []string
	// Whether to override existing CNI binaries
	UpdateCNIBinaries bool

	// The names of binaries to skip when copying
	SkipCNIBinaries []string

	// The HTTP port for monitoring
	MonitoringPort int

	// The UDS server address that CNI plugin will send log to.
	LogUDSAddress string
}

// RepairConfig struct defines the Istio CNI race repair configuration
type RepairConfig struct {
	// Whether to enable CNI race repair
	Enabled bool

	// Whether to run CNI as a DaemonSet (i.e. continuously via k8s watch),
	// or just one-off
	RunAsDaemon bool

	// The node name that the CNI DaemonSet runs on
	NodeName string

	// Key and value for broken pod label
	LabelKey   string
	LabelValue string

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
	b.WriteString("CNINetDir: " + c.CNINetDir + "\n")
	b.WriteString("MountedCNINetDir: " + c.MountedCNINetDir + "\n")
	b.WriteString("CNIConfName: " + c.CNIConfName + "\n")
	b.WriteString("ChainedCNIPlugin: " + fmt.Sprint(c.ChainedCNIPlugin) + "\n")
	b.WriteString("CNINetworkConfigFile: " + c.CNINetworkConfigFile + "\n")
	b.WriteString("CNINetworkConfig: " + c.CNINetworkConfig + "\n")
	b.WriteString("CNIEnableInstall: " + fmt.Sprint(c.CNIEnableInstall) + "\n")
	b.WriteString("CNIEnableReinstall: " + fmt.Sprint(c.CNIEnableReinstall) + "\n")

	b.WriteString("LogLevel: " + c.LogLevel + "\n")
	b.WriteString("KubeconfigFilename: " + c.KubeconfigFilename + "\n")
	b.WriteString("KubeconfigMode: " + fmt.Sprintf("%#o", c.KubeconfigMode) + "\n")
	b.WriteString("KubeCAFile: " + c.KubeCAFile + "\n")
	b.WriteString("SkipTLSVerify: " + fmt.Sprint(c.SkipTLSVerify) + "\n")

	b.WriteString("K8sServiceProtocol: " + c.K8sServiceProtocol + "\n")
	b.WriteString("K8sServiceHost: " + c.K8sServiceHost + "\n")
	b.WriteString("K8sServicePort: " + fmt.Sprint(c.K8sServicePort) + "\n")
	b.WriteString("K8sNodeName: " + c.K8sNodeName + "\n")
	b.WriteString("UpdateCNIBinaries: " + fmt.Sprint(c.UpdateCNIBinaries) + "\n")
	b.WriteString("SkipCNIBinaries: " + fmt.Sprint(c.SkipCNIBinaries) + "\n")
	b.WriteString("MonitoringPort: " + fmt.Sprint(c.MonitoringPort) + "\n")
	b.WriteString("LogUDSAddress: " + fmt.Sprint(c.LogUDSAddress) + "\n")
	return b.String()
}

func (c RepairConfig) String() string {
	var b strings.Builder
	b.WriteString("Enabled: " + fmt.Sprint(c.Enabled) + "\n")
	b.WriteString("RunAsDaemon: " + fmt.Sprint(c.RunAsDaemon) + "\n")
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
