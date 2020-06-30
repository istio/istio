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

package constants

// Command line arguments
const (
	CNINetDir            = "cni-net-dir"
	MountedCNINetDir     = "mounted-cni-net-dir"
	CNIConfName          = "cni-conf-name"
	KubeCfgFilename      = "kubecfg-file-name"
	ChainedCNIPlugin     = "chained-cni-plugin"
	SkipCNIBinaries      = "skip-cni-binaries"
	UpdateCNIBinaries    = "update-cni-binaries"
	CNINetworkConfig     = "cni-network-config"
	CNINetworkConfigFile = "cni-network-config-file"
	KubeCAFile           = "kube-ca-file"
	SkipTLSVerify        = "skip-tls-verify"
	LogLevel             = "log-level"
)

// Internal constants
const (
	HostCNIBinDir   = "/host/opt/cni/bin"
	SecondaryBinDir = "/host/secondary-bin-dir"
)
