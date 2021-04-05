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
	CNINetDir        string
	MountedCNINetDir string
	CNIConfName      string
	ChainedCNIPlugin bool

	CNINetworkConfigFile string
	CNINetworkConfig     string

	LogLevel           string
	KubeconfigFilename string
	KubeconfigMode     int
	KubeCAFile         string
	SkipTLSVerify      bool

	K8sServiceProtocol string
	K8sServiceHost     string
	K8sServicePort     string
	K8sNodeName        string

	UpdateCNIBinaries bool
	SkipCNIBinaries   []string
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
