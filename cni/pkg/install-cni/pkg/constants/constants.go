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
	MountedCNINetDir     = "mounted-cni-net-dir"
	CNINetDir            = "cni-net-dir"
	CNIConfName          = "cni-conf-name"
	ChainedCNIPlugin     = "chained-cni-plugin"
	Sleep                = "sleep"
	CNINetworkConfigFile = "cni-network-config-file"
	CNINetworkConfig     = "cni-network-config"
	LogLevel             = "log-level"
	KubeconfigFilename   = "kubecfg-file-name"
	KubeconfigMode       = "kubeconfig-mode"
	KubeCAFile           = "kube-ca-file"
	SkipTLSVerify        = "skip-tls-verify"
	SkipCNIBinaries      = "skip-cni-binaries"
	UpdateCNIBinaries    = "update-cni-binaries"
)

// Internal constants
const (
	CNIBinDir             = "/opt/cni/bin"
	HostCNIBinDir         = "/host/opt/cni/bin"
	SecondaryBinDir       = "/host/secondary-bin-dir"
	ServiceAccountPath    = "/var/run/secrets/kubernetes.io/serviceaccount"
	DefaultKubeconfigMode = 0600
)
