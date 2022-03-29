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
	// Install
	MountedCNINetDir     = "mounted-cni-net-dir"
	CNINetDir            = "cni-net-dir"
	CNIConfName          = "cni-conf-name"
	ChainedCNIPlugin     = "chained-cni-plugin"
	CNINetworkConfigFile = "cni-network-config-file"
	CNINetworkConfig     = "cni-network-config"
	CNIEnableInstall     = "cni-enable-install"
	CNIEnableReinstall   = "cni-enable-reinstall"
	LogLevel             = "log-level"
	KubeconfigFilename   = "kubecfg-file-name"
	KubeconfigMode       = "kubeconfig-mode"
	KubeCAFile           = "kube-ca-file"
	SkipTLSVerify        = "skip-tls-verify"
	SkipCNIBinaries      = "skip-cni-binaries"
	UpdateCNIBinaries    = "update-cni-binaries"
	MonitoringPort       = "monitoring-port"
	LogUDSAddress        = "log-uds-address"

	// Repair
	RepairEnabled            = "repair-enabled"
	RepairDeletePods         = "repair-delete-pods"
	RepairLabelPods          = "repair-label-pods"
	RepairRunAsDaemon        = "repair-run-as-daemon"
	RepairLabelKey           = "repair-broken-pod-label-key"
	RepairLabelValue         = "repair-broken-pod-label-value"
	RepairNodeName           = "repair-node-name"
	RepairSidecarAnnotation  = "repair-sidecar-annotation"
	RepairInitContainerName  = "repair-init-container-name"
	RepairInitTerminationMsg = "repair-init-container-termination-message"
	RepairInitExitCode       = "repair-init-container-exit-code"
	RepairLabelSelectors     = "repair-label-selectors"
	RepairFieldSelectors     = "repair-field-selectors"
)

// Internal constants
var (
	CNIBinDir             = "/opt/cni/bin"
	HostCNIBinDir         = "/host/opt/cni/bin"
	SecondaryBinDir       = "/host/secondary-bin-dir"
	ServiceAccountPath    = "/var/run/secrets/kubernetes.io/serviceaccount"
	DefaultKubeconfigMode = 0o600
	UDSLogPath            = "/log"

	// K8s liveness and readiness endpoints
	LivenessEndpoint  = "/healthz"
	ReadinessEndpoint = "/readyz"
	Port              = "8000"
)
