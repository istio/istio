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

import (
	"os"
	"strconv"
)

// Command line arguments
const (
	// Install
	MountedCNINetDir                  = "mounted-cni-net-dir"
	CNIConfName                       = "cni-conf-name"
	ChainedCNIPlugin                  = "chained-cni-plugin"
	CNINetworkConfigFile              = "cni-network-config-file"
	CNINetworkConfig                  = "cni-network-config"
	IstioOwnedCNIConfig               = "istio-owned-cni-config"
	IstioOwnedCNIConfigFilename       = "istio-owned-cni-config-filename"
	LogLevel                          = "log-level"
	KubeconfigMode                    = "kubeconfig-mode"
	KubeCAFile                        = "kube-ca-file"
	SkipTLSVerify                     = "skip-tls-verify"
	MonitoringPort                    = "monitoring-port"
	LogUDSSocket                      = "log-uds-socket"
	ZtunnelUDSAddress                 = "ztunnel-uds-address"
	CNIEventSocket                    = "cni-event-address"
	CNIAgentRunDir                    = "cni-agent-run-dir"
	ExcludeNamespaces                 = "exclude-namespaces"
	PodNamespace                      = "pod-namespace"
	AmbientEnabled                    = "ambient-enabled"
	AmbientEnablementSelector         = "ambient-enablement-selector"
	AmbientDNSCapture                 = "ambient-dns-capture"
	AmbientIPv6                       = "ambient-ipv6"
	AmbientDisableSafeUpgrade         = "ambient-disable-safe-upgrade"
	AmbientReconcilePodRulesOnStartup = "ambient-reconcile-pod-rules-on-startup"

	NativeNftables = "native-nftables"
	KubeletCgroup  = "kubelet-cgroup"

	// Repair
	RepairEnabled            = "repair-enabled"
	RepairDeletePods         = "repair-delete-pods"
	RepairRepairPods         = "repair-repair-pods"
	RepairLabelPods          = "repair-label-pods"
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
const (
	DefaultKubeconfigMode = 0o600
	CNIAgentLogScope      = "cni-agent"
	CNIPluginLogScope     = "cni-plugin"
	CNIAddEventPath       = "/cmdadd"
	UDSLogPath            = "/log"
	CNIEventSocketName    = "pluginevent.sock"
	LogUDSSocketName      = "log.sock"
	LocalRollingLogName   = "istio-cni.log"
	RollingLogMaxSizeMB   = 10
	CNIPluginKubeconfName = "istio-cni-kubeconfig"
	// K8s liveness and readiness endpoints
	LivenessEndpoint                   = "/healthz"
	ReadinessEndpoint                  = "/readyz"
	ReadinessPort                      = "8000"
	ServiceAccountPath                 = "/var/run/secrets/kubernetes.io/serviceaccount"
	SelfNetNSPath                      = "/proc/self/ns/net"
	DefaultIstioOwnedCNIConfigFilename = "02-istio-cni.conflist"
)

// Exposed for testing "constants"
var (
	CNIBinDir     = "/opt/cni/bin"
	HostCNIBinDir = "/host/opt/cni/bin"
	// Well-known subpath we will mount any needed host-mounts under,
	// to preclude shadowing or breaking any pod-internal mounts
	HostMountsPath = "/host"
	// HostNetNSPath is set to "/proc/self/ns/net" by default to prevent unintended execution on the host,
	// even during test development. At runtime, it is overridden when the ALLOW_SWITCH_TO_HOST_NS
	// environment variable is set to true (see CNI daemonset), which updates it to "/host/proc/1/ns/net" to align with the actual
	// host network namespace.
	HostNetNSPath = SelfNetNSPath
)

func init() {
	if allowSwitch, err := strconv.ParseBool(os.Getenv("ALLOW_SWITCH_TO_HOST_NS")); err == nil && allowSwitch {
		HostNetNSPath = HostMountsPath + "/proc/1/ns/net"
	}
}
