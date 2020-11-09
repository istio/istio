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
