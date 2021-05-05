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

// Config struct defines the Istio CNI installation options
type Config struct {
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
}

func (c *Config) String() string {
	var b strings.Builder
	b.WriteString("CNINetDir: " + c.CNINetDir + "\n")
	b.WriteString("MountedCNINetDir: " + c.MountedCNINetDir + "\n")
	b.WriteString("CNIConfName: " + c.CNIConfName + "\n")
	b.WriteString("ChainedCNIPlugin: " + fmt.Sprint(c.ChainedCNIPlugin) + "\n")
	b.WriteString("CNINetworkConfigFile: " + c.CNINetworkConfigFile + "\n")
	b.WriteString("CNINetworkConfig: " + c.CNINetworkConfig + "\n")

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
	return b.String()
}
